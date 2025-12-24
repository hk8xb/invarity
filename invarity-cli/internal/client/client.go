// Package client provides an HTTP client for the Invarity API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/invarity/invarity-cli/internal/config"
)

// Version is the CLI version, set at build time.
var Version = "dev"

// Client is an HTTP client for the Invarity API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	trace      bool
	traceOut   io.Writer
}

// RequestTrace contains metadata about an HTTP request/response for debugging.
type RequestTrace struct {
	Method       string
	URL          string
	StatusCode   int
	Duration     time.Duration
	RequestSize  int
	ResponseSize int
}

// Option is a functional option for configuring the client.
type Option func(*Client)

// WithTrace enables request/response tracing.
func WithTrace(w io.Writer) Option {
	return func(c *Client) {
		c.trace = true
		c.traceOut = w
	}
}

// New creates a new Invarity API client.
func New(cfg *config.Config, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimSuffix(cfg.Server, "/"),
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// doRequest performs an HTTP request with common handling.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, []byte, error) {
	// Build URL
	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid URL path: %w", err)
	}

	// Prepare body
	var reqBody io.Reader
	var reqSize int
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
		reqSize = len(jsonBody)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", fmt.Sprintf("invarity-cli/%s", Version))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	// Execute request with timing
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Trace output
	if c.trace && c.traceOut != nil {
		trace := RequestTrace{
			Method:       method,
			URL:          u,
			StatusCode:   resp.StatusCode,
			Duration:     duration,
			RequestSize:  reqSize,
			ResponseSize: len(respBody),
		}
		c.printTrace(trace)
	}

	return resp, respBody, nil
}

func (c *Client) printTrace(t RequestTrace) {
	fmt.Fprintf(c.traceOut, "\n[TRACE] %s %s\n", t.Method, t.URL)
	fmt.Fprintf(c.traceOut, "[TRACE] Status: %d | Duration: %s | Request: %d bytes | Response: %d bytes\n",
		t.StatusCode, t.Duration.Round(time.Millisecond), t.RequestSize, t.ResponseSize)
}

// HealthResponse represents the response from /healthz.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// Ping checks the server health.
func (c *Client) Ping(ctx context.Context) (*HealthResponse, error) {
	resp, body, err := c.doRequest(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var health HealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		// If we can't parse the response, just report healthy based on status code
		return &HealthResponse{Status: "ok"}, nil
	}

	return &health, nil
}

// EvaluateRequest represents a tool call evaluation request.
type EvaluateRequest map[string]interface{}

// EvaluateResponse represents the response from /v1/firewall/evaluate.
type EvaluateResponse struct {
	Decision   string                 `json:"decision"`
	BaseRisk   string                 `json:"base_risk,omitempty"`
	RiskScore  float64                `json:"risk_score,omitempty"`
	AuditID    string                 `json:"audit_id,omitempty"`
	RequestID  string                 `json:"request_id,omitempty"`
	Policy     *PolicyStatus          `json:"policy,omitempty"`
	Alignment  *AlignmentResult       `json:"alignment,omitempty"`
	Threat     *ThreatInfo            `json:"threat,omitempty"`
	Arbiter    *ArbiterResult         `json:"arbiter,omitempty"`
	Raw        map[string]interface{} `json:"-"`
}

// PolicyStatus represents policy evaluation status.
type PolicyStatus struct {
	Status   string `json:"status,omitempty"`
	Version  string `json:"version,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	Name     string `json:"name,omitempty"`
}

// AlignmentResult represents alignment voter results.
type AlignmentResult struct {
	AggregatedVote string        `json:"aggregated_vote,omitempty"`
	Confidence     float64       `json:"confidence,omitempty"`
	Voters         []VoterResult `json:"voters,omitempty"`
}

// VoterResult represents a single voter's result.
type VoterResult struct {
	Name       string  `json:"name,omitempty"`
	Vote       string  `json:"vote,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// ThreatInfo represents threat detection information.
type ThreatInfo struct {
	Label string   `json:"label,omitempty"`
	Types []string `json:"types,omitempty"`
	Score float64  `json:"score,omitempty"`
}

// ArbiterResult represents arbiter reasoning results.
type ArbiterResult struct {
	DerivedFacts []string `json:"derived_facts,omitempty"`
	ClausesUsed  []string `json:"clauses_used,omitempty"`
	Reasoning    string   `json:"reasoning,omitempty"`
}

// Evaluate sends a tool call request for evaluation.
func (c *Client) Evaluate(ctx context.Context, request EvaluateRequest) (*EvaluateResponse, []byte, error) {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "/v1/firewall/evaluate", request)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var evalResp EvaluateResponse
	if err := json.Unmarshal(body, &evalResp); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	// Also store raw response for full JSON output
	json.Unmarshal(body, &evalResp.Raw)

	return &evalResp, body, nil
}

// RegisterTool registers a new tool with the server.
func (c *Client) RegisterTool(ctx context.Context, tool map[string]interface{}) ([]byte, error) {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "/v1/tools", tool)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &NotSupportedError{Feature: "tool registration"}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// ApplyPolicy applies a policy to the server.
func (c *Client) ApplyPolicy(ctx context.Context, policy map[string]interface{}) ([]byte, error) {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "/v1/policies/apply", policy)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &NotSupportedError{Feature: "policy application"}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// AuditRecord represents an audit record.
type AuditRecord struct {
	AuditID   string                 `json:"audit_id"`
	Timestamp string                 `json:"timestamp,omitempty"`
	Decision  string                 `json:"decision,omitempty"`
	Request   map[string]interface{} `json:"request,omitempty"`
	Response  map[string]interface{} `json:"response,omitempty"`
}

// GetAudit retrieves an audit record by ID.
func (c *Client) GetAudit(ctx context.Context, auditID string) (*AuditRecord, []byte, error) {
	path := fmt.Sprintf("/v1/audit/%s", url.PathEscape(auditID))
	resp, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		// Check if it's a "feature not supported" vs "record not found"
		var errResp map[string]interface{}
		if json.Unmarshal(body, &errResp) == nil {
			if msg, ok := errResp["error"].(string); ok && strings.Contains(strings.ToLower(msg), "not found") {
				return nil, nil, &NotSupportedError{Feature: "audit retrieval"}
			}
		}
		return nil, nil, fmt.Errorf("audit record not found: %s", auditID)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var audit AuditRecord
	if err := json.Unmarshal(body, &audit); err != nil {
		return nil, body, fmt.Errorf("failed to parse audit response: %w", err)
	}

	return &audit, body, nil
}

// NotSupportedError indicates a feature is not yet supported by the server.
type NotSupportedError struct {
	Feature string
}

func (e *NotSupportedError) Error() string {
	return fmt.Sprintf("server does not support %s yet", e.Feature)
}

// IsNotSupportedError checks if an error is a NotSupportedError.
func IsNotSupportedError(err error) bool {
	_, ok := err.(*NotSupportedError)
	return ok
}

// PolicyApplyRequest represents a request to apply a policy.
type PolicyApplyRequest struct {
	OrgID       string                 `json:"org_id"`
	Environment string                 `json:"environment"`
	ProjectID   string                 `json:"project_id,omitempty"`
	Policy      map[string]interface{} `json:"policy"`
}

// PolicyApplyResponse represents the response from applying a policy.
type PolicyApplyResponse struct {
	PolicyVersion   string           `json:"policy_version"`
	Status          string           `json:"status"`
	FuzzinessReport *FuzzinessReport `json:"fuzziness_report,omitempty"`
	Message         string           `json:"message,omitempty"`
	CreatedAt       string           `json:"created_at,omitempty"`
}

// FuzzinessReport contains information about policy fuzziness.
type FuzzinessReport struct {
	UnresolvedTerms    []UnresolvedTerm  `json:"unresolved_terms,omitempty"`
	RequiredVariables  []RequiredVar     `json:"required_variables,omitempty"`
	SuggestedMappings  []SuggestedMap    `json:"suggested_mappings,omitempty"`
	FuzzinessScore     float64           `json:"fuzziness_score,omitempty"`
	Summary            string            `json:"summary,omitempty"`
}

// UnresolvedTerm represents an unresolved term in the policy.
type UnresolvedTerm struct {
	Term        string   `json:"term"`
	Location    string   `json:"location,omitempty"`
	Context     string   `json:"context,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// RequiredVar represents a required variable.
type RequiredVar struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

// SuggestedMap represents a suggested mapping for an unresolved term.
type SuggestedMap struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// ApplyPolicyV2 applies a policy with org/env context.
func (c *Client) ApplyPolicyV2(ctx context.Context, req *PolicyApplyRequest) (*PolicyApplyResponse, []byte, error) {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "/v1/policies/apply", req)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, &NotSupportedError{Feature: "policy application"}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var applyResp PolicyApplyResponse
	if err := json.Unmarshal(body, &applyResp); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	return &applyResp, body, nil
}

// PolicyStatusResponse represents the status of a policy version.
type PolicyStatusResponse struct {
	PolicyVersion string   `json:"policy_version"`
	Status        string   `json:"status"`
	Errors        []string `json:"errors,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	Artifacts     []string `json:"artifacts,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	Message       string   `json:"message,omitempty"`
}

// GetPolicyStatus retrieves the status of a policy version.
func (c *Client) GetPolicyStatus(ctx context.Context, policyVersion string) (*PolicyStatusResponse, []byte, error) {
	path := fmt.Sprintf("/v1/policies/%s/status", url.PathEscape(policyVersion))
	resp, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, &NotSupportedError{Feature: "policy status"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var statusResp PolicyStatusResponse
	if err := json.Unmarshal(body, &statusResp); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	return &statusResp, body, nil
}

// PolicyPromoteRequest represents a request to promote a policy.
type PolicyPromoteRequest struct {
	Target string `json:"target"`
}

// PolicyPromoteResponse represents the response from promoting a policy.
type PolicyPromoteResponse struct {
	PolicyVersion string `json:"policy_version"`
	Status        string `json:"status"`
	Target        string `json:"target"`
	Message       string `json:"message,omitempty"`
	ActivatedAt   string `json:"activated_at,omitempty"`
}

// PromotePolicy promotes a policy version to a target state.
func (c *Client) PromotePolicy(ctx context.Context, policyVersion string, target string) (*PolicyPromoteResponse, []byte, error) {
	path := fmt.Sprintf("/v1/policies/%s/promote", url.PathEscape(policyVersion))
	req := PolicyPromoteRequest{Target: target}

	resp, body, err := c.doRequest(ctx, http.MethodPost, path, req)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, &NotSupportedError{Feature: "policy promotion"}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var promoteResp PolicyPromoteResponse
	if err := json.Unmarshal(body, &promoteResp); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	return &promoteResp, body, nil
}

// GetPolicyFuzziness retrieves the fuzziness report for a policy version.
func (c *Client) GetPolicyFuzziness(ctx context.Context, policyVersion string) (*FuzzinessReport, []byte, error) {
	path := fmt.Sprintf("/v1/policies/%s/fuzziness", url.PathEscape(policyVersion))
	resp, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, &NotSupportedError{Feature: "fuzziness report"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var fuzziness FuzzinessReport
	if err := json.Unmarshal(body, &fuzziness); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	return &fuzziness, body, nil
}

// GetActivePolicy retrieves the currently active policy for an org/env.
func (c *Client) GetActivePolicy(ctx context.Context, orgID, env, projectID string) (map[string]interface{}, []byte, error) {
	path := fmt.Sprintf("/v1/policies/active?org_id=%s&environment=%s",
		url.QueryEscape(orgID), url.QueryEscape(env))
	if projectID != "" {
		path += fmt.Sprintf("&project_id=%s", url.QueryEscape(projectID))
	}

	resp, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, &NotSupportedError{Feature: "active policy retrieval"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var policy map[string]interface{}
	if err := json.Unmarshal(body, &policy); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	return policy, body, nil
}

// ToolsetApplyRequest represents a request to apply a toolset.
type ToolsetApplyRequest struct {
	Toolset map[string]interface{} `json:"toolset"`
	Env     string                 `json:"env,omitempty"`
	Status  string                 `json:"status,omitempty"`
}

// ToolsetApplyResponse represents the response from applying a toolset.
type ToolsetApplyResponse struct {
	ToolsetID string   `json:"toolset_id"`
	Revision  string   `json:"revision"`
	Status    string   `json:"status"`
	Envs      []string `json:"envs,omitempty"`
	ToolCount int      `json:"tool_count"`
	Message   string   `json:"message,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

// ApplyToolset applies a toolset to the server.
func (c *Client) ApplyToolset(ctx context.Context, req *ToolsetApplyRequest) (*ToolsetApplyResponse, []byte, error) {
	resp, body, err := c.doRequest(ctx, http.MethodPost, "/v1/toolsets/apply", req)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, &NotSupportedError{Feature: "toolset application"}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, body, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var applyResp ToolsetApplyResponse
	if err := json.Unmarshal(body, &applyResp); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	return &applyResp, body, nil
}
