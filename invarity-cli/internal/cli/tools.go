package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/invarity/invarity-cli/internal/client"
	"github.com/invarity/invarity-cli/internal/validate"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage tool manifests",
	Long: `Commands for validating and registering tool manifests.

Tools are registered to a tenant's tool library and can be referenced by toolsets.
Toolsets are then applied to principals.`,
}

var (
	toolsValidateFile    string
	toolsRegisterFile    string
	toolsRegisterStdin   bool
	toolsTenant          string
	toolsContinueOnError bool
)

var toolsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a tool manifest",
	Long: `Validates a tool manifest file against the Invarity Tool Manifest JSON Schema.

Supports both YAML and JSON formats. Returns a non-zero exit code if validation fails.`,
	Example: `  invarity tools validate -f tool.yaml
  invarity tools validate -f tool.json --json`,
	RunE: runToolsValidate,
}

var toolsRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a tool with the tenant's tool library",
	Long: `Validates a tool manifest locally, computes schema_hash if missing, then registers
with the tenant's tool library in the Invarity registry.

Tools are registered to a tenant (not a principal). Once registered, tools can be
referenced by toolsets, which are then applied to principals.

Schema Hash Computation:
  If the tool does not include invarity.schema_hash, the CLI computes it as:
  sha256(<canonicalized JSON of invarity block>)

Exit Codes:
  0 - Success
  1 - Validation failed (no network calls made)
  2 - Network/server error`,
	Example: `  # Register a tool to the tenant's library
  invarity tools register -f tool.yaml

  # Register with explicit tenant
  invarity tools register -f tool.yaml --tenant acme

  # JSON output for scripting
  invarity tools register -f tool.json --json`,
	RunE: runToolsRegister,
}

var toolsValidateDirCmd = &cobra.Command{
	Use:   "validate-dir <directory>",
	Short: "Validate all tool manifests in a directory",
	Long: `Recursively finds all *.json, *.yaml, *.yml files in the directory
and validates each as a tool manifest.

Prints a summary with total, valid, and invalid counts.
Returns exit code 1 if any files are invalid.`,
	Example: `  invarity tools validate-dir ./tools
  invarity tools validate-dir ./tools --json`,
	Args: cobra.ExactArgs(1),
	RunE: runToolsValidateDir,
}

var toolsRegisterDirCmd = &cobra.Command{
	Use:   "register-dir <directory>",
	Short: "Register all tools in a directory to the tenant's library",
	Long: `Recursively finds and registers all tool manifests in a directory
to the tenant's tool library.

Each file is validated locally before registration. By default, if any file
fails validation, registration is aborted (no tools are registered).

Use --continue-on-error to register valid tools even when some fail validation.

Tools are registered to a tenant (not a principal). Once registered, tools can be
referenced by toolsets, which are then applied to principals.

Exit Codes:
  0 - All tools registered successfully
  1 - Validation failed (at least one invalid file)
  2 - Network/server error`,
	Example: `  # Register all tools in a directory
  invarity tools register-dir ./tools

  # Continue even if some files are invalid
  invarity tools register-dir ./tools --continue-on-error

  # With explicit tenant
  invarity tools register-dir ./tools --tenant acme`,
	Args: cobra.ExactArgs(1),
	RunE: runToolsRegisterDir,
}

func init() {
	// Validate command
	toolsValidateCmd.Flags().StringVarP(&toolsValidateFile, "file", "f", "", "Path to tool manifest file (required)")
	toolsValidateCmd.MarkFlagRequired("file")

	// Register command - tools are registered to tenant (not principal)
	toolsRegisterCmd.Flags().StringVarP(&toolsRegisterFile, "file", "f", "", "Path to tool manifest file")
	toolsRegisterCmd.Flags().BoolVar(&toolsRegisterStdin, "stdin", false, "Read tool manifest from stdin")
	toolsRegisterCmd.Flags().StringVar(&toolsTenant, "tenant", "", "Tenant ID (uses config default, or 'default' if not specified)")

	// Register-dir command - tools are registered to tenant (not principal)
	toolsRegisterDirCmd.Flags().BoolVar(&toolsContinueOnError, "continue-on-error", false, "Continue registering after validation failures")
	toolsRegisterDirCmd.Flags().StringVar(&toolsTenant, "tenant", "", "Tenant ID (uses config default, or 'default' if not specified)")

	// Add subcommands
	toolsCmd.AddCommand(toolsValidateCmd)
	toolsCmd.AddCommand(toolsRegisterCmd)
	toolsCmd.AddCommand(toolsValidateDirCmd)
	toolsCmd.AddCommand(toolsRegisterDirCmd)
}

func runToolsValidate(cmd *cobra.Command, args []string) error {
	validator, err := validate.NewValidator()
	if err != nil {
		printError("Failed to initialize validator: %v", err)
		os.Exit(ExitNetworkError)
	}

	result, err := validator.ValidateFile(toolsValidateFile)
	if err != nil {
		printError("Validation error: %v", err)
		os.Exit(ExitValidationError)
	}

	// Run constraint lint checks if schema validation passed
	var lintResult *validate.ConstraintLintResult
	if result.Valid {
		tool, err := validate.ParseToolFile(toolsValidateFile)
		if err == nil {
			lintResult = validate.LintToolConstraints(tool)
			if !lintResult.Valid {
				result.Valid = false
				for _, e := range lintResult.Errors {
					result.Errors = append(result.Errors, &validate.ValidationError{
						Path:    "invarity.constraints",
						Message: e,
					})
				}
			}
		}
	}

	// JSON output
	if cfgJSON {
		jsonOut, _ := json.MarshalIndent(result, "", "  ")
		printJSON(jsonOut)
		if !result.Valid {
			os.Exit(ExitValidationError)
		}
		return nil
	}

	// Human-readable output
	if result.Valid {
		printSuccess("Tool manifest is valid")
		return nil
	}

	// Validation failed
	printError("Tool manifest validation failed")
	fmt.Println()
	for _, e := range result.Errors {
		if e.Path != "" {
			fmt.Fprintf(os.Stderr, "  • %s: %s\n", errorColor.Sprint(e.Path), e.Message)
		} else {
			fmt.Fprintf(os.Stderr, "  • %s\n", e.Message)
		}
	}

	os.Exit(ExitValidationError)
	return nil
}

func runToolsRegister(cmd *cobra.Command, args []string) error {
	// Validate input source
	if toolsRegisterFile == "" && !toolsRegisterStdin {
		return fmt.Errorf("either --file or --stdin is required")
	}
	if toolsRegisterFile != "" && toolsRegisterStdin {
		return fmt.Errorf("cannot use both --file and --stdin")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Apply flag overrides
	if toolsTenant != "" {
		cfg.TenantID = toolsTenant
	}

	// Validate configuration (tools don't require principal)
	if err := cfg.ValidateForTools(); err != nil {
		return err
	}

	// Initialize validator
	validator, err := validate.NewValidator()
	if err != nil {
		printError("Failed to initialize validator: %v", err)
		os.Exit(ExitNetworkError)
	}

	// Parse and validate tool
	var tool map[string]interface{}
	var result *validate.ValidationResult

	if toolsRegisterStdin {
		// Read from stdin
		data, err := os.ReadFile("/dev/stdin")
		if err != nil {
			printError("Failed to read from stdin: %v", err)
			os.Exit(ExitValidationError)
		}
		result, err = validator.ValidateJSON(data)
		if err != nil {
			printError("Validation error: %v", err)
			os.Exit(ExitValidationError)
		}
		if err := json.Unmarshal(data, &tool); err != nil {
			printError("Invalid JSON: %v", err)
			os.Exit(ExitValidationError)
		}
	} else {
		result, err = validator.ValidateFile(toolsRegisterFile)
		if err != nil {
			printError("Validation error: %v", err)
			os.Exit(ExitValidationError)
		}
		tool, err = validate.ParseToolFile(toolsRegisterFile)
		if err != nil {
			printError("Failed to parse tool file: %v", err)
			os.Exit(ExitValidationError)
		}
	}

	if !result.Valid {
		printError("Tool manifest validation failed - cannot register")
		for _, e := range result.Errors {
			if e.Path != "" {
				fmt.Fprintf(os.Stderr, "  • %s: %s\n", errorColor.Sprint(e.Path), e.Message)
			} else {
				fmt.Fprintf(os.Stderr, "  • %s\n", e.Message)
			}
		}
		os.Exit(ExitValidationError)
	}

	// Run constraint lint checks
	lintResult := validate.LintToolConstraints(tool)
	if !lintResult.Valid {
		printError("Tool constraint validation failed - cannot register")
		for _, e := range lintResult.Errors {
			fmt.Fprintf(os.Stderr, "  • invarity.constraints: %s\n", e)
		}
		os.Exit(ExitValidationError)
	}

	// Normalize enums to lowercase and compute schema_hash if missing
	tool = validate.NormalizeToolEnums(tool)
	tool, err = validate.EnsureSchemaHash(tool)
	if err != nil {
		printError("Failed to compute schema_hash: %v", err)
		os.Exit(ExitValidationError)
	}

	c := newClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use scoped endpoint if tenant is specified, otherwise use default tenant
	tenantID := cfg.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	regResp, rawJSON, err := c.RegisterToolScoped(ctx, tenantID, tool)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support tenant-scoped tool registration yet.")
			printInfo("The tool manifest is valid and ready to be registered when the server supports it.")
			os.Exit(ExitNetworkError)
		}
		printError("Registration failed: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	toolName := ""
	if name, ok := tool["name"].(string); ok {
		toolName = name
	}

	if toolName != "" {
		printSuccess("Tool '%s' registered successfully", toolName)
	} else {
		printSuccess("Tool registered successfully")
	}
	printKeyValue("Tool ID", regResp.ToolID)
	printKeyValue("Version", regResp.Version)
	printKeyValue("Schema Hash", regResp.SchemaHash)
	if regResp.CreatedAt != "" {
		printKeyValue("Created At", regResp.CreatedAt)
	}

	return nil
}

// ValidateDirResult contains the result of validating a directory.
type ValidateDirResult struct {
	Total        int      `json:"total"`
	Valid        int      `json:"valid"`
	Invalid      int      `json:"invalid"`
	InvalidFiles []string `json:"invalid_files,omitempty"`
}

func runToolsValidateDir(cmd *cobra.Command, args []string) error {
	dir := args[0]

	files, err := validate.FindToolFiles(dir)
	if err != nil {
		printError("Failed to scan directory: %v", err)
		os.Exit(ExitValidationError)
	}

	if len(files) == 0 {
		printWarn("No tool files found in %s", dir)
		return nil
	}

	validator, err := validate.NewValidator()
	if err != nil {
		printError("Failed to initialize validator: %v", err)
		os.Exit(ExitNetworkError)
	}

	result := ValidateDirResult{Total: len(files)}
	var invalidDetails []map[string]interface{}

	for _, file := range files {
		vr, err := validator.ValidateFile(file)
		if err != nil {
			result.Invalid++
			result.InvalidFiles = append(result.InvalidFiles, file)
			invalidDetails = append(invalidDetails, map[string]interface{}{
				"file":  file,
				"error": err.Error(),
			})
			continue
		}

		// Run constraint lint checks if schema validation passed
		if vr.Valid {
			tool, err := validate.ParseToolFile(file)
			if err == nil {
				lintResult := validate.LintToolConstraints(tool)
				if !lintResult.Valid {
					vr.Valid = false
					for _, e := range lintResult.Errors {
						vr.Errors = append(vr.Errors, &validate.ValidationError{
							Path:    "invarity.constraints",
							Message: e,
						})
					}
				}
			}
		}

		if vr.Valid {
			result.Valid++
		} else {
			result.Invalid++
			result.InvalidFiles = append(result.InvalidFiles, file)
			errors := make([]string, 0, len(vr.Errors))
			for _, e := range vr.Errors {
				errors = append(errors, e.Error())
			}
			invalidDetails = append(invalidDetails, map[string]interface{}{
				"file":   file,
				"errors": errors,
			})
		}
	}

	// JSON output
	if cfgJSON {
		output := map[string]interface{}{
			"total":         result.Total,
			"valid":         result.Valid,
			"invalid":       result.Invalid,
			"invalid_files": invalidDetails,
		}
		jsonOut, _ := json.MarshalIndent(output, "", "  ")
		printJSON(jsonOut)
		if result.Invalid > 0 {
			os.Exit(ExitValidationError)
		}
		return nil
	}

	// Human-readable output
	printSection("Validation Summary")
	printKeyValue("Directory", dir)
	printKeyValue("Total Files", fmt.Sprintf("%d", result.Total))
	printKeyValue("Valid", fmt.Sprintf("%d", result.Valid))
	if result.Invalid > 0 {
		fmt.Fprintf(os.Stdout, "  %-20s %s\n", "Invalid:", errorColor.Sprintf("%d", result.Invalid))
	} else {
		printKeyValue("Invalid", "0")
	}

	if result.Invalid > 0 {
		printSection("Invalid Files")
		for _, detail := range invalidDetails {
			file := detail["file"].(string)
			fmt.Fprintf(os.Stderr, "  • %s\n", errorColor.Sprint(file))
			if errors, ok := detail["errors"].([]string); ok {
				for _, e := range errors {
					dimColor.Fprintf(os.Stderr, "      %s\n", e)
				}
			} else if errStr, ok := detail["error"].(string); ok {
				dimColor.Fprintf(os.Stderr, "      %s\n", errStr)
			}
		}
		os.Exit(ExitValidationError)
	}

	printSuccess("All %d tool manifests are valid", result.Valid)
	return nil
}

// RegisterResult tracks results for a single registration.
type RegisterResult struct {
	File       string `json:"file"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	ToolID     string `json:"tool_id,omitempty"`
	Version    string `json:"version,omitempty"`
	SchemaHash string `json:"schema_hash,omitempty"`
}

func runToolsRegisterDir(cmd *cobra.Command, args []string) error {
	dir := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Apply flag overrides
	if toolsTenant != "" {
		cfg.TenantID = toolsTenant
	}

	// Validate configuration (tools don't require principal)
	if err := cfg.ValidateForTools(); err != nil {
		return err
	}

	files, err := validate.FindToolFiles(dir)
	if err != nil {
		printError("Failed to scan directory: %v", err)
		os.Exit(ExitValidationError)
	}

	if len(files) == 0 {
		printWarn("No tool files found in %s", dir)
		return nil
	}

	validator, err := validate.NewValidator()
	if err != nil {
		printError("Failed to initialize validator: %v", err)
		os.Exit(ExitNetworkError)
	}

	// First, validate all files (schema + constraint lint checks)
	validFiles := make([]string, 0, len(files))
	invalidFiles := make([]string, 0)
	var invalidDetails []map[string]interface{}

	for _, file := range files {
		vr, err := validator.ValidateFile(file)
		if err != nil {
			invalidFiles = append(invalidFiles, file)
			invalidDetails = append(invalidDetails, map[string]interface{}{
				"file":  file,
				"error": err.Error(),
			})
			continue
		}

		// Run constraint lint checks if schema validation passed
		if vr.Valid {
			tool, err := validate.ParseToolFile(file)
			if err == nil {
				lintResult := validate.LintToolConstraints(tool)
				if !lintResult.Valid {
					vr.Valid = false
					for _, e := range lintResult.Errors {
						vr.Errors = append(vr.Errors, &validate.ValidationError{
							Path:    "invarity.constraints",
							Message: e,
						})
					}
				}
			}
		}

		if !vr.Valid {
			invalidFiles = append(invalidFiles, file)
			errors := make([]string, 0, len(vr.Errors))
			for _, e := range vr.Errors {
				errors = append(errors, e.Error())
			}
			invalidDetails = append(invalidDetails, map[string]interface{}{
				"file":   file,
				"errors": errors,
			})
		} else {
			validFiles = append(validFiles, file)
		}
	}

	// If any invalid and not continuing on error, abort
	if len(invalidFiles) > 0 && !toolsContinueOnError {
		printError("Validation failed for %d files - aborting registration", len(invalidFiles))
		printSection("Invalid Files")
		for _, detail := range invalidDetails {
			file := detail["file"].(string)
			fmt.Fprintf(os.Stderr, "  • %s\n", errorColor.Sprint(file))
			if errors, ok := detail["errors"].([]string); ok {
				for _, e := range errors {
					dimColor.Fprintf(os.Stderr, "      %s\n", e)
				}
			} else if errStr, ok := detail["error"].(string); ok {
				dimColor.Fprintf(os.Stderr, "      %s\n", errStr)
			}
		}
		os.Exit(ExitValidationError)
	}

	if len(validFiles) == 0 {
		printError("No valid tool files to register")
		os.Exit(ExitValidationError)
	}

	c := newClient(cfg)

	// Use default tenant if not specified
	tenantID := cfg.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	// Register with limited concurrency
	const maxConcurrency = 4
	results := make([]RegisterResult, len(validFiles))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)
	var serverNotSupported bool
	var mu sync.Mutex

	for i, file := range validFiles {
		wg.Add(1)
		go func(idx int, filePath string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := RegisterResult{File: filePath}

			// Check if we already know server doesn't support
			mu.Lock()
			if serverNotSupported {
				mu.Unlock()
				result.Error = "server does not support tenant-scoped tool registration"
				results[idx] = result
				return
			}
			mu.Unlock()

			tool, err := validate.ParseToolFile(filePath)
			if err != nil {
				result.Error = err.Error()
				results[idx] = result
				return
			}

			// Normalize and ensure schema_hash
			tool = validate.NormalizeToolEnums(tool)
			tool, err = validate.EnsureSchemaHash(tool)
			if err != nil {
				result.Error = fmt.Sprintf("failed to compute schema_hash: %v", err)
				results[idx] = result
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			regResp, _, err := c.RegisterToolScoped(ctx, tenantID, tool)
			if err != nil {
				if client.IsNotSupportedError(err) {
					mu.Lock()
					serverNotSupported = true
					mu.Unlock()
				}
				result.Error = err.Error()
			} else {
				result.Success = true
				result.ToolID = regResp.ToolID
				result.Version = regResp.Version
				result.SchemaHash = regResp.SchemaHash
			}
			results[idx] = result
		}(i, file)
	}

	wg.Wait()

	// Count results
	successCount := 0
	failureCount := 0
	var failures []RegisterResult

	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failureCount++
			failures = append(failures, r)
		}
	}

	// JSON output
	if cfgJSON {
		output := map[string]interface{}{
			"total":         len(validFiles),
			"success":       successCount,
			"failed":        failureCount,
			"skipped":       len(invalidFiles),
			"tenant_id":     tenantID,
			"results":       results,
			"invalid_files": invalidDetails,
		}
		jsonOut, _ := json.MarshalIndent(output, "", "  ")
		printJSON(jsonOut)
		if failureCount > 0 {
			os.Exit(ExitNetworkError)
		}
		return nil
	}

	// Human-readable output
	if serverNotSupported {
		printWarn("Server does not support tenant-scoped tool registration yet.")
		printInfo("All %d tool manifests are valid and ready to be registered when the server supports it.", len(validFiles))
		os.Exit(ExitNetworkError)
	}

	printSection("Registration Summary")
	printKeyValue("Directory", dir)
	printKeyValue("Tenant", tenantID)
	printKeyValue("Total Files", fmt.Sprintf("%d", len(files)))
	printKeyValue("Validated", fmt.Sprintf("%d", len(validFiles)))
	if len(invalidFiles) > 0 {
		fmt.Fprintf(os.Stdout, "  %-20s %s\n", "Skipped (invalid):", warnColor.Sprintf("%d", len(invalidFiles)))
	}
	printKeyValue("Registered", fmt.Sprintf("%d", successCount))
	if failureCount > 0 {
		fmt.Fprintf(os.Stdout, "  %-20s %s\n", "Failed:", errorColor.Sprintf("%d", failureCount))
	}

	if failureCount > 0 {
		printSection("Failed Registrations")
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "  • %s: %s\n", f.File, errorColor.Sprint(f.Error))
		}
		os.Exit(ExitNetworkError)
	}

	if successCount > 0 {
		printSuccess("Successfully registered %d tools", successCount)
	}

	return nil
}
