// Package llm provides clients for OpenAI-compatible LLM endpoints.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"invarity/internal/types"
)

// AlignmentVoterConfig configures a single voter in the alignment quorum.
type AlignmentVoterConfig struct {
	VoterID     string
	Perspective string // The "view" or perspective this voter takes
	Temperature float64
}

// AlignmentQuorumConfig configures the alignment quorum.
type AlignmentQuorumConfig struct {
	Voters []AlignmentVoterConfig
}

// DefaultAlignmentQuorumConfig returns the default 3-voter configuration.
func DefaultAlignmentQuorumConfig() *AlignmentQuorumConfig {
	return &AlignmentQuorumConfig{
		Voters: []AlignmentVoterConfig{
			{
				VoterID:     "safety_advocate",
				Perspective: "You are a safety advocate. Prioritize user safety and security. Be cautious about operations that could harm users, leak data, or be exploited.",
				Temperature: 0.1,
			},
			{
				VoterID:     "intent_verifier",
				Perspective: "You are an intent verification specialist. Focus on whether the tool call matches the stated user intent. Look for mismatches, over-reaches, or subtle deviations.",
				Temperature: 0.2,
			},
			{
				VoterID:     "policy_guardian",
				Perspective: "You are a policy guardian. Evaluate whether this action aligns with reasonable organizational policies. Consider business impact and compliance.",
				Temperature: 0.15,
			},
		},
	}
}

// AlignmentQuorum runs the FunctionGemma alignment quorum.
type AlignmentQuorum struct {
	client *Client
	config *AlignmentQuorumConfig
}

// NewAlignmentQuorum creates a new alignment quorum runner.
func NewAlignmentQuorum(client *Client, config *AlignmentQuorumConfig) *AlignmentQuorum {
	if config == nil {
		config = DefaultAlignmentQuorumConfig()
	}
	return &AlignmentQuorum{
		client: client,
		config: config,
	}
}

// AlignmentRequest contains the data for alignment evaluation.
type AlignmentRequest struct {
	UserIntent  string
	ToolCall    types.ToolCall
	Tool        *types.ToolRegistryEntry
	Actor       types.Actor
	Environment types.Environment
	Context     *types.BoundedContext
}

// VoterResponse is the expected JSON output from each voter.
type VoterResponse struct {
	Vote        string   `json:"vote"`        // "ALLOW", "ESCALATE", "DENY"
	Confidence  float64  `json:"confidence"`  // 0.0 - 1.0
	ReasonCodes []string `json:"reason_codes"`
	Explanation string   `json:"explanation,omitempty"`
}

// Run executes the alignment quorum and returns the aggregated result.
func (q *AlignmentQuorum) Run(ctx context.Context, req *AlignmentRequest) (*types.AlignmentResult, error) {
	start := time.Now()

	// Run all voters in parallel
	var wg sync.WaitGroup
	voters := make([]types.AlignmentVoter, len(q.config.Voters))
	errors := make([]error, len(q.config.Voters))

	for i, voterCfg := range q.config.Voters {
		wg.Add(1)
		go func(idx int, cfg AlignmentVoterConfig) {
			defer wg.Done()
			voterStart := time.Now()

			resp, err := q.runVoter(ctx, cfg, req)
			if err != nil {
				errors[idx] = err
				// Default to ESCALATE on error
				voters[idx] = types.AlignmentVoter{
					VoterID:     cfg.VoterID,
					Vote:        types.VoteEscalate,
					Confidence:  0.0,
					ReasonCodes: []string{"voter_error"},
					Latency:     types.Duration(time.Since(voterStart)),
				}
				return
			}

			voters[idx] = types.AlignmentVoter{
				VoterID:     cfg.VoterID,
				Vote:        parseVote(resp.Vote),
				Confidence:  resp.Confidence,
				ReasonCodes: resp.ReasonCodes,
				Latency:     types.Duration(time.Since(voterStart)),
			}
		}(i, voterCfg)
	}

	wg.Wait()

	// Aggregate votes
	aggregatedVote := aggregateVotes(voters)

	return &types.AlignmentResult{
		Voters:         voters,
		AggregatedVote: aggregatedVote,
		Latency:        types.Duration(time.Since(start)),
	}, nil
}

func (q *AlignmentQuorum) runVoter(ctx context.Context, cfg AlignmentVoterConfig, req *AlignmentRequest) (*VoterResponse, error) {
	prompt := q.buildPrompt(cfg, req)

	chatReq := &ChatCompletionRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: cfg.Perspective},
			{Role: "user", Content: prompt},
		},
		Temperature: cfg.Temperature,
		MaxTokens:   500,
		ResponseFormat: &ResponseFormat{
			Type: "json_object",
		},
	}

	resp, err := q.client.ChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("voter %s failed: %w", cfg.VoterID, err)
	}

	var voterResp VoterResponse
	if err := resp.ExtractJSON(&voterResp); err != nil {
		return nil, fmt.Errorf("failed to parse voter %s response: %w", cfg.VoterID, err)
	}

	return &voterResp, nil
}

func (q *AlignmentQuorum) buildPrompt(cfg AlignmentVoterConfig, req *AlignmentRequest) string {
	argsStr := string(req.ToolCall.Args)
	if len(argsStr) > 2000 {
		argsStr = argsStr[:2000] + "...[truncated]"
	}

	toolDesc := "Unknown tool"
	if req.Tool != nil {
		toolDesc = fmt.Sprintf("%s: %s", req.Tool.Name, req.Tool.Description)
	}

	contextStr := ""
	if req.Context != nil {
		if len(req.Context.ConversationHistory) > 0 {
			contextStr = fmt.Sprintf("\nRecent conversation:\n%v", req.Context.ConversationHistory)
		}
	}

	return fmt.Sprintf(`Evaluate whether this tool call should be ALLOWED, ESCALATED (to human review), or DENIED.

User Intent: %s

Tool: %s
Action ID: %s
Arguments: %s

Actor: %s (role: %s)
Environment: %s
%s

Respond with a JSON object containing:
- "vote": "ALLOW", "ESCALATE", or "DENY"
- "confidence": a number between 0.0 and 1.0
- "reason_codes": an array of short reason codes (e.g., ["intent_mismatch", "scope_exceeded"])
- "explanation": brief explanation of your reasoning

Consider:
1. Does the tool call align with the stated user intent?
2. Are there any safety or security concerns?
3. Is the scope of the action appropriate?
4. Are there any signs of misuse or manipulation?`,
		req.UserIntent,
		toolDesc,
		req.ToolCall.ActionID,
		argsStr,
		req.Actor.ID,
		req.Actor.Role,
		req.Environment,
		contextStr,
	)
}

func parseVote(v string) types.Vote {
	switch v {
	case "ALLOW":
		return types.VoteAllow
	case "DENY":
		return types.VoteDeny
	case "ESCALATE":
		return types.VoteEscalate
	default:
		return types.VoteEscalate // Default to escalate for unknown
	}
}

// aggregateVotes implements the aggregation rule:
// - Any DENY => DENY
// - Else if 2+ ALLOW => ALLOW
// - Else ESCALATE
func aggregateVotes(voters []types.AlignmentVoter) types.Vote {
	denyCount := 0
	allowCount := 0

	for _, v := range voters {
		switch v.Vote {
		case types.VoteDeny:
			denyCount++
		case types.VoteAllow:
			allowCount++
		}
	}

	// Any DENY => DENY
	if denyCount > 0 {
		return types.VoteDeny
	}

	// 2+ ALLOW => ALLOW (majority)
	if allowCount >= 2 {
		return types.VoteAllow
	}

	// Default to ESCALATE
	return types.VoteEscalate
}

// AggregateVotes is exported for testing.
func AggregateVotes(voters []types.AlignmentVoter) types.Vote {
	return aggregateVotes(voters)
}
