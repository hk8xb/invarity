// Package risk provides deterministic risk computation for tool calls.
package risk

import (
	"encoding/json"

	"invarity/internal/types"
)

// ComputeContext contains data needed for risk computation.
type ComputeContext struct {
	Tool        *types.ToolRegistryEntry
	Args        json.RawMessage
	Environment types.Environment
	Actor       types.Actor
}

// ComputeResult contains the computed risk level and contributing factors.
type ComputeResult struct {
	Level   types.RiskLevel
	Score   int
	Factors []string
}

// Compute calculates the base risk level for a tool call.
// This is a deterministic computation based on:
// - Tool's risk profile
// - Argument values (amounts, scopes, etc.)
// - Environment
// - Actor role
func Compute(ctx *ComputeContext) *ComputeResult {
	result := &ComputeResult{
		Score:   0,
		Factors: make([]string, 0),
	}

	if ctx.Tool == nil {
		result.Level = types.RiskMedium
		result.Factors = append(result.Factors, "unknown_tool")
		return result
	}

	profile := ctx.Tool.RiskProfile

	// Check for override
	if profile.BaseRiskLevel != "" {
		switch profile.BaseRiskLevel {
		case "LOW":
			result.Level = types.RiskLow
		case "MEDIUM":
			result.Level = types.RiskMedium
		case "HIGH":
			result.Level = types.RiskHigh
		case "CRITICAL":
			result.Level = types.RiskCritical
		}
		result.Factors = append(result.Factors, "base_risk_override")
		return result
	}

	// Money movement is high risk
	if profile.MoneyMovement {
		result.Score += 30
		result.Factors = append(result.Factors, "money_movement")

		// Check amount threshold
		amount := extractAmount(ctx.Args)
		if amount > 100000 {
			result.Score += 20
			result.Factors = append(result.Factors, "high_value_transaction")
		} else if amount > 10000 {
			result.Score += 10
			result.Factors = append(result.Factors, "significant_value_transaction")
		}
	}

	// Privilege changes are high risk
	if profile.PrivilegeChange {
		result.Score += 25
		result.Factors = append(result.Factors, "privilege_change")
	}

	// Irreversible operations add risk
	if profile.Irreversible {
		result.Score += 15
		result.Factors = append(result.Factors, "irreversible")
	}

	// Bulk operations add risk
	if profile.BulkOperation {
		result.Score += 20
		result.Factors = append(result.Factors, "bulk_operation")

		// Check for large batch sizes
		batchSize := extractBatchSize(ctx.Args)
		if batchSize > 1000 {
			result.Score += 15
			result.Factors = append(result.Factors, "large_batch")
		} else if batchSize > 100 {
			result.Score += 5
			result.Factors = append(result.Factors, "medium_batch")
		}
	}

	// Resource scope
	switch profile.ResourceScope {
	case "global":
		result.Score += 20
		result.Factors = append(result.Factors, "global_scope")
	case "tenant":
		result.Score += 10
		result.Factors = append(result.Factors, "tenant_scope")
	case "single":
		// No additional score
	}

	// Data classification
	switch profile.DataClass {
	case "restricted":
		result.Score += 20
		result.Factors = append(result.Factors, "restricted_data")
	case "confidential":
		result.Score += 15
		result.Factors = append(result.Factors, "confidential_data")
	case "internal":
		result.Score += 5
		result.Factors = append(result.Factors, "internal_data")
	case "public":
		// No additional score
	}

	// Environment factors
	switch ctx.Environment {
	case types.EnvProduction:
		result.Score += 15
		result.Factors = append(result.Factors, "production_environment")
	case types.EnvStaging:
		result.Score += 5
		result.Factors = append(result.Factors, "staging_environment")
	}

	// Check environment restrictions
	if len(profile.AllowedEnvs) > 0 {
		allowed := false
		for _, env := range profile.AllowedEnvs {
			if env == string(ctx.Environment) {
				allowed = true
				break
			}
		}
		if !allowed {
			result.Score += 30
			result.Factors = append(result.Factors, "environment_not_allowed")
		}
	}

	// Requires approval flag
	if profile.RequiresApproval {
		result.Score += 10
		result.Factors = append(result.Factors, "requires_approval")
	}

	// Convert score to risk level
	result.Level = scoreToLevel(result.Score)

	return result
}

// scoreToLevel converts a numeric score to a risk level.
func scoreToLevel(score int) types.RiskLevel {
	switch {
	case score >= 60:
		return types.RiskCritical
	case score >= 40:
		return types.RiskHigh
	case score >= 20:
		return types.RiskMedium
	default:
		return types.RiskLow
	}
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

// CompareRiskLevels returns true if a >= b.
func CompareRiskLevels(a, b types.RiskLevel) bool {
	return a.Value() >= b.Value()
}

// MaxRiskLevel returns the higher of two risk levels.
func MaxRiskLevel(a, b types.RiskLevel) types.RiskLevel {
	if a.Value() >= b.Value() {
		return a
	}
	return b
}
