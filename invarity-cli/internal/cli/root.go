// Package cli implements the CLI commands for the Invarity CLI.
package cli

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/invarity/invarity-cli/internal/client"
	"github.com/invarity/invarity-cli/internal/config"
)

var (
	// Version is set at build time
	Version = "dev"

	// Global flags
	cfgServer    string
	cfgAPIKey    string
	cfgTenant    string
	cfgPrincipal string
	cfgTrace     bool
	cfgJSON      bool

	// Colors for output
	successColor = color.New(color.FgGreen)
	errorColor   = color.New(color.FgRed)
	warnColor    = color.New(color.FgYellow)
	infoColor    = color.New(color.FgCyan)
	dimColor     = color.New(color.Faint)
)

// Exit codes
const (
	ExitSuccess         = 0
	ExitValidationError = 1
	ExitNetworkError    = 2
)

// RootCmd is the root command for the Invarity CLI.
var RootCmd = &cobra.Command{
	Use:   "invarity",
	Short: "Invarity CLI - Control plane for agent tool execution",
	Long: `Invarity CLI provides tools for managing and evaluating agent tool execution.

It interfaces with the Invarity server to evaluate tool calls, register tools,
apply policies, and retrieve audit records.

Tool Registration:
  Tools are registered to a principal-scoped registry. The "register" command
  validates locally, computes schema_hash if missing, then stores in the registry.
  Registration is scoped to a tenant and principal.

Configuration can be provided via:
  - Command-line flags (highest priority)
  - Environment variables (INVARITY_SERVER, INVARITY_API_KEY, INVARITY_TENANT_ID, INVARITY_PRINCIPAL_ID)
  - Config file (~/.invarity/config.yaml)`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	// Global flags
	RootCmd.PersistentFlags().StringVar(&cfgServer, "server", "", "Invarity server URL (default: http://localhost:8080)")
	RootCmd.PersistentFlags().StringVar(&cfgAPIKey, "api-key", "", "API key for authentication")
	RootCmd.PersistentFlags().StringVar(&cfgTenant, "tenant", "", "Default tenant ID")
	RootCmd.PersistentFlags().StringVar(&cfgPrincipal, "principal", "", "Default principal ID")
	RootCmd.PersistentFlags().BoolVar(&cfgTrace, "trace", false, "Print HTTP request/response metadata")
	RootCmd.PersistentFlags().BoolVar(&cfgJSON, "json", false, "Output raw JSON response")

	// Add subcommands
	RootCmd.AddCommand(pingCmd)
	RootCmd.AddCommand(simulateCmd)
	RootCmd.AddCommand(toolsCmd)
	RootCmd.AddCommand(toolsetsCmd)
	RootCmd.AddCommand(policyCmd)
	RootCmd.AddCommand(auditCmd)
	RootCmd.AddCommand(versionCmd)
}

// Execute runs the root command.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		printError(err.Error())
		os.Exit(ExitNetworkError)
	}
}

// loadConfig loads configuration with flag overrides.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	// Override with flags
	if cfgServer != "" {
		cfg.Server = cfgServer
	}
	if cfgAPIKey != "" {
		cfg.APIKey = cfgAPIKey
	}
	if cfgTenant != "" {
		cfg.TenantID = cfgTenant
	}
	if cfgPrincipal != "" {
		cfg.PrincipalID = cfgPrincipal
	}
	cfg.Trace = cfgTrace
	cfg.JSON = cfgJSON

	return cfg, nil
}

// newClient creates a new API client from config.
func newClient(cfg *config.Config) *client.Client {
	var opts []client.Option
	if cfg.Trace {
		opts = append(opts, client.WithTrace(os.Stderr))
	}
	return client.New(cfg, opts...)
}

// Output helpers

func printSuccess(format string, args ...interface{}) {
	if cfgJSON {
		return
	}
	successColor.Fprintf(os.Stdout, "✓ "+format+"\n", args...)
}

func printError(format string, args ...interface{}) {
	errorColor.Fprintf(os.Stderr, "✗ "+format+"\n", args...)
}

func printWarn(format string, args ...interface{}) {
	if cfgJSON {
		return
	}
	warnColor.Fprintf(os.Stderr, "⚠ "+format+"\n", args...)
}

func printInfo(format string, args ...interface{}) {
	if cfgJSON {
		return
	}
	infoColor.Fprintf(os.Stdout, format+"\n", args...)
}

func printDim(format string, args ...interface{}) {
	if cfgJSON {
		return
	}
	dimColor.Fprintf(os.Stdout, format+"\n", args...)
}

func printJSON(data []byte) {
	fmt.Fprintln(os.Stdout, string(data))
}

func printKeyValue(key, value string) {
	if cfgJSON {
		return
	}
	fmt.Fprintf(os.Stdout, "  %-20s %s\n", key+":", value)
}

func printSection(title string) {
	if cfgJSON {
		return
	}
	fmt.Fprintf(os.Stdout, "\n%s\n", infoColor.Sprint(title))
}
