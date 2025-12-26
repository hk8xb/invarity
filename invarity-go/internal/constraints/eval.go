// Package constraints provides deterministic constraint evaluation for tool calls.
package constraints

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"invarity/internal/types"
)

// EvalResult contains the result of constraint evaluation.
type EvalResult struct {
	Passed       bool     `json:"passed"`
	Violations   []string `json:"violations,omitempty"`
	MatchedRules []string `json:"matched_rules,omitempty"`
}

// Evaluator evaluates deterministic tool-level constraints.
type Evaluator struct{}

// NewEvaluator creates a new constraint evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// Evaluate runs deterministic constraint evaluation on a tool call.
// Constraints are defined at the tool level and are 100% deterministically evaluable.
// This replaces the old risk scoring and policy DSL.
func (e *Evaluator) Evaluate(ctx context.Context, tool *types.ToolRegistryEntry, req *types.ToolCallRequest) (*EvalResult, error) {
	if tool == nil {
		return &EvalResult{
			Passed:     false,
			Violations: []string{"tool_not_found"},
		}, nil
	}

	result := &EvalResult{
		Passed:       true,
		Violations:   make([]string, 0),
		MatchedRules: make([]string, 0),
	}

	constraints := tool.Constraints

	// Check environment restrictions
	if len(constraints.AllowedEnvs) > 0 {
		allowed := false
		for _, env := range constraints.AllowedEnvs {
			if env == string(req.Environment) {
				allowed = true
				break
			}
		}
		if !allowed {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("environment_not_allowed:%s", req.Environment))
			result.MatchedRules = append(result.MatchedRules, "env_restriction")
		}
	}

	// Check denied environments
	for _, env := range constraints.DeniedEnvs {
		if env == string(req.Environment) {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("environment_denied:%s", req.Environment))
			result.MatchedRules = append(result.MatchedRules, "env_deny")
		}
	}

	// Check allowed roles
	if len(constraints.AllowedRoles) > 0 {
		allowed := false
		for _, role := range constraints.AllowedRoles {
			if role == req.Actor.Role {
				allowed = true
				break
			}
		}
		if !allowed {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("role_not_allowed:%s", req.Actor.Role))
			result.MatchedRules = append(result.MatchedRules, "role_restriction")
		}
	}

	// Check denied roles
	for _, role := range constraints.DeniedRoles {
		if role == req.Actor.Role {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("role_denied:%s", req.Actor.Role))
			result.MatchedRules = append(result.MatchedRules, "role_deny")
		}
	}

	// Check max amount constraint
	if constraints.MaxAmount != nil {
		amount := extractAmount(req.ToolCall.Args)
		if amount > *constraints.MaxAmount {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("amount_exceeds_max:%.2f>%.2f", amount, *constraints.MaxAmount))
			result.MatchedRules = append(result.MatchedRules, "max_amount")
		}
	}

	// Check max batch size constraint
	if constraints.MaxBatchSize != nil {
		batchSize := extractBatchSize(req.ToolCall.Args)
		if batchSize > *constraints.MaxBatchSize {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("batch_size_exceeds_max:%d>%d", batchSize, *constraints.MaxBatchSize))
			result.MatchedRules = append(result.MatchedRules, "max_batch_size")
		}
	}

	// Check required fields in args
	for _, field := range constraints.RequiredFields {
		if !hasField(req.ToolCall.Args, field) {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("missing_required_field:%s", field))
			result.MatchedRules = append(result.MatchedRules, "required_field")
		}
	}

	// Check denied argument patterns
	for _, pattern := range constraints.DeniedArgPatterns {
		if matchesArgPattern(req.ToolCall.Args, pattern) {
			result.Passed = false
			result.Violations = append(result.Violations, fmt.Sprintf("denied_arg_pattern:%s", pattern))
			result.MatchedRules = append(result.MatchedRules, "denied_arg_pattern")
		}
	}

	return result, nil
}

// extractAmount attempts to extract a monetary amount from args.
func extractAmount(args json.RawMessage) float64 {
	var parsed map[string]any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return 0
	}

	// Check common amount field names
	for _, key := range []string{"amount", "value", "total", "sum"} {
		if val, ok := parsed[key]; ok {
			switch v := val.(type) {
			case float64:
				return v
			case int:
				return float64(v)
			}
		}
	}

	return 0
}

// extractBatchSize attempts to extract batch size from args.
func extractBatchSize(args json.RawMessage) int {
	var parsed map[string]any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return 0
	}

	// Check common batch size field names
	for _, key := range []string{"limit", "batch_size", "count", "size"} {
		if val, ok := parsed[key]; ok {
			switch v := val.(type) {
			case float64:
				return int(v)
			case int:
				return v
			}
		}
	}

	// Check for array fields that might indicate batch size
	for _, key := range []string{"ids", "items", "records", "users"} {
		if val, ok := parsed[key]; ok {
			if arr, ok := val.([]any); ok {
				return len(arr)
			}
		}
	}

	return 0
}

// hasField checks if a field exists in the args JSON.
func hasField(args json.RawMessage, field string) bool {
	var parsed map[string]any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return false
	}

	// Support nested fields with dot notation (e.g., "user.id")
	parts := strings.Split(field, ".")
	current := parsed

	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return false
		}
		if i == len(parts)-1 {
			return val != nil
		}
		if nested, ok := val.(map[string]any); ok {
			current = nested
		} else {
			return false
		}
	}

	return true
}

// matchesArgPattern checks if any arg value matches a denied pattern.
// Pattern format: "field=value" or "field:contains:substring"
func matchesArgPattern(args json.RawMessage, pattern string) bool {
	var parsed map[string]any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return false
	}

	// Parse pattern
	if strings.Contains(pattern, ":contains:") {
		parts := strings.SplitN(pattern, ":contains:", 2)
		if len(parts) != 2 {
			return false
		}
		field, substr := parts[0], parts[1]
		if val, ok := parsed[field]; ok {
			if str, ok := val.(string); ok {
				return strings.Contains(str, substr)
			}
		}
		return false
	}

	if strings.Contains(pattern, "=") {
		parts := strings.SplitN(pattern, "=", 2)
		if len(parts) != 2 {
			return false
		}
		field, expected := parts[0], parts[1]
		if val, ok := parsed[field]; ok {
			return fmt.Sprintf("%v", val) == expected
		}
		return false
	}

	return false
}
