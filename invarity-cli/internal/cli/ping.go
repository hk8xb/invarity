package cli

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check server health",
	Long:  `Sends a health check request to the Invarity server and displays the status.`,
	RunE:  runPing,
}

func runPing(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	c := newClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	health, err := c.Ping(ctx)
	if err != nil {
		printError("Server unreachable: %v", err)
		os.Exit(ExitNetworkError)
	}

	if cfgJSON {
		jsonOut, _ := json.MarshalIndent(health, "", "  ")
		printJSON(jsonOut)
		return nil
	}

	printSuccess("Server is healthy")
	if health.Status != "" {
		printKeyValue("Status", health.Status)
	}
	if health.Version != "" {
		printKeyValue("Version", health.Version)
	}

	return nil
}
