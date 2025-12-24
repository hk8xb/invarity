package cli

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/invarity/invarity-cli/internal/client"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Manage audit records",
	Long:  `Commands for retrieving and viewing audit records.`,
}

var auditShowCmd = &cobra.Command{
	Use:   "show <audit_id>",
	Short: "Show an audit record",
	Long: `Retrieves and displays an audit record by its ID.

If the server does not yet support audit retrieval, a helpful message is displayed.`,
	Example: `  invarity audit show abc123
  invarity audit show abc123 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runAuditShow,
}

func init() {
	auditCmd.AddCommand(auditShowCmd)
}

func runAuditShow(cmd *cobra.Command, args []string) error {
	auditID := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	c := newClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	audit, rawJSON, err := c.GetAudit(ctx, auditID)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support audit retrieval yet.")
			return nil
		}
		printError("Failed to retrieve audit record: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printSection("Audit Record")
	printKeyValue("Audit ID", audit.AuditID)
	if audit.Timestamp != "" {
		printKeyValue("Timestamp", audit.Timestamp)
	}
	if audit.Decision != "" {
		printKeyValue("Decision", audit.Decision)
	}

	if audit.Request != nil {
		printSection("Request")
		reqJSON, _ := json.MarshalIndent(audit.Request, "  ", "  ")
		dimColor.Fprintf(os.Stdout, "  %s\n", string(reqJSON))
	}

	if audit.Response != nil {
		printSection("Response")
		respJSON, _ := json.MarshalIndent(audit.Response, "  ", "  ")
		dimColor.Fprintf(os.Stdout, "  %s\n", string(respJSON))
	}

	return nil
}
