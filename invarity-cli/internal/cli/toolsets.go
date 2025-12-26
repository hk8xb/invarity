package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/invarity/invarity-cli/internal/client"
	"github.com/invarity/invarity-cli/internal/validate"
)

var toolsetsCmd = &cobra.Command{
	Use:   "toolsets",
	Short: "Manage toolsets",
	Long: `Commands for validating, registering, and linting toolsets.

Toolsets are registered to a tenant's toolset library. They bundle tool references
and can be applied to principals using the 'principals apply-toolset' command.`,
}

var (
	toolsetsFile    string
	toolsetsToolDir string
	toolsetsTenant  string
)

func init() {
	// Add subcommands
	toolsetsCmd.AddCommand(toolsetsValidateCmd)
	toolsetsCmd.AddCommand(toolsetsRegisterCmd)
	toolsetsCmd.AddCommand(toolsetsLintCmd)
}

// ============================================================================
// toolsets validate
// ============================================================================

var toolsetsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a toolset manifest",
	Long: `Validates a toolset manifest file against the Invarity Toolset Schema.

Checks that all required fields are present and properly formatted.`,
	Example: `  invarity toolsets validate -f toolset.yaml
  invarity toolsets validate -f toolset.json --json`,
	RunE: runToolsetsValidate,
}

func init() {
	toolsetsValidateCmd.Flags().StringVarP(&toolsetsFile, "file", "f", "", "Path to toolset file (required)")
	toolsetsValidateCmd.MarkFlagRequired("file")
}

func runToolsetsValidate(cmd *cobra.Command, args []string) error {
	validator, err := validate.NewToolsetValidator()
	if err != nil {
		printError("Failed to initialize validator: %v", err)
		os.Exit(ExitNetworkError)
	}

	result, err := validator.ValidateFile(toolsetsFile)
	if err != nil {
		printError("Validation error: %v", err)
		os.Exit(ExitValidationError)
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
		printSuccess("Toolset manifest is valid")

		// Parse and show summary
		toolset, err := validate.ParseToolsetWithMetadata(toolsetsFile)
		if err == nil {
			printSection("Summary")
			printKeyValue("Toolset ID", toolset.ToolsetID)
			if toolset.Revision != "" {
				printKeyValue("Revision", toolset.Revision)
			}
			printKeyValue("Tool Count", fmt.Sprintf("%d", len(toolset.Tools)))
		}
		return nil
	}

	// Validation failed
	printError("Toolset manifest validation failed")
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

// ============================================================================
// toolsets register
// ============================================================================

var toolsetsRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a toolset with the tenant's toolset library",
	Long: `Validates a toolset locally and registers it with the tenant's toolset library.

Toolsets are registered to a tenant (not a principal). Once registered, toolsets
can be applied to principals using the 'principals apply-toolset' command.

When --tools-dir is provided:
  1. Verifies that all tools referenced in the toolset exist in the directory
  2. Automatically registers the referenced tools before registering the toolset
  3. Only tools referenced by the toolset are registered (not all tools in dir)

Exit Codes:
  0 - Success
  1 - Validation failed (no network calls made)
  2 - Network/server error`,
	Example: `  # Register a toolset to the tenant's library
  invarity toolsets register -f toolset.yaml

  # Register with auto-registration of referenced tools
  invarity toolsets register -f toolset.yaml --tools-dir ./tools

  # With explicit tenant
  invarity toolsets register -f toolset.yaml --tenant acme`,
	RunE: runToolsetsRegister,
}

func init() {
	toolsetsRegisterCmd.Flags().StringVarP(&toolsetsFile, "file", "f", "", "Path to toolset file (required)")
	toolsetsRegisterCmd.Flags().StringVar(&toolsetsToolDir, "tools-dir", "", "Path to tools directory (auto-registers referenced tools)")
	toolsetsRegisterCmd.Flags().StringVar(&toolsetsTenant, "tenant", "", "Tenant ID (uses config default, or 'default' if not specified)")
	toolsetsRegisterCmd.MarkFlagRequired("file")
}

func runToolsetsRegister(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Apply flag overrides
	if toolsetsTenant != "" {
		cfg.TenantID = toolsetsTenant
	}

	// Validate configuration (toolsets don't require principal)
	if err := cfg.ValidateForToolsets(); err != nil {
		return err
	}

	// Validate toolset first
	validator, err := validate.NewToolsetValidator()
	if err != nil {
		printError("Failed to initialize validator: %v", err)
		os.Exit(ExitNetworkError)
	}

	result, err := validator.ValidateFile(toolsetsFile)
	if err != nil {
		printError("Validation error: %v", err)
		os.Exit(ExitValidationError)
	}

	if !result.Valid {
		printError("Toolset manifest validation failed - cannot register")
		for _, e := range result.Errors {
			if e.Path != "" {
				fmt.Fprintf(os.Stderr, "  • %s: %s\n", errorColor.Sprint(e.Path), e.Message)
			} else {
				fmt.Fprintf(os.Stderr, "  • %s\n", e.Message)
			}
		}
		os.Exit(ExitValidationError)
	}

	// Parse toolset with metadata
	toolset, err := validate.ParseToolsetWithMetadata(toolsetsFile)
	if err != nil {
		printError("Failed to parse toolset: %v", err)
		os.Exit(ExitValidationError)
	}

	// Parse toolset as raw map for API
	toolsetRaw, err := validate.ParseToolsetFile(toolsetsFile)
	if err != nil {
		printError("Failed to parse toolset: %v", err)
		os.Exit(ExitValidationError)
	}

	c := newClient(cfg)

	// Use default tenant if not specified
	tenantID := cfg.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	// If --tools-dir is provided, auto-register referenced tools
	if toolsetsToolDir != "" {
		if err := autoRegisterToolsForToolset(c, tenantID, toolset, toolsetsToolDir); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	regResp, rawJSON, err := c.RegisterToolset(ctx, tenantID, toolsetRaw)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support tenant-scoped toolset registration yet.")
			printInfo("The toolset manifest is valid and ready to be registered when the server supports it.")
			os.Exit(ExitNetworkError)
		}
		printError("Toolset registration failed: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printSuccess("Toolset registered successfully")
	printKeyValue("Toolset ID", regResp.ToolsetID)
	printKeyValue("Revision", regResp.Revision)
	printKeyValue("Tenant", tenantID)
	printKeyValue("Tool Count", fmt.Sprintf("%d", regResp.ToolCount))
	if regResp.Message != "" {
		printKeyValue("Message", regResp.Message)
	}

	return nil
}

// autoRegisterToolsForToolset registers all tools referenced by the toolset from the tools directory.
func autoRegisterToolsForToolset(c *client.Client, tenantID string, toolset *validate.Toolset, toolsDir string) error {
	// Build set of referenced tool keys
	referencedTools := make(map[string]validate.ToolRef)
	for _, ref := range toolset.Tools {
		key := fmt.Sprintf("%s@%s", ref.ID, ref.Version)
		referencedTools[key] = ref
	}

	// Find all tool files in directory
	toolFiles, err := validate.FindToolFiles(toolsDir)
	if err != nil {
		printError("Failed to scan tools directory: %v", err)
		os.Exit(ExitValidationError)
	}

	// Build map of available tools
	availableTools := make(map[string]*validate.ParsedTool)
	for _, file := range toolFiles {
		tool, err := validate.ParseToolWithMetadata(file)
		if err != nil {
			continue // Skip invalid files
		}
		if tool.ID == "" || tool.Version == "" {
			continue // Skip tools without ID/version
		}
		key := fmt.Sprintf("%s@%s", tool.ID, tool.Version)
		availableTools[key] = tool
	}

	// Check for missing tools
	var missingTools []validate.ToolRef
	var toolsToRegister []*validate.ParsedTool
	for key, ref := range referencedTools {
		if tool, exists := availableTools[key]; exists {
			toolsToRegister = append(toolsToRegister, tool)
		} else {
			missingTools = append(missingTools, ref)
		}
	}

	if len(missingTools) > 0 {
		printError("Cannot register toolset - missing tools in %s:", toolsDir)
		for _, ref := range missingTools {
			fmt.Fprintf(os.Stderr, "  • %s@%s\n", errorColor.Sprint(ref.ID), ref.Version)
		}
		os.Exit(ExitValidationError)
	}

	if len(toolsToRegister) == 0 {
		return nil // No tools to register
	}

	// Register the referenced tools
	printInfo("Registering %d tools referenced by toolset...", len(toolsToRegister))

	toolValidator, err := validate.NewValidator()
	if err != nil {
		printError("Failed to initialize tool validator: %v", err)
		os.Exit(ExitNetworkError)
	}

	registeredCount := 0
	failedCount := 0

	for _, parsedTool := range toolsToRegister {
		// Validate the tool
		vr, err := toolValidator.ValidateFile(parsedTool.FilePath)
		if err != nil || !vr.Valid {
			printWarn("Skipping invalid tool: %s", parsedTool.FilePath)
			failedCount++
			continue
		}

		// Parse, normalize, and ensure schema_hash
		tool, err := validate.ParseToolFile(parsedTool.FilePath)
		if err != nil {
			printWarn("Failed to parse tool %s: %v", parsedTool.FilePath, err)
			failedCount++
			continue
		}

		tool = validate.NormalizeToolEnums(tool)
		tool, err = validate.EnsureSchemaHash(tool)
		if err != nil {
			printWarn("Failed to compute schema_hash for %s: %v", parsedTool.FilePath, err)
			failedCount++
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _, err = c.RegisterToolScoped(ctx, tenantID, tool)
		cancel()

		if err != nil {
			if client.IsNotSupportedError(err) {
				printWarn("Server does not support tenant-scoped tool registration yet.")
				os.Exit(ExitNetworkError)
			}
			printWarn("Failed to register %s@%s: %v", parsedTool.ID, parsedTool.Version, err)
			failedCount++
			continue
		}

		registeredCount++
		if !cfgJSON {
			printDim("  Registered: %s@%s", parsedTool.ID, parsedTool.Version)
		}
	}

	if failedCount > 0 {
		printError("Failed to register %d tools", failedCount)
		os.Exit(ExitNetworkError)
	}

	printSuccess("Registered %d tools", registeredCount)
	return nil
}

// ============================================================================
// toolsets lint
// ============================================================================

var toolsetsLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Lint a toolset against a tools directory",
	Long: `Checks that all tool references in a toolset exist in the tools directory.

This command:
- Loads the toolset manifest
- Scans the tools directory for all tool definitions
- Verifies each tool reference {tool_id, version} exists
- Reports missing references and unreferenced tools`,
	Example: `  invarity toolsets lint -f toolset.yaml --tools-dir ./tools
  invarity toolsets lint -f toolset.json --tools-dir ./tools --json`,
	RunE: runToolsetsLint,
}

func init() {
	toolsetsLintCmd.Flags().StringVarP(&toolsetsFile, "file", "f", "", "Path to toolset file (required)")
	toolsetsLintCmd.Flags().StringVar(&toolsetsToolDir, "tools-dir", "", "Path to tools directory (required)")
	toolsetsLintCmd.MarkFlagRequired("file")
	toolsetsLintCmd.MarkFlagRequired("tools-dir")
}

func runToolsetsLint(cmd *cobra.Command, args []string) error {
	// Parse toolset
	toolset, err := validate.ParseToolsetWithMetadata(toolsetsFile)
	if err != nil {
		printError("Failed to parse toolset: %v", err)
		os.Exit(ExitValidationError)
	}

	// Run lint
	result, err := validate.LintToolset(toolset, toolsetsToolDir)
	if err != nil {
		printError("Lint failed: %v", err)
		os.Exit(ExitValidationError)
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
	printSection("Toolset Lint Report")
	printKeyValue("Toolset", toolset.ToolsetID)
	printKeyValue("Tools Referenced", fmt.Sprintf("%d", len(toolset.Tools)))

	if result.Valid && len(result.ExtraTools) == 0 {
		printSuccess("All tool references are valid")
		return nil
	}

	// Missing tools
	if len(result.MissingTools) > 0 {
		printSection("Missing Tools")
		for _, ref := range result.MissingTools {
			fmt.Fprintf(os.Stderr, "  • %s@%s\n", errorColor.Sprint(ref.ID), ref.Version)
		}
	}

	// Extra tools (warnings, not errors)
	if len(result.ExtraTools) > 0 {
		printSection("Unreferenced Tools")
		for _, ref := range result.ExtraTools {
			fmt.Fprintf(os.Stdout, "  • %s@%s\n", warnColor.Sprint(ref.ID), ref.Version)
		}
	}

	// Invalid tool files
	if len(result.InvalidTools) > 0 {
		printSection("Invalid Tool Files")
		for _, file := range result.InvalidTools {
			fmt.Fprintf(os.Stderr, "  • %s\n", errorColor.Sprint(file))
		}
	}

	// Tools without ID
	if len(result.ToolsWithoutID) > 0 {
		printSection("Tools Missing invarity.id/version")
		for _, file := range result.ToolsWithoutID {
			fmt.Fprintf(os.Stderr, "  • %s\n", warnColor.Sprint(file))
		}
	}

	// Errors
	if len(result.Errors) > 0 {
		printSection("Errors")
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  • %s\n", e)
		}
	}

	if !result.Valid {
		os.Exit(ExitValidationError)
	}

	return nil
}
