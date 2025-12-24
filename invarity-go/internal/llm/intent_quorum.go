// Package llm provides clients for OpenAI-compatible LLM endpoints.
package llm

import (
	"context"
	"sync"
	"time"

	"invarity/internal/types"
)

// IntentQuorumConfig configures the intent alignment quorum.
type IntentQuorumConfig struct {
	// Timeout for each voter call (default: 1.5s)
	VoterTimeout time.Duration
}

// DefaultIntentQuorumConfig returns the default configuration.
func DefaultIntentQuorumConfig() *IntentQuorumConfig {
	return &IntentQuorumConfig{
		VoterTimeout: 1500 * time.Millisecond,
	}
}

// IntentQuorum runs the intent alignment quorum with three voters.
type IntentQuorum struct {
	voters []IntentVoter
	config *IntentQuorumConfig
}

// NewIntentQuorum creates a new intent alignment quorum.
// It creates three voters using the provided client:
// - LiteralIntentVoter (Voter A)
// - ScopeAuditVoter (Voter B)
// - PreconditionsVoter (Voter C)
func NewIntentQuorum(client *Client, config *IntentQuorumConfig) *IntentQuorum {
	if config == nil {
		config = DefaultIntentQuorumConfig()
	}

	return &IntentQuorum{
		voters: []IntentVoter{
			NewLiteralIntentVoter(client),
			NewScopeAuditVoter(client),
			NewPreconditionsVoter(client),
		},
		config: config,
	}
}

// IntentQuorumRequest contains the data for intent alignment evaluation.
type IntentQuorumRequest struct {
	UserIntent  string
	ToolCall    types.ToolCall
	Tool        *types.ToolRegistryEntry
	Actor       types.Actor
	Environment types.Environment
	Context     *types.BoundedContext
}

// Run executes the intent alignment quorum and returns the aggregated result.
func (q *IntentQuorum) Run(ctx context.Context, req *IntentQuorumRequest) (*types.IntentAlignmentResult, error) {
	start := time.Now()

	// Build the intent context from the request
	intentCtx := q.buildIntentContext(req)

	// Run all voters in parallel
	var wg sync.WaitGroup
	results := make([]types.IntentVoterResult, len(q.voters))

	for i, voter := range q.voters {
		wg.Add(1)
		go func(idx int, v IntentVoter) {
			defer wg.Done()

			// Create a context with timeout for this voter
			voterCtx, cancel := context.WithTimeout(ctx, q.config.VoterTimeout)
			defer cancel()

			result, err := v.Vote(voterCtx, intentCtx)
			if err != nil {
				// Error → ABSTAIN
				results[idx] = types.IntentVoterResult{
					VoterID:    v.VoterID(),
					Vote:       types.IntentVoteAbstain,
					Confidence: 0.0,
					Reasons:    []string{"voter_error"},
					Latency:    types.Duration(q.config.VoterTimeout),
				}
				return
			}
			results[idx] = *result
		}(i, voter)
	}

	wg.Wait()

	// Aggregate votes using the specified rules
	decision := aggregateIntentVotes(results)

	return &types.IntentAlignmentResult{
		Voters:   results,
		Decision: decision,
		Latency:  types.Duration(time.Since(start)),
	}, nil
}

// buildIntentContext creates an IntentContext from the request.
func (q *IntentQuorum) buildIntentContext(req *IntentQuorumRequest) *types.IntentContext {
	toolName := "unknown"
	toolDesc := ""
	var requiredFields []string
	operation := ""
	resourceScope := ""
	sideEffectScope := ""
	bulk := false

	if req.Tool != nil {
		toolName = req.Tool.Name
		toolDesc = req.Tool.Description

		// Extract risk hints from the tool's risk profile
		if req.Tool.RiskProfile.BulkOperation {
			bulk = true
		}
		resourceScope = req.Tool.RiskProfile.ResourceScope

		// Derive operation from risk profile
		if req.Tool.RiskProfile.MoneyMovement {
			operation = "money_movement"
		} else if req.Tool.RiskProfile.PrivilegeChange {
			operation = "privilege_change"
		} else if req.Tool.RiskProfile.Irreversible {
			operation = "irreversible"
		}

		// Side effect scope based on risk profile
		if req.Tool.RiskProfile.Irreversible {
			sideEffectScope = "permanent"
		} else if req.Tool.RiskProfile.BulkOperation {
			sideEffectScope = "batch"
		} else {
			sideEffectScope = "single"
		}

		// Extract required fields from schema (simplified - in production, parse JSON schema)
		// For now, we'll leave this empty and let the voter infer from args
	}

	return &types.IntentContext{
		IntentSummary:   req.UserIntent,
		ToolName:        toolName,
		ToolDescription: toolDesc,
		Args:            req.ToolCall.Args,
		Operation:       operation,
		ResourceScope:   resourceScope,
		SideEffectScope: sideEffectScope,
		Bulk:            bulk,
		RequiredFields:  requiredFields,
	}
}

// aggregateIntentVotes applies the voting rules:
// - if ALL votes = DENY → DENY
// - if ANY vote = DENY → ESCALATE
// - if ALL votes = SAFE → SAFE
// - any ABSTAIN → ESCALATE
func aggregateIntentVotes(votes []types.IntentVoterResult) types.IntentDecision {
	totalVoters := len(votes)

	// Edge case: no voters = ESCALATE (safe default)
	if totalVoters == 0 {
		return types.IntentDecisionEscalate
	}

	denyCount := 0
	safeCount := 0
	abstainCount := 0

	for _, v := range votes {
		switch v.Vote {
		case types.IntentVoteDeny:
			denyCount++
		case types.IntentVoteSafe:
			safeCount++
		case types.IntentVoteAbstain:
			abstainCount++
		}
	}

	// Rule: if ALL votes = DENY → DENY
	if denyCount == totalVoters {
		return types.IntentDecisionDeny
	}

	// Rule: if ANY vote = DENY → ESCALATE
	if denyCount > 0 {
		return types.IntentDecisionEscalate
	}

	// Rule: any ABSTAIN → ESCALATE
	if abstainCount > 0 {
		return types.IntentDecisionEscalate
	}

	// Rule: if ALL votes = SAFE → SAFE
	if safeCount == totalVoters {
		return types.IntentDecisionSafe
	}

	// Default to ESCALATE (shouldn't reach here, but safety first)
	return types.IntentDecisionEscalate
}

// AggregateIntentVotes is exported for testing.
func AggregateIntentVotes(votes []types.IntentVoterResult) types.IntentDecision {
	return aggregateIntentVotes(votes)
}
