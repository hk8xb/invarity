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
	"github.com/invarity/invarity-cli/internal/config"
	"github.com/invarity/invarity-cli/internal/policy"
	"github.com/invarity/invarity-cli/internal/poller"
)

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage policies",
	Long: `Commands for validating, applying, and managing policies.

Policy Lifecycle:
  1. validate  - Check policy syntax and structure locally
  2. diff      - Compare local policy with active policy
  3. apply     - Upload policy to server for compilation
  4. status    - Check compilation status
  5. fuzziness - Review unresolved terms and variables
  6. promote   - Activate a compiled policy`,
}

// Policy command flags
var (
	policyFile      string
	policyWait      bool
	policyOrg       string
	policyEnv       string
	policyProjectID string
	policyActive    bool
)

func init() {
	// Global policy flags
	policyCmd.PersistentFlags().StringVar(&policyOrg, "org", "", "Organization ID (overrides config)")
	policyCmd.PersistentFlags().StringVar(&policyEnv, "env", "", "Environment (sandbox, staging, prod)")
	policyCmd.PersistentFlags().StringVar(&policyProjectID, "project", "", "Project ID (optional)")

	// Add subcommands
	policyCmd.AddCommand(policyValidateCmd)
	policyCmd.AddCommand(policyDiffCmd)
	policyCmd.AddCommand(policyApplyCmd)
	policyCmd.AddCommand(policyStatusCmd)
	policyCmd.AddCommand(policyPromoteCmd)
	policyCmd.AddCommand(policyFuzzinessCmd)
}

// loadPolicyConfig loads config with policy-specific overrides
func loadPolicyConfig() (*config.Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	// Apply policy-specific overrides
	if policyOrg != "" {
		cfg.OrgID = policyOrg
	}
	if policyEnv != "" {
		cfg.Env = config.NormalizeEnv(policyEnv)
	}
	if policyProjectID != "" {
		cfg.ProjectID = policyProjectID
	}

	return cfg, nil
}

// ============================================================================
// policy validate
// ============================================================================

var policyValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a policy file locally",
	Long: `Validates a policy file against the Invarity policy schema.

Performs syntax and structural validation without contacting the server.
Returns a detailed validation report with errors and warnings.`,
	Example: `  invarity policy validate -f policy.yaml
  invarity policy validate -f policy.yaml --json`,
	RunE: runPolicyValidate,
}

func init() {
	policyValidateCmd.Flags().StringVarP(&policyFile, "file", "f", "", "Path to policy file (required)")
	policyValidateCmd.MarkFlagRequired("file")
}

func runPolicyValidate(cmd *cobra.Command, args []string) error {
	report, p, err := policy.ValidateFile(policyFile)
	if err != nil {
		printError("Failed to validate file: %v", err)
		os.Exit(ExitValidationError)
	}

	// JSON output
	if cfgJSON {
		jsonOut, _ := json.MarshalIndent(report, "", "  ")
		printJSON(jsonOut)
		if !report.Valid {
			os.Exit(ExitValidationError)
		}
		return nil
	}

	// Human-readable output
	if report.Valid {
		printSuccess("Policy is valid")
	} else {
		printError("Policy validation failed")
	}

	// Print summary
	if report.Summary != nil {
		printSection("Summary")
		if report.Summary.Name != "" {
			printKeyValue("Name", report.Summary.Name)
		}
		if report.Summary.Version != "" {
			printKeyValue("Version", report.Summary.Version)
		}
		if report.Summary.APIVersion != "" {
			printKeyValue("API Version", report.Summary.APIVersion)
		}
		printKeyValue("Rules", fmt.Sprintf("%d", report.Summary.RuleCount))
	}

	// Print errors
	if len(report.Errors) > 0 {
		printSection("Errors")
		for _, e := range report.Errors {
			if e.Field != "" {
				fmt.Fprintf(os.Stderr, "  • %s: %s\n", errorColor.Sprint(e.Field), e.Message)
			} else {
				fmt.Fprintf(os.Stderr, "  • %s\n", e.Message)
			}
		}
	}

	// Print warnings
	if len(report.Warnings) > 0 {
		printSection("Warnings")
		for _, w := range report.Warnings {
			if w.Field != "" {
				fmt.Fprintf(os.Stdout, "  • %s: %s\n", warnColor.Sprint(w.Field), w.Message)
			} else {
				fmt.Fprintf(os.Stdout, "  • %s\n", w.Message)
			}
		}
	}

	// Show canonical render preview
	if p != nil && report.Valid {
		printSection("Canonical Preview")
		canonical, err := policy.CanonicalRender(p)
		if err == nil {
			lines := strings.Split(canonical, "\n")
			maxLines := 15
			if len(lines) > maxLines {
				for _, line := range lines[:maxLines] {
					dimColor.Fprintf(os.Stdout, "  %s\n", line)
				}
				dimColor.Fprintf(os.Stdout, "  ... (%d more lines)\n", len(lines)-maxLines)
			} else {
				for _, line := range lines {
					dimColor.Fprintf(os.Stdout, "  %s\n", line)
				}
			}
		}
	}

	if !report.Valid {
		os.Exit(ExitValidationError)
	}
	return nil
}

// ============================================================================
// policy diff
// ============================================================================

var policyDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Compare local policy with active policy",
	Long: `Computes and displays a diff between a local policy file and the
currently active policy on the server.

If the server doesn't support fetching the active policy, shows a canonical
rendering of the local policy instead.`,
	Example: `  invarity policy diff -f policy.yaml
  invarity policy diff -f policy.yaml --org my-org --env prod`,
	RunE: runPolicyDiff,
}

func init() {
	policyDiffCmd.Flags().StringVarP(&policyFile, "file", "f", "", "Path to policy file (required)")
	policyDiffCmd.MarkFlagRequired("file")
}

func runPolicyDiff(cmd *cobra.Command, args []string) error {
	cfg, err := loadPolicyConfig()
	if err != nil {
		return err
	}

	// Parse local policy
	localPolicy, err := policy.ParseFile(policyFile)
	if err != nil {
		printError("Failed to parse local policy: %v", err)
		os.Exit(ExitValidationError)
	}

	localCanonical, err := policy.CanonicalRender(localPolicy)
	if err != nil {
		printError("Failed to render local policy: %v", err)
		os.Exit(ExitValidationError)
	}

	// Try to fetch remote policy
	var remoteCanonical string
	var remoteErr error

	if cfg.OrgID != "" {
		c := newClient(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		remoteData, _, err := c.GetActivePolicy(ctx, cfg.OrgID, cfg.Env, cfg.ProjectID)
		if err != nil {
			if client.IsNotSupportedError(err) {
				remoteErr = err
			} else {
				remoteErr = err
			}
		} else if remoteData != nil {
			remotePolicy := &policy.Policy{Raw: remoteData}
			remoteCanonical, _ = policy.CanonicalRender(remotePolicy)
		}
	} else {
		remoteErr = fmt.Errorf("org_id not configured")
	}

	// JSON output
	if cfgJSON {
		output := map[string]interface{}{
			"local_policy":   localPolicy.Raw,
			"diff_available": remoteCanonical != "",
		}
		if remoteErr != nil {
			output["remote_error"] = remoteErr.Error()
		}
		jsonOut, _ := json.MarshalIndent(output, "", "  ")
		printJSON(jsonOut)
		return nil
	}

	// Human-readable output
	if remoteErr != nil {
		if client.IsNotSupportedError(remoteErr) {
			printWarn("Server does not support fetching active policy yet.")
		} else {
			printWarn("Could not fetch active policy: %v", remoteErr)
		}
		printInfo("Showing canonical render of local policy instead:")
		fmt.Println()
		fmt.Println(localCanonical)
		return nil
	}

	if remoteCanonical == "" {
		printInfo("No active policy found on server. Local policy:")
		fmt.Println()
		fmt.Println(localCanonical)
		return nil
	}

	// Show diff
	diff := policy.DiffPolicies(localCanonical, remoteCanonical)
	if diff == "No differences found." {
		printSuccess("Local policy matches active policy")
	} else {
		printSection("Policy Diff")
		fmt.Println(diff)
	}

	return nil
}

// ============================================================================
// policy apply
// ============================================================================

var policyApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a policy to the server",
	Long: `Uploads a policy to the Invarity server for compilation.

The policy is validated locally before being sent. After upload, the server
compiles the policy and may report fuzziness (unresolved terms/variables).

Use --wait to block until compilation completes.`,
	Example: `  invarity policy apply -f policy.yaml
  invarity policy apply -f policy.yaml --wait
  invarity policy apply -f policy.yaml --org my-org --env prod --json`,
	RunE: runPolicyApply,
}

func init() {
	policyApplyCmd.Flags().StringVarP(&policyFile, "file", "f", "", "Path to policy file (required)")
	policyApplyCmd.Flags().BoolVar(&policyWait, "wait", false, "Wait for compilation to complete")
	policyApplyCmd.MarkFlagRequired("file")
}

func runPolicyApply(cmd *cobra.Command, args []string) error {
	cfg, err := loadPolicyConfig()
	if err != nil {
		return err
	}

	if err := cfg.ValidateForPolicy(); err != nil {
		return err
	}

	// Validate locally first
	report, p, err := policy.ValidateFile(policyFile)
	if err != nil {
		printError("Failed to validate file: %v", err)
		os.Exit(ExitValidationError)
	}

	if !report.Valid {
		printError("Policy validation failed - cannot apply")
		for _, e := range report.Errors {
			if e.Field != "" {
				fmt.Fprintf(os.Stderr, "  • %s: %s\n", errorColor.Sprint(e.Field), e.Message)
			} else {
				fmt.Fprintf(os.Stderr, "  • %s\n", e.Message)
			}
		}
		os.Exit(ExitValidationError)
	}

	c := newClient(cfg)

	// Build apply request
	req := &client.PolicyApplyRequest{
		OrgID:       cfg.OrgID,
		Environment: cfg.Env,
		ProjectID:   cfg.ProjectID,
		Policy:      p.Raw,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	applyResp, rawJSON, err := c.ApplyPolicyV2(ctx, req)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support policy application yet.")
			printInfo("The policy file is valid and ready to be applied when the server supports it.")
			return nil
		}
		printError("Policy application failed: %v", err)
		os.Exit(ExitNetworkError)
	}

	// If --wait, poll for completion
	if policyWait && applyResp.PolicyVersion != "" {
		applyResp, rawJSON, err = waitForPolicyReady(c, applyResp.PolicyVersion)
		if err != nil {
			printError("Error waiting for policy: %v", err)
			os.Exit(ExitNetworkError)
		}
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printSuccess("Policy applied successfully")
	printKeyValue("Policy Version", applyResp.PolicyVersion)
	printKeyValue("Status", formatPolicyStatus(applyResp.Status))

	if applyResp.Message != "" {
		printKeyValue("Message", applyResp.Message)
	}

	// Show fuzziness summary if present
	if applyResp.FuzzinessReport != nil {
		printFuzzinessSummary(applyResp.FuzzinessReport)
	}

	// Hint about next steps
	if applyResp.Status == "READY" || applyResp.Status == "ready" {
		printInfo("\nNext step: invarity policy promote %s --active", applyResp.PolicyVersion)
	} else if applyResp.Status == "COMPILING" || applyResp.Status == "compiling" {
		printInfo("\nCheck status: invarity policy status %s", applyResp.PolicyVersion)
	}

	return nil
}

func waitForPolicyReady(c *client.Client, policyVersion string) (*client.PolicyApplyResponse, []byte, error) {
	pollCfg := poller.DefaultConfig()
	pollCfg.MaxWait = 5 * time.Minute

	pollFunc := func(ctx context.Context) (poller.Status, interface{}, error) {
		statusResp, rawJSON, err := c.GetPolicyStatus(ctx, policyVersion)
		if err != nil {
			return poller.StatusFailed, nil, err
		}
		return poller.ParseStatus(statusResp.Status), rawJSON, nil
	}

	p := poller.New(pollFunc, pollCfg)
	if !cfgJSON {
		p.WithProgress(func(attempt int, elapsed time.Duration, status poller.Status) {
			fmt.Fprintf(os.Stderr, "\r⏳ Compiling policy... [%s] (attempt %d, %s)   ",
				status, attempt, elapsed.Round(time.Second))
		})
	}

	result := p.Poll(context.Background())

	if !cfgJSON {
		fmt.Fprintln(os.Stderr) // Clear progress line
	}

	if result.Error != nil {
		return nil, nil, result.Error
	}

	// Convert result to PolicyApplyResponse
	rawJSON, _ := result.Data.([]byte)
	return &client.PolicyApplyResponse{
		PolicyVersion: policyVersion,
		Status:        string(result.Status),
	}, rawJSON, nil
}

// ============================================================================
// policy status
// ============================================================================

var policyStatusCmd = &cobra.Command{
	Use:   "status <policy_version>",
	Short: "Check policy compilation status",
	Long: `Retrieves the current status of a policy version.

Shows compilation status, any errors or warnings, and artifact references
if compilation is complete.`,
	Example: `  invarity policy status pol_abc123
  invarity policy status pol_abc123 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runPolicyStatus,
}

func runPolicyStatus(cmd *cobra.Command, args []string) error {
	policyVersion := args[0]

	cfg, err := loadPolicyConfig()
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	c := newClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	statusResp, rawJSON, err := c.GetPolicyStatus(ctx, policyVersion)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support policy status yet.")
			return nil
		}
		printError("Failed to get policy status: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printSection("Policy Status")
	printKeyValue("Version", statusResp.PolicyVersion)
	printKeyValue("Status", formatPolicyStatus(statusResp.Status))

	if statusResp.CreatedAt != "" {
		printKeyValue("Created", statusResp.CreatedAt)
	}
	if statusResp.UpdatedAt != "" {
		printKeyValue("Updated", statusResp.UpdatedAt)
	}
	if statusResp.Message != "" {
		printKeyValue("Message", statusResp.Message)
	}

	// Errors
	if len(statusResp.Errors) > 0 {
		printSection("Errors")
		for _, e := range statusResp.Errors {
			fmt.Fprintf(os.Stderr, "  • %s\n", errorColor.Sprint(e))
		}
	}

	// Warnings
	if len(statusResp.Warnings) > 0 {
		printSection("Warnings")
		for _, w := range statusResp.Warnings {
			fmt.Fprintf(os.Stdout, "  • %s\n", warnColor.Sprint(w))
		}
	}

	// Artifacts
	if len(statusResp.Artifacts) > 0 {
		printSection("Artifacts")
		for _, a := range statusResp.Artifacts {
			fmt.Fprintf(os.Stdout, "  • %s\n", a)
		}
	}

	return nil
}

// ============================================================================
// policy promote
// ============================================================================

var policyPromoteCmd = &cobra.Command{
	Use:   "promote <policy_version>",
	Short: "Promote a policy to active status",
	Long: `Promotes a compiled policy version to active status.

This makes the policy the currently active policy for the organization/environment.
Only policies with READY status can be promoted.`,
	Example: `  invarity policy promote pol_abc123 --active
  invarity policy promote pol_abc123 --active --json`,
	Args: cobra.ExactArgs(1),
	RunE: runPolicyPromote,
}

func init() {
	policyPromoteCmd.Flags().BoolVar(&policyActive, "active", false, "Promote to ACTIVE status (required)")
	policyPromoteCmd.MarkFlagRequired("active")
}

func runPolicyPromote(cmd *cobra.Command, args []string) error {
	policyVersion := args[0]

	if !policyActive {
		return fmt.Errorf("--active flag is required")
	}

	cfg, err := loadPolicyConfig()
	if err != nil {
		return err
	}

	if err := cfg.ValidateWithAuth(); err != nil {
		return err
	}

	c := newClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	promoteResp, rawJSON, err := c.PromotePolicy(ctx, policyVersion, "ACTIVE")
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not support policy promotion yet.")
			return nil
		}
		printError("Failed to promote policy: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printSuccess("Policy promoted successfully")
	printKeyValue("Version", promoteResp.PolicyVersion)
	printKeyValue("Status", formatPolicyStatus(promoteResp.Status))
	printKeyValue("Target", promoteResp.Target)

	if promoteResp.ActivatedAt != "" {
		printKeyValue("Activated At", promoteResp.ActivatedAt)
	}
	if promoteResp.Message != "" {
		printKeyValue("Message", promoteResp.Message)
	}

	return nil
}

// ============================================================================
// policy fuzziness
// ============================================================================

var policyFuzzinessCmd = &cobra.Command{
	Use:   "fuzziness <policy_version>",
	Short: "Show policy fuzziness report",
	Long: `Displays the fuzziness report for a policy version.

The fuzziness report shows:
  - Unresolved terms that couldn't be mapped to known concepts
  - Required variables that must be provided at runtime
  - Suggested mappings that may help resolve ambiguities

When variables are missing, requests may ESCALATE to human review.`,
	Example: `  invarity policy fuzziness pol_abc123
  invarity policy fuzziness pol_abc123 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runPolicyFuzziness,
}

func runPolicyFuzziness(cmd *cobra.Command, args []string) error {
	policyVersion := args[0]

	cfg, err := loadPolicyConfig()
	if err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	c := newClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fuzziness, rawJSON, err := c.GetPolicyFuzziness(ctx, policyVersion)
	if err != nil {
		if client.IsNotSupportedError(err) {
			printWarn("Server does not expose fuzziness report yet.")
			return nil
		}
		printError("Failed to get fuzziness report: %v", err)
		os.Exit(ExitNetworkError)
	}

	// JSON output
	if cfgJSON {
		printJSON(rawJSON)
		return nil
	}

	// Human-readable output
	printFuzzinessReport(fuzziness)

	return nil
}

// ============================================================================
// Helper functions
// ============================================================================

func formatPolicyStatus(status string) string {
	upper := strings.ToUpper(status)
	switch upper {
	case "READY", "ACTIVE":
		return successColor.Sprint(upper)
	case "FAILED", "ERROR":
		return errorColor.Sprint(upper)
	case "COMPILING", "PENDING", "PROCESSING":
		return warnColor.Sprint(upper)
	default:
		return status
	}
}

func printFuzzinessSummary(f *client.FuzzinessReport) {
	if f == nil {
		return
	}

	hasIssues := len(f.UnresolvedTerms) > 0 || len(f.RequiredVariables) > 0

	if !hasIssues && f.FuzzinessScore == 0 {
		return
	}

	printSection("Fuzziness Report")

	if f.FuzzinessScore > 0 {
		scoreColor := successColor
		if f.FuzzinessScore > 0.5 {
			scoreColor = warnColor
		}
		if f.FuzzinessScore > 0.8 {
			scoreColor = errorColor
		}
		fmt.Fprintf(os.Stdout, "  Score: %s\n", scoreColor.Sprintf("%.2f", f.FuzzinessScore))
	}

	if len(f.UnresolvedTerms) > 0 {
		fmt.Fprintf(os.Stdout, "  Unresolved Terms: %d\n", len(f.UnresolvedTerms))
	}
	if len(f.RequiredVariables) > 0 {
		fmt.Fprintf(os.Stdout, "  Required Variables: %d\n", len(f.RequiredVariables))
	}

	if f.Summary != "" {
		fmt.Fprintf(os.Stdout, "\n  %s\n", dimColor.Sprint(f.Summary))
	}
}

func printFuzzinessReport(f *client.FuzzinessReport) {
	if f == nil {
		printInfo("No fuzziness report available.")
		return
	}

	printSection("Fuzziness Report")

	// Score
	if f.FuzzinessScore > 0 {
		scoreColor := successColor
		label := "Low"
		if f.FuzzinessScore > 0.3 {
			scoreColor = infoColor
			label = "Moderate"
		}
		if f.FuzzinessScore > 0.5 {
			scoreColor = warnColor
			label = "High"
		}
		if f.FuzzinessScore > 0.8 {
			scoreColor = errorColor
			label = "Very High"
		}
		fmt.Fprintf(os.Stdout, "  Fuzziness Score: %s (%s)\n\n",
			scoreColor.Sprintf("%.2f", f.FuzzinessScore), label)
	}

	// Unresolved terms
	if len(f.UnresolvedTerms) > 0 {
		fmt.Fprintf(os.Stdout, "  %s\n", warnColor.Sprint("Unresolved Terms:"))
		for _, term := range f.UnresolvedTerms {
			fmt.Fprintf(os.Stdout, "    • %s\n", errorColor.Sprint(term.Term))
			if term.Location != "" {
				dimColor.Fprintf(os.Stdout, "      Location: %s\n", term.Location)
			}
			if term.Context != "" {
				dimColor.Fprintf(os.Stdout, "      Context: %s\n", term.Context)
			}
			if len(term.Suggestions) > 0 {
				dimColor.Fprintf(os.Stdout, "      Suggestions: %s\n", strings.Join(term.Suggestions, ", "))
			}
		}
		fmt.Println()
	}

	// Required variables
	if len(f.RequiredVariables) > 0 {
		fmt.Fprintf(os.Stdout, "  %s\n", infoColor.Sprint("Required Variables:"))
		for _, v := range f.RequiredVariables {
			fmt.Fprintf(os.Stdout, "    • %s", v.Name)
			if v.Type != "" {
				fmt.Fprintf(os.Stdout, " (%s)", v.Type)
			}
			fmt.Println()
			if v.Description != "" {
				dimColor.Fprintf(os.Stdout, "      %s\n", v.Description)
			}
			if v.Default != "" {
				dimColor.Fprintf(os.Stdout, "      Default: %s\n", v.Default)
			}
		}
		fmt.Println()
	}

	// Suggested mappings
	if len(f.SuggestedMappings) > 0 {
		fmt.Fprintf(os.Stdout, "  %s\n", successColor.Sprint("Suggested Mappings:"))
		for _, m := range f.SuggestedMappings {
			fmt.Fprintf(os.Stdout, "    • %s → %s", m.From, successColor.Sprint(m.To))
			if m.Confidence > 0 {
				fmt.Fprintf(os.Stdout, " (%.0f%% confidence)", m.Confidence*100)
			}
			fmt.Println()
			if m.Reason != "" {
				dimColor.Fprintf(os.Stdout, "      %s\n", m.Reason)
			}
		}
		fmt.Println()
	}

	// Summary
	if f.Summary != "" {
		printSection("What This Means")
		fmt.Fprintf(os.Stdout, "  %s\n", f.Summary)
	} else if len(f.UnresolvedTerms) > 0 || len(f.RequiredVariables) > 0 {
		printSection("What This Means")
		fmt.Fprintf(os.Stdout, "  %s\n",
			warnColor.Sprint("Requests matching fuzzy conditions may ESCALATE to human review"))
		fmt.Fprintf(os.Stdout, "  %s\n",
			"until unresolved terms are mapped or required variables are provided.")
	}
}
