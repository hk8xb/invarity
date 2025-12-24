package test

import (
	"context"
	"encoding/json"
	"testing"

	"invarity/internal/policy"
	"invarity/internal/types"
)

func TestPolicyEvaluator_DenyRule(t *testing.T) {
	store := policy.NewInMemoryStore()

	// Add a test policy
	bundle := &types.PolicyBundle{
		OrgID:   "test-org",
		Version: "1.0.0",
		Rules: []types.PolicyRule{
			{
				ID:       "deny-delete-prod",
				Name:     "Deny Delete in Production",
				Priority: 100,
				Conditions: json.RawMessage(`{
					"all": [
						{"env": "production"},
						{"action_prefix": "delete"}
					]
				}`),
				Effect: "deny",
			},
		},
	}
	_ = store.PutCompiledPolicy(context.Background(), bundle)

	evaluator := policy.NewEvaluator(store)

	ctx := &policy.EvaluationContext{
		OrgID:       "test-org",
		Environment: types.EnvProduction,
		ToolCall: types.ToolCall{
			ActionID: "delete_user",
		},
	}

	result, err := evaluator.Evaluate(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != types.PolicyStatusDeny {
		t.Errorf("expected DENY, got %s", result.Status)
	}

	if len(result.MatchedRules) == 0 || result.MatchedRules[0] != "deny-delete-prod" {
		t.Errorf("expected matched rule 'deny-delete-prod', got %v", result.MatchedRules)
	}
}

func TestPolicyEvaluator_AllowRule(t *testing.T) {
	store := policy.NewInMemoryStore()

	bundle := &types.PolicyBundle{
		OrgID:   "test-org",
		Version: "1.0.0",
		Rules: []types.PolicyRule{
			{
				ID:       "allow-read",
				Name:     "Allow Read Operations",
				Priority: 50,
				Conditions: json.RawMessage(`{
					"action_prefix": "read"
				}`),
				Effect: "allow",
			},
		},
	}
	_ = store.PutCompiledPolicy(context.Background(), bundle)

	evaluator := policy.NewEvaluator(store)

	ctx := &policy.EvaluationContext{
		OrgID:       "test-org",
		Environment: types.EnvProduction,
		ToolCall: types.ToolCall{
			ActionID: "read_file",
		},
	}

	result, err := evaluator.Evaluate(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != types.PolicyStatusCovered {
		t.Errorf("expected COVERED, got %s", result.Status)
	}
}

func TestPolicyEvaluator_Uncovered(t *testing.T) {
	store := policy.NewInMemoryStore()

	bundle := &types.PolicyBundle{
		OrgID:   "test-org",
		Version: "1.0.0",
		Rules: []types.PolicyRule{
			{
				ID:       "allow-read",
				Name:     "Allow Read Operations",
				Priority: 50,
				Conditions: json.RawMessage(`{
					"action_prefix": "read"
				}`),
				Effect: "allow",
			},
		},
	}
	_ = store.PutCompiledPolicy(context.Background(), bundle)

	evaluator := policy.NewEvaluator(store)

	// Action that doesn't match any rule
	ctx := &policy.EvaluationContext{
		OrgID:       "test-org",
		Environment: types.EnvProduction,
		ToolCall: types.ToolCall{
			ActionID: "custom_action",
		},
	}

	result, err := evaluator.Evaluate(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != types.PolicyStatusUncovered {
		t.Errorf("expected UNCOVERED, got %s", result.Status)
	}
}

func TestPolicyEvaluator_RequiresFacts(t *testing.T) {
	store := policy.NewInMemoryStore()

	bundle := &types.PolicyBundle{
		OrgID:   "test-org",
		Version: "1.0.0",
		Rules: []types.PolicyRule{
			{
				ID:       "verify-transfer",
				Name:     "Verify Transfer",
				Priority: 80,
				Conditions: json.RawMessage(`{
					"action_id": "transfer_funds"
				}`),
				Effect:       "allow",
				RequiresFct: []string{"verified_recipient"},
			},
		},
	}
	_ = store.PutCompiledPolicy(context.Background(), bundle)

	evaluator := policy.NewEvaluator(store)

	ctx := &policy.EvaluationContext{
		OrgID:       "test-org",
		Environment: types.EnvProduction,
		ToolCall: types.ToolCall{
			ActionID: "transfer_funds",
		},
	}

	result, err := evaluator.Evaluate(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != types.PolicyStatusRequiresFact {
		t.Errorf("expected REQUIRES_FACTS, got %s", result.Status)
	}

	if len(result.RequiresFact) == 0 {
		t.Error("expected required facts to be set")
	}
}

func TestPolicyEvaluator_WithFacts(t *testing.T) {
	store := policy.NewInMemoryStore()

	bundle := &types.PolicyBundle{
		OrgID:   "test-org",
		Version: "1.0.0",
		Rules: []types.PolicyRule{
			{
				ID:       "verify-transfer",
				Name:     "Verify Transfer",
				Priority: 80,
				Conditions: json.RawMessage(`{
					"action_id": "transfer_funds"
				}`),
				Effect:       "allow",
				RequiresFct: []string{"verified_recipient"},
			},
		},
	}
	_ = store.PutCompiledPolicy(context.Background(), bundle)

	evaluator := policy.NewEvaluator(store)

	ctx := &policy.EvaluationContext{
		OrgID:       "test-org",
		Environment: types.EnvProduction,
		ToolCall: types.ToolCall{
			ActionID: "transfer_funds",
		},
		DerivedFact: map[string]any{
			"verified_recipient": true,
		},
	}

	result, err := evaluator.EvaluateWithFacts(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != types.PolicyStatusCovered {
		t.Errorf("expected COVERED with facts, got %s", result.Status)
	}
}

func TestPolicyEvaluator_PriorityOrder(t *testing.T) {
	store := policy.NewInMemoryStore()

	bundle := &types.PolicyBundle{
		OrgID:   "test-org",
		Version: "1.0.0",
		Rules: []types.PolicyRule{
			{
				ID:       "low-priority-allow",
				Name:     "Low Priority Allow",
				Priority: 10,
				Conditions: json.RawMessage(`{
					"action_prefix": "test"
				}`),
				Effect: "allow",
			},
			{
				ID:       "high-priority-deny",
				Name:     "High Priority Deny",
				Priority: 100,
				Conditions: json.RawMessage(`{
					"action_prefix": "test"
				}`),
				Effect: "deny",
			},
		},
	}
	_ = store.PutCompiledPolicy(context.Background(), bundle)

	evaluator := policy.NewEvaluator(store)

	ctx := &policy.EvaluationContext{
		OrgID: "test-org",
		ToolCall: types.ToolCall{
			ActionID: "test_action",
		},
	}

	result, err := evaluator.Evaluate(context.Background(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Higher priority deny should win
	if result.Status != types.PolicyStatusDeny {
		t.Errorf("expected DENY (high priority), got %s", result.Status)
	}
}

func TestPolicyStore_Cache(t *testing.T) {
	inner := policy.NewInMemoryStore()
	cached := policy.NewCachedStore(inner, 0) // 0 TTL for testing

	bundle := &types.PolicyBundle{
		OrgID:   "test-org",
		Version: "1.0.0",
		Rules:   []types.PolicyRule{},
	}

	// Put through cached store
	err := cached.PutCompiledPolicy(context.Background(), bundle)
	if err != nil {
		t.Fatalf("failed to put policy: %v", err)
	}

	// Get should work
	retrieved, err := cached.GetCompiledPolicy(context.Background(), "test-org")
	if err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	if retrieved.OrgID != "test-org" {
		t.Errorf("got org %s, want test-org", retrieved.OrgID)
	}
}
