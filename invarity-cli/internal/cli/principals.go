package cli

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/invarity/invarity-cli/internal/client"
)

var principalsCmd = &cobra.Command{
	Use:   "principals",
	Short: "Manage principals",
	Long: `Commands for managing principals.

Principals are the agents or services that use toolsets. Toolsets are applied
to principals to grant them access to specific tools.`,
}

var (
	principalsTenant    string
	principalsPrincipal string
	principalsToolsetID string
	principalsRevision  string
)

func init() {
	// Add subcommands
	principalsCmd.AddCommand(principalsApplyToolsetCmd)
}

// ============================================================================
// principals apply-toolset
// ============================================================================

var principalsApplyToolsetCmd = &cobra.Command{
	Use:   "apply-toolset",
	Short: "Apply a toolset to a principal",
	Long: `Applies a registered toolset to a principal.

The toolset must already be registered in the tenant's toolset library.
Use 'invarity toolsets register' to register a toolset first.

Exit Codes:
  0 - Success
  1 - Validation failed
  2 - Network/server error`,
	Example: `  # Apply a toolset to a principal
  invarity principals apply-toolset --principal my-agent --toolset payments-v1 --revision 1.0.0

  # With explicit tenant
  invarity principals apply-toolset --tenant acme --principal my-agent --toolset payments-v1 --revision 1.0.0`,
	RunE: runPrincipalsApplyToolset,
}

func init() {
	principalsApplyToolsetCmd.Flags().StringVar(&principalsTenant, "tenant", "", "Tenant ID (uses config default, or 'default' if not specified)")
	principalsApplyToolsetCmd.Flags().StringVar(&principalsPrincipal, "principal", "", "Principal ID (required)")
	principalsApplyToolsetCmd.Flags().StringVar(&principalsToolsetID, "toolset", "", "Toolset ID to apply (required)")
	principalsApplyToolsetCmd.Flags().StringVar(&principalsRevision, "revision", "", "Toolset revision to apply (required)")
	principalsApplyToolsetCmd.MarkFlagRequired("principal")
	principalsApplyToolsetCmd.MarkFlagRequired("toolset")
	principalsApplyToolsetCmd.MarkFlagRequired("revision")
}

func runPrincipalsApplyToolset(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Apply flag overrides
	if principalsTenant != "" {
		cfg.TenantID = principalsTenant
	}
	if principalsPrincipal != "" {
		cfg.PrincipalID = principalsPrincipal
	}

	// Validate configuration (requires principal for this operation)
	if err := cfg.ValidateForPrincipal(); err != nil {
		return err
	}

	c := newClient(cfg)

	// Use default tenant if not specified
	tenantID := cfg.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	req := &client.ApplyToolsetToPrincipalRequest{
		ToolsetID: principalsToolsetID,
		Revision:  principalsRevision,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	applyResp, rawJSON, err := c.ApplyToolsetToPrincipal(ctx, tenantID, cfg.PrincipalID, req)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support principal toolset application yet.")
			os.Exit(ExitNetworkError)
		}
		printError("Failed to apply toolset: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printSuccess("Toolset applied to principal successfully")
	printKeyValue("Toolset ID", applyResp.ToolsetID)
	printKeyValue("Revision", applyResp.Revision)
	printKeyValue("Principal", applyResp.PrincipalID)
	printKeyValue("Tenant", tenantID)
	printKeyValue("Status", applyResp.Status)
	if applyResp.Message != "" {
		printKeyValue("Message", applyResp.Message)
	}
	if applyResp.AppliedAt != "" {
		printKeyValue("Applied At", applyResp.AppliedAt)
	}

	return nil
}
