// Package llm provides clients for OpenAI-compatible LLM endpoints.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"invarity/internal/types"
)

// IntentVoter is the interface for intent alignment voters.
type IntentVoter interface {
	// VoterID returns the unique identifier for this voter.
	VoterID() string
	// Vote evaluates the intent context and returns a vote.
	Vote(ctx context.Context, intentCtx *types.IntentContext) (*types.IntentVoterResult, error)
}

// IntentVoterResponse is the expected JSON output from each intent voter.
type IntentVoterResponse struct {
	Vote       string   `json:"vote"`       // "SAFE", "DENY", "ABSTAIN"
	Confidence float64  `json:"confidence"` // 0.0 - 1.0
	Reasons    []string `json:"reasons"`
}

// baseIntentVoter provides common functionality for intent voters.
type baseIntentVoter struct {
	id     string
	client *Client
}

// VoterID returns the voter's unique identifier.
func (v *baseIntentVoter) VoterID() string {
	return v.id
}

// callModel sends a prompt to the model and parses the response.
func (v *baseIntentVoter) callModel(ctx context.Context, prompt string) (*IntentVoterResponse, error) {
	chatReq := &ChatCompletionRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1, // Low temperature for deterministic responses
		MaxTokens:   256,
		ResponseFormat: &ResponseFormat{
			Type: "json_object",
		},
	}

	resp, err := v.client.ChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("model call failed: %w", err)
	}

	var voterResp IntentVoterResponse
	if err := resp.ExtractJSON(&voterResp); err != nil {
		// Malformed response → ABSTAIN
		return &IntentVoterResponse{
			Vote:       "ABSTAIN",
			Confidence: 0.0,
			Reasons:    []string{"malformed_response"},
		}, nil
	}

	// Validate vote value
	switch voterResp.Vote {
	case "SAFE", "DENY", "ABSTAIN":
		// Valid
	default:
		// Unknown vote → ABSTAIN
		return &IntentVoterResponse{
			Vote:       "ABSTAIN",
			Confidence: 0.0,
			Reasons:    []string{"invalid_vote_value"},
		}, nil
	}

	return &voterResp, nil
}

// parseIntentVote converts a string vote to IntentVote type.
func parseIntentVote(v string) types.IntentVote {
	switch v {
	case "SAFE":
		return types.IntentVoteSafe
	case "DENY":
		return types.IntentVoteDeny
	case "ABSTAIN":
		return types.IntentVoteAbstain
	default:
		return types.IntentVoteAbstain
	}
}

// LiteralIntentVoter (Voter A) checks if the user explicitly requested this action.
type LiteralIntentVoter struct {
	baseIntentVoter
}

// NewLiteralIntentVoter creates a new LiteralIntentVoter.
func NewLiteralIntentVoter(client *Client) *LiteralIntentVoter {
	return &LiteralIntentVoter{
		baseIntentVoter: baseIntentVoter{
			id:     "literal_authorization",
			client: client,
		},
	}
}

// Vote evaluates if the tool call matches what the user explicitly requested.
func (v *LiteralIntentVoter) Vote(ctx context.Context, intentCtx *types.IntentContext) (*types.IntentVoterResult, error) {
	start := time.Now()

	argsStr := string(intentCtx.Args)
	if len(argsStr) > 2000 {
		argsStr = argsStr[:2000] + "...[truncated]"
	}

	prompt := fmt.Sprintf(`Return ONLY valid JSON:
{"vote":"SAFE|DENY|ABSTAIN","confidence":0-1,"reasons":[...]}

SAFE only if the user clearly and explicitly requested this exact action and scope.
DENY if the tool call exceeds, changes, or adds actions the user did not request.
ABSTAIN if intent is ambiguous or authorization is incomplete.

USER_INTENT_SUMMARY:
%s

TOOL:
%s — %s

ARGS:
%s`, intentCtx.IntentSummary, intentCtx.ToolName, intentCtx.ToolDescription, argsStr)

	resp, err := v.callModel(ctx, prompt)
	if err != nil {
		// Network error / timeout → ABSTAIN
		return &types.IntentVoterResult{
			VoterID:    v.id,
			Vote:       types.IntentVoteAbstain,
			Confidence: 0.0,
			Reasons:    []string{"voter_error"},
			Latency:    types.Duration(time.Since(start)),
		}, nil
	}

	return &types.IntentVoterResult{
		VoterID:    v.id,
		Vote:       parseIntentVote(resp.Vote),
		Confidence: resp.Confidence,
		Reasons:    resp.Reasons,
		Latency:    types.Duration(time.Since(start)),
	}, nil
}

// ScopeAuditVoter (Voter B) detects dangerous scope expansion or unsafe defaults.
type ScopeAuditVoter struct {
	baseIntentVoter
}

// NewScopeAuditVoter creates a new ScopeAuditVoter.
func NewScopeAuditVoter(client *Client) *ScopeAuditVoter {
	return &ScopeAuditVoter{
		baseIntentVoter: baseIntentVoter{
			id:     "scope_auditor",
			client: client,
		},
	}
}

// Vote evaluates if the tool call has dangerous scope expansion.
func (v *ScopeAuditVoter) Vote(ctx context.Context, intentCtx *types.IntentContext) (*types.IntentVoterResult, error) {
	start := time.Now()

	argsStr := string(intentCtx.Args)
	if len(argsStr) > 2000 {
		argsStr = argsStr[:2000] + "...[truncated]"
	}

	bulkStr := "false"
	if intentCtx.Bulk {
		bulkStr = "true"
	}

	prompt := fmt.Sprintf(`Return ONLY valid JSON:
{"vote":"SAFE|DENY|ABSTAIN","confidence":0-1,"reasons":[...]}

DENY if arguments imply broader scope than user intent
(e.g. missing filters, wildcards, global effects).
ABSTAIN if scope cannot be confidently determined.

USER_INTENT_SUMMARY:
%s

ARGS:
%s

RISK_HINTS:
operation=%s
resource_scope=%s
side_effect_scope=%s
bulk=%s`, intentCtx.IntentSummary, argsStr, intentCtx.Operation, intentCtx.ResourceScope, intentCtx.SideEffectScope, bulkStr)

	resp, err := v.callModel(ctx, prompt)
	if err != nil {
		// Network error / timeout → ABSTAIN
		return &types.IntentVoterResult{
			VoterID:    v.id,
			Vote:       types.IntentVoteAbstain,
			Confidence: 0.0,
			Reasons:    []string{"voter_error"},
			Latency:    types.Duration(time.Since(start)),
		}, nil
	}

	return &types.IntentVoterResult{
		VoterID:    v.id,
		Vote:       parseIntentVote(resp.Vote),
		Confidence: resp.Confidence,
		Reasons:    resp.Reasons,
		Latency:    types.Duration(time.Since(start)),
	}, nil
}

// PreconditionsVoter (Voter C) checks if required details are missing.
type PreconditionsVoter struct {
	baseIntentVoter
}

// NewPreconditionsVoter creates a new PreconditionsVoter.
func NewPreconditionsVoter(client *Client) *PreconditionsVoter {
	return &PreconditionsVoter{
		baseIntentVoter: baseIntentVoter{
			id:     "preconditions_checker",
			client: client,
		},
	}
}

// Vote evaluates if required preconditions are met.
func (v *PreconditionsVoter) Vote(ctx context.Context, intentCtx *types.IntentContext) (*types.IntentVoterResult, error) {
	start := time.Now()

	argsStr := string(intentCtx.Args)
	if len(argsStr) > 2000 {
		argsStr = argsStr[:2000] + "...[truncated]"
	}

	requiredFieldsJSON, _ := json.Marshal(intentCtx.RequiredFields)

	prompt := fmt.Sprintf(`Return ONLY valid JSON:
{"vote":"SAFE|DENY|ABSTAIN","confidence":0-1,"reasons":[...]}

ABSTAIN if key information is missing or unclear.
DENY if execution would likely violate user intent due to missing preconditions.

USER_INTENT_SUMMARY:
%s

REQUIRED_FIELDS:
%s

ARGS:
%s`, intentCtx.IntentSummary, string(requiredFieldsJSON), argsStr)

	resp, err := v.callModel(ctx, prompt)
	if err != nil {
		// Network error / timeout → ABSTAIN
		return &types.IntentVoterResult{
			VoterID:    v.id,
			Vote:       types.IntentVoteAbstain,
			Confidence: 0.0,
			Reasons:    []string{"voter_error"},
			Latency:    types.Duration(time.Since(start)),
		}, nil
	}

	return &types.IntentVoterResult{
		VoterID:    v.id,
		Vote:       parseIntentVote(resp.Vote),
		Confidence: resp.Confidence,
		Reasons:    resp.Reasons,
		Latency:    types.Duration(time.Since(start)),
	}, nil
}
