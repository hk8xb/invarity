package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/invarity/invarity-cli/internal/client"
	"github.com/invarity/invarity-cli/internal/validate"
)

var toolsetsCmd = &cobra.Command{
	Use:   "toolsets",
	Short: "Manage toolsets",
	Long:  `Commands for validating, applying, and linting toolsets.`,
}

var (
	toolsetsFile    string
	toolsetsToolDir string
	toolsetsEnv     string
	toolsetsStatus  string
)

func init() {
	// Add subcommands
	toolsetsCmd.AddCommand(toolsetsValidateCmd)
	toolsetsCmd.AddCommand(toolsetsApplyCmd)
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
			if toolset.Status != "" {
				printKeyValue("Status", toolset.Status)
			}
			if len(toolset.Envs) > 0 {
				printKeyValue("Environments", strings.Join(toolset.Envs, ", "))
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
// toolsets apply
// ============================================================================

var toolsetsApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a toolset to the server",
	Long: `Uploads a toolset manifest to the Invarity server.

The toolset is validated locally before being sent. Use --env and --status
to override values in the manifest.`,
	Example: `  invarity toolsets apply -f toolset.yaml
  invarity toolsets apply -f toolset.yaml --env prod --status ACTIVE
  invarity toolsets apply -f toolset.json --json`,
	RunE: runToolsetsApply,
}

func init() {
	toolsetsApplyCmd.Flags().StringVarP(&toolsetsFile, "file", "f", "", "Path to toolset file (required)")
	toolsetsApplyCmd.Flags().StringVar(&toolsetsEnv, "env", "", "Environment override (sandbox, prod)")
	toolsetsApplyCmd.Flags().StringVar(&toolsetsStatus, "status", "", "Status override (DRAFT, ACTIVE, DEPRECATED)")
	toolsetsApplyCmd.MarkFlagRequired("file")
}

func runToolsetsApply(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := cfg.ValidateWithAuth(); err != nil {
		return err
	}

	// Validate first
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
		printError("Toolset manifest validation failed - cannot apply")
		for _, e := range result.Errors {
			if e.Path != "" {
				fmt.Fprintf(os.Stderr, "  • %s: %s\n", errorColor.Sprint(e.Path), e.Message)
			} else {
				fmt.Fprintf(os.Stderr, "  • %s\n", e.Message)
			}
		}
		os.Exit(ExitValidationError)
	}

	// Parse toolset
	toolset, err := validate.ParseToolsetFile(toolsetsFile)
	if err != nil {
		printError("Failed to parse toolset: %v", err)
		os.Exit(ExitValidationError)
	}

	c := newClient(cfg)

	req := &client.ToolsetApplyRequest{
		Toolset: toolset,
		Env:     toolsetsEnv,
		Status:  toolsetsStatus,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	applyResp, rawJSON, err := c.ApplyToolset(ctx, req)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support toolset application yet.")
			printInfo("The toolset manifest is valid and ready to be applied when the server supports it.")
			os.Exit(ExitNetworkError)
		}
		printError("Toolset application failed: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printSuccess("Toolset applied successfully")
	printKeyValue("Toolset ID", applyResp.ToolsetID)
	printKeyValue("Revision", applyResp.Revision)
	printKeyValue("Status", applyResp.Status)
	if len(applyResp.Envs) > 0 {
		printKeyValue("Environments", strings.Join(applyResp.Envs, ", "))
	}
	printKeyValue("Tool Count", fmt.Sprintf("%d", applyResp.ToolCount))
	if applyResp.Message != "" {
		printKeyValue("Message", applyResp.Message)
	}

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
- Verifies each tool reference {id, version} exists
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
