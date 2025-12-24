package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/invarity/invarity-cli/internal/client"
	"github.com/invarity/invarity-cli/internal/validate"
)

var (
	simulateFile    string
	simulateExplain bool
)

var simulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Simulate a tool call evaluation",
	Long: `Reads a ToolCallRequest JSON file and sends it to the firewall evaluation endpoint.

The request file should contain a JSON object with the tool call details.
Use --explain to see a detailed breakdown of the decision.`,
	Example: `  invarity simulate -f request.json
  invarity simulate -f request.json --explain
  invarity simulate -f request.json --json`,
	RunE: runSimulate,
}

func init() {
	simulateCmd.Flags().StringVarP(&simulateFile, "file", "f", "", "Path to request JSON file (required)")
	simulateCmd.Flags().BoolVar(&simulateExplain, "explain", false, "Show detailed decision explanation")
	simulateCmd.MarkFlagRequired("file")
}

func runSimulate(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	// Parse request file
	request, err := validate.ParseRequestFile(simulateFile)
	if err != nil {
		printError("Failed to parse request file: %v", err)
		os.Exit(ExitValidationError)
	}

	c := newClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, rawJSON, err := c.Evaluate(ctx, request)
	if err != nil {
		printError("Evaluation failed: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output mode
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printDecisionSummary(resp)

	if simulateExplain {
		printDecisionExplanation(resp)
	}

	return nil
}

func printDecisionSummary(resp *client.EvaluateResponse) {
	// Decision with color coding
	var decisionColor *color.Color
	switch strings.ToLower(resp.Decision) {
	case "allow", "allowed", "approve", "approved":
		decisionColor = successColor
	case "deny", "denied", "block", "blocked", "reject", "rejected":
		decisionColor = errorColor
	case "review", "escalate", "pending":
		decisionColor = warnColor
	default:
		decisionColor = infoColor
	}

	fmt.Fprintf(os.Stdout, "\nDecision: %s\n", decisionColor.Sprint(strings.ToUpper(resp.Decision)))

	if resp.BaseRisk != "" {
		riskColor := getRiskColor(resp.BaseRisk)
		fmt.Fprintf(os.Stdout, "Base Risk: %s\n", riskColor.Sprint(resp.BaseRisk))
	}

	if resp.RiskScore > 0 {
		fmt.Fprintf(os.Stdout, "Risk Score: %.2f\n", resp.RiskScore)
	}
}

func printDecisionExplanation(resp *client.EvaluateResponse) {
	// IDs
	printSection("Identifiers")
	if resp.AuditID != "" {
		printKeyValue("Audit ID", resp.AuditID)
	}
	if resp.RequestID != "" {
		printKeyValue("Request ID", resp.RequestID)
	}

	// Policy
	if resp.Policy != nil {
		printSection("Policy")
		if resp.Policy.Name != "" {
			printKeyValue("Name", resp.Policy.Name)
		}
		if resp.Policy.PolicyID != "" {
			printKeyValue("Policy ID", resp.Policy.PolicyID)
		}
		if resp.Policy.Status != "" {
			printKeyValue("Status", resp.Policy.Status)
		}
		if resp.Policy.Version != "" {
			printKeyValue("Version", resp.Policy.Version)
		}
	}

	// Alignment
	if resp.Alignment != nil {
		printSection("Alignment")
		if resp.Alignment.AggregatedVote != "" {
			printKeyValue("Aggregated Vote", resp.Alignment.AggregatedVote)
		}
		if resp.Alignment.Confidence > 0 {
			printKeyValue("Confidence", fmt.Sprintf("%.2f", resp.Alignment.Confidence))
		}
		if len(resp.Alignment.Voters) > 0 {
			fmt.Println("\n  Voters:")
			for _, voter := range resp.Alignment.Voters {
				voteColor := getVoteColor(voter.Vote)
				fmt.Fprintf(os.Stdout, "    • %s: %s", voter.Name, voteColor.Sprint(voter.Vote))
				if voter.Confidence > 0 {
					fmt.Fprintf(os.Stdout, " (%.2f)", voter.Confidence)
				}
				fmt.Println()
				if voter.Reason != "" {
					dimColor.Fprintf(os.Stdout, "      %s\n", voter.Reason)
				}
			}
		}
	}

	// Threat
	if resp.Threat != nil && (resp.Threat.Label != "" || len(resp.Threat.Types) > 0) {
		printSection("Threat Detection")
		if resp.Threat.Label != "" {
			threatColor := getThreatColor(resp.Threat.Label)
			printKeyValue("Label", threatColor.Sprint(resp.Threat.Label))
		}
		if len(resp.Threat.Types) > 0 {
			printKeyValue("Types", strings.Join(resp.Threat.Types, ", "))
		}
		if resp.Threat.Score > 0 {
			printKeyValue("Score", fmt.Sprintf("%.2f", resp.Threat.Score))
		}
	}

	// Arbiter
	if resp.Arbiter != nil {
		printSection("Arbiter Reasoning")
		if len(resp.Arbiter.DerivedFacts) > 0 {
			fmt.Println("\n  Derived Facts:")
			for _, fact := range resp.Arbiter.DerivedFacts {
				fmt.Fprintf(os.Stdout, "    • %s\n", fact)
			}
		}
		if len(resp.Arbiter.ClausesUsed) > 0 {
			fmt.Println("\n  Clauses Used:")
			for _, clause := range resp.Arbiter.ClausesUsed {
				fmt.Fprintf(os.Stdout, "    • %s\n", clause)
			}
		}
		if resp.Arbiter.Reasoning != "" {
			fmt.Println("\n  Reasoning:")
			dimColor.Fprintf(os.Stdout, "    %s\n", resp.Arbiter.Reasoning)
		}
	}

	fmt.Println()
}

func getRiskColor(risk string) *color.Color {
	switch strings.ToLower(risk) {
	case "low":
		return successColor
	case "medium":
		return warnColor
	case "high", "critical":
		return errorColor
	default:
		return infoColor
	}
}

func getVoteColor(vote string) *color.Color {
	switch strings.ToLower(vote) {
	case "allow", "approve", "yes", "aligned":
		return successColor
	case "deny", "reject", "no", "misaligned":
		return errorColor
	case "abstain", "uncertain", "review":
		return warnColor
	default:
		return infoColor
	}
}

func getThreatColor(label string) *color.Color {
	switch strings.ToLower(label) {
	case "none", "safe":
		return successColor
	case "low":
		return infoColor
	case "medium", "suspicious":
		return warnColor
	case "high", "malicious", "critical":
		return errorColor
	default:
		return infoColor
	}
}
