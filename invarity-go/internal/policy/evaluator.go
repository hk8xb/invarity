// Package policy provides policy storage and evaluation.
package policy

import (
	"context"
	"encoding/json"
	"strings"

	"invarity/internal/types"
)

// EvaluationContext contains all data needed for policy evaluation.
type EvaluationContext struct {
	OrgID       string
	Actor       types.Actor
	Environment types.Environment
	ToolCall    types.ToolCall
	Tool        *types.ToolRegistryEntry
	RiskLevel   types.RiskLevel
	UserIntent  string
	DerivedFact map[string]any // Facts derived by arbiter (Pass 2 only)
}

// EvaluationResult contains the result of policy evaluation.
type EvaluationResult struct {
	Status       types.PolicyStatus
	MatchedRules []string
	RequiresFact []string
	DenyReasons  []string
}

// Evaluator evaluates policy rules against a context.
type Evaluator struct {
	store Store
}

// NewEvaluator creates a new policy evaluator.
func NewEvaluator(store Store) *Evaluator {
	return &Evaluator{store: store}
}

// Evaluate runs policy evaluation (Pass 1 - no derived facts).
func (e *Evaluator) Evaluate(ctx context.Context, evalCtx *EvaluationContext) (*EvaluationResult, error) {
	return e.evaluate(ctx, evalCtx, false)
}

// EvaluateWithFacts runs policy evaluation (Pass 2 - with derived facts).
func (e *Evaluator) EvaluateWithFacts(ctx context.Context, evalCtx *EvaluationContext) (*EvaluationResult, error) {
	return e.evaluate(ctx, evalCtx, true)
}

func (e *Evaluator) evaluate(ctx context.Context, evalCtx *EvaluationContext, useFacts bool) (*EvaluationResult, error) {
	bundle, err := e.store.GetCompiledPolicy(ctx, evalCtx.OrgID)
	if err != nil {
		if err == ErrPolicyNotFound {
			// No policy = uncovered
			return &EvaluationResult{
				Status: types.PolicyStatusUncovered,
			}, nil
		}
		return nil, err
	}

	result := &EvaluationResult{
		MatchedRules: make([]string, 0),
		RequiresFact: make([]string, 0),
		DenyReasons:  make([]string, 0),
	}

	// Evaluate rules in priority order (higher priority first)
	sortedRules := sortRulesByPriority(bundle.Rules)

	var hasCoverage bool
	var requiresFacts bool

	for _, rule := range sortedRules {
		matched, needsFacts, err := e.evaluateRule(rule, evalCtx, useFacts)
		if err != nil {
			continue // Skip rules that fail to evaluate
		}

		if needsFacts && !useFacts {
			requiresFacts = true
			result.RequiresFact = append(result.RequiresFact, rule.RequiresFct...)
			continue
		}

		if matched {
			result.MatchedRules = append(result.MatchedRules, rule.ID)
			hasCoverage = true

			switch rule.Effect {
			case "deny":
				result.Status = types.PolicyStatusDeny
				result.DenyReasons = append(result.DenyReasons, rule.Name)
				return result, nil // Deny is terminal
			case "allow":
				result.Status = types.PolicyStatusCovered
				return result, nil // Allow is terminal
			case "escalate":
				// Continue checking for deny rules
				if result.Status != types.PolicyStatusDeny {
					result.Status = types.PolicyStatusCovered
				}
			}
		}
	}

	// Determine final status
	if result.Status == types.PolicyStatusDeny {
		return result, nil
	}

	if requiresFacts && !useFacts {
		result.Status = types.PolicyStatusRequiresFact
		return result, nil
	}

	if hasCoverage {
		result.Status = types.PolicyStatusCovered
	} else {
		result.Status = types.PolicyStatusUncovered
	}

	return result, nil
}

func (e *Evaluator) evaluateRule(rule types.PolicyRule, evalCtx *EvaluationContext, useFacts bool) (matched bool, needsFacts bool, err error) {
	// Check if rule requires facts we don't have
	if len(rule.RequiresFct) > 0 && !useFacts {
		// Check if any required fact is missing
		if evalCtx.DerivedFact == nil {
			return false, true, nil
		}
		for _, factKey := range rule.RequiresFct {
			if _, ok := evalCtx.DerivedFact[factKey]; !ok {
				return false, true, nil
			}
		}
	}

	// Parse and evaluate conditions
	var conditions map[string]any
	if err := json.Unmarshal(rule.Conditions, &conditions); err != nil {
		return false, false, err
	}

	matched = e.evaluateConditions(conditions, evalCtx)
	return matched, false, nil
}

func (e *Evaluator) evaluateConditions(conditions map[string]any, evalCtx *EvaluationContext) bool {
	for key, value := range conditions {
		switch key {
		case "all":
			// All conditions must match
			items, ok := value.([]any)
			if !ok {
				return false
			}
			for _, item := range items {
				itemMap, ok := item.(map[string]any)
				if !ok {
					return false
				}
				if !e.evaluateConditions(itemMap, evalCtx) {
					return false
				}
			}
			return true

		case "any":
			// Any condition must match
			items, ok := value.([]any)
			if !ok {
				return false
			}
			for _, item := range items {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if e.evaluateConditions(itemMap, evalCtx) {
					return true
				}
			}
			return false

		case "not":
			// Negate the condition
			itemMap, ok := value.(map[string]any)
			if !ok {
				return false
			}
			return !e.evaluateConditions(itemMap, evalCtx)

		case "env":
			// Match environment
			envStr, ok := value.(string)
			return ok && string(evalCtx.Environment) == envStr

		case "action_prefix":
			// Match action ID prefix
			prefix, ok := value.(string)
			return ok && strings.HasPrefix(evalCtx.ToolCall.ActionID, prefix)

		case "action_id":
			// Exact action ID match
			actionID, ok := value.(string)
			return ok && evalCtx.ToolCall.ActionID == actionID

		case "risk_level":
			// Match risk level
			level, ok := value.(string)
			return ok && string(evalCtx.RiskLevel) == level

		case "money_movement":
			// Check money movement flag
			if evalCtx.Tool == nil {
				return false
			}
			expected, ok := value.(bool)
			return ok && evalCtx.Tool.RiskProfile.MoneyMovement == expected

		case "privilege_change":
			// Check privilege change flag
			if evalCtx.Tool == nil {
				return false
			}
			expected, ok := value.(bool)
			return ok && evalCtx.Tool.RiskProfile.PrivilegeChange == expected

		case "irreversible":
			// Check irreversible flag
			if evalCtx.Tool == nil {
				return false
			}
			expected, ok := value.(bool)
			return ok && evalCtx.Tool.RiskProfile.Irreversible == expected

		case "bulk_operation":
			// Check bulk operation flag
			if evalCtx.Tool == nil {
				return false
			}
			expected, ok := value.(bool)
			return ok && evalCtx.Tool.RiskProfile.BulkOperation == expected

		case "actor_role":
			// Match actor role
			role, ok := value.(string)
			return ok && evalCtx.Actor.Role == role

		case "has_approval":
			// Check if approval exists (stub - always false for now)
			expected, ok := value.(bool)
			return ok && !expected // Always assume no approval

		case "amount_gt":
			// Check if amount in args is greater than threshold
			threshold, ok := value.(float64)
			if !ok {
				return false
			}
			return e.checkAmountGT(evalCtx, threshold)

		case "data_class":
			// Match data classification
			if evalCtx.Tool == nil {
				return false
			}
			class, ok := value.(string)
			return ok && evalCtx.Tool.RiskProfile.DataClass == class

		case "fact":
			// Check derived fact
			factCheck, ok := value.(map[string]any)
			if !ok || evalCtx.DerivedFact == nil {
				return false
			}
			return e.checkFact(factCheck, evalCtx.DerivedFact)

		default:
			// Unknown condition - skip
			continue
		}
	}

	return true // Empty conditions = match
}

func (e *Evaluator) checkAmountGT(evalCtx *EvaluationContext, threshold float64) bool {
	var args map[string]any
	if err := json.Unmarshal(evalCtx.ToolCall.Args, &args); err != nil {
		return false
	}

	amount, ok := args["amount"].(float64)
	return ok && amount > threshold
}

func (e *Evaluator) checkFact(factCheck, derivedFacts map[string]any) bool {
	for key, expected := range factCheck {
		actual, ok := derivedFacts[key]
		if !ok {
			return false
		}
		if actual != expected {
			return false
		}
	}
	return true
}

// sortRulesByPriority returns rules sorted by priority (highest first).
func sortRulesByPriority(rules []types.PolicyRule) []types.PolicyRule {
	sorted := make([]types.PolicyRule, len(rules))
	copy(sorted, rules)

	// Simple bubble sort (rules list is typically small)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j].Priority < sorted[j+1].Priority {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted
}
