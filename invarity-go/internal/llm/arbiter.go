// Package llm provides clients for OpenAI-compatible LLM endpoints.
package llm

import (
	"context"
	"fmt"
	"time"

	"invarity/internal/types"
)

// PolicyArbiter derives facts using Qwen for policy evaluation.
// IMPORTANT: The arbiter MUST NOT make ALLOW/DENY decisions.
// It only derives facts that are then used by deterministic policy evaluation.
type PolicyArbiter struct {
	client *Client
}

// NewPolicyArbiter creates a new policy arbiter.
func NewPolicyArbiter(client *Client) *PolicyArbiter {
	return &PolicyArbiter{client: client}
}

// ArbiterRequest contains the data for fact derivation.
type ArbiterRequest struct {
	UserIntent     string
	ToolCall       types.ToolCall
	Tool           *types.ToolRegistryEntry
	Actor          types.Actor
	Environment    types.Environment
	Context        *types.BoundedContext
	RequiredFacts  []string // Facts that need to be derived
	PolicyClauses  []string // Relevant policy clauses for context
}

// ArbiterResponse is the expected JSON output from Qwen.
type ArbiterResponse struct {
	DerivedFacts []struct {
		Key        string  `json:"key"`
		Value      any     `json:"value"`
		Confidence float64 `json:"confidence"`
		Source     string  `json:"source,omitempty"`
		Reasoning  string  `json:"reasoning,omitempty"`
	} `json:"derived_facts"`
	ClausesUsed []string `json:"clauses_used"`
	Confidence  float64  `json:"confidence"`
}

// Run executes fact derivation.
// IMPORTANT: This method derives facts ONLY. It does not make decisions.
func (a *PolicyArbiter) Run(ctx context.Context, req *ArbiterRequest) (*types.ArbiterResult, error) {
	start := time.Now()

	prompt := a.buildPrompt(req)

	chatReq := &ChatCompletionRequest{
		Messages: []ChatMessage{
			{
				Role: "system",
				Content: `You are a fact-finding assistant for policy evaluation. Your role is to:
1. Analyze the tool call and context
2. Derive factual information needed for policy evaluation
3. Cite sources and explain reasoning

CRITICAL: You must NOT make ALLOW/DENY decisions. Only derive facts.
The policy engine will use your derived facts to make the final decision.

Output only factual findings with confidence scores and sources.`,
			},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1,
		MaxTokens:   1000,
		ResponseFormat: &ResponseFormat{
			Type: "json_object",
		},
	}

	resp, err := a.client.ChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("policy arbiter failed: %w", err)
	}

	var arbiterResp ArbiterResponse
	if err := resp.ExtractJSON(&arbiterResp); err != nil {
		return nil, fmt.Errorf("failed to parse arbiter response: %w", err)
	}

	// Convert to types.DerivedFact
	derivedFacts := make([]types.DerivedFact, len(arbiterResp.DerivedFacts))
	for i, f := range arbiterResp.DerivedFacts {
		derivedFacts[i] = types.DerivedFact{
			Key:        f.Key,
			Value:      f.Value,
			Confidence: f.Confidence,
			Source:     f.Source,
		}
	}

	return &types.ArbiterResult{
		DerivedFacts: derivedFacts,
		ClausesUsed:  arbiterResp.ClausesUsed,
		Confidence:   arbiterResp.Confidence,
		Latency:      types.Duration(time.Since(start)),
	}, nil
}

func (a *PolicyArbiter) buildPrompt(req *ArbiterRequest) string {
	argsStr := string(req.ToolCall.Args)
	if len(argsStr) > 3000 {
		argsStr = argsStr[:3000] + "...[truncated]"
	}

	toolDesc := "Unknown tool"
	if req.Tool != nil {
		toolDesc = fmt.Sprintf("%s: %s", req.Tool.Name, req.Tool.Description)
	}

	requiredFactsStr := ""
	if len(req.RequiredFacts) > 0 {
		requiredFactsStr = fmt.Sprintf("\nRequired Facts to Derive:\n- %v", req.RequiredFacts)
	}

	clausesStr := ""
	if len(req.PolicyClauses) > 0 {
		clausesStr = fmt.Sprintf("\nRelevant Policy Clauses:\n%v", req.PolicyClauses)
	}

	contextStr := ""
	if req.Context != nil {
		if len(req.Context.ConversationHistory) > 0 {
			contextStr = fmt.Sprintf("\nConversation History:\n%v", req.Context.ConversationHistory)
		}
		if len(req.Context.RelevantDocuments) > 0 {
			contextStr += fmt.Sprintf("\nRelevant Documents:\n%v", req.Context.RelevantDocuments)
		}
	}

	return fmt.Sprintf(`Derive facts for policy evaluation. DO NOT make ALLOW/DENY decisions.

User Intent: %s

Tool: %s
Action ID: %s
Arguments:
%s

Actor: %s (role: %s)
Environment: %s
%s
%s
%s

Respond with a JSON object:
{
  "derived_facts": [
    {
      "key": "fact_name",
      "value": <any JSON value>,
      "confidence": 0.0-1.0,
      "source": "where this fact came from",
      "reasoning": "how you derived this"
    }
  ],
  "clauses_used": ["clause_id_1", "clause_id_2"],
  "confidence": 0.0-1.0
}

Derive facts such as:
- verified_recipient: Is the recipient a known/verified entity?
- compliance_check: Does this pass basic compliance rules?
- intent_match: Does the tool call match stated intent?
- scope_appropriate: Is the action scope appropriate for the actor?
- data_sensitivity: What is the sensitivity of data involved?
- business_justification: Is there clear business justification?`,
		req.UserIntent,
		toolDesc,
		req.ToolCall.ActionID,
		argsStr,
		req.Actor.ID,
		req.Actor.Role,
		req.Environment,
		requiredFactsStr,
		clausesStr,
		contextStr,
	)
}
