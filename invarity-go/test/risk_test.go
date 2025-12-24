package test

import (
	"encoding/json"
	"testing"

	"invarity/internal/risk"
	"invarity/internal/types"
)

func TestRiskCompute_LowRisk(t *testing.T) {
	ctx := &risk.ComputeContext{
		Tool: &types.ToolRegistryEntry{
			ActionID: "read_file",
			RiskProfile: types.RiskProfile{
				MoneyMovement:   false,
				PrivilegeChange: false,
				Irreversible:    false,
				BulkOperation:   false,
				ResourceScope:   "single",
				DataClass:       "public",
			},
		},
		Args:        json.RawMessage(`{"path": "/tmp/test.txt"}`),
		Environment: types.EnvDevelopment,
	}

	result := risk.Compute(ctx)

	if result.Level != types.RiskLow {
		t.Errorf("expected LOW risk, got %s", result.Level)
	}
}

func TestRiskCompute_HighRisk_MoneyMovement(t *testing.T) {
	ctx := &risk.ComputeContext{
		Tool: &types.ToolRegistryEntry{
			ActionID: "transfer_funds",
			RiskProfile: types.RiskProfile{
				MoneyMovement:   true,
				PrivilegeChange: false,
				Irreversible:    true,
				BulkOperation:   false,
				ResourceScope:   "single",
				DataClass:       "confidential",
			},
		},
		Args:        json.RawMessage(`{"amount": 50000, "currency": "USD"}`),
		Environment: types.EnvProduction,
	}

	result := risk.Compute(ctx)

	if result.Level != types.RiskCritical {
		t.Errorf("expected CRITICAL risk, got %s (score: %d)", result.Level, result.Score)
	}

	// Check factors
	hasMoneyMovement := false
	hasProduction := false
	for _, f := range result.Factors {
		if f == "money_movement" {
			hasMoneyMovement = true
		}
		if f == "production_environment" {
			hasProduction = true
		}
	}

	if !hasMoneyMovement {
		t.Error("expected money_movement factor")
	}
	if !hasProduction {
		t.Error("expected production_environment factor")
	}
}

func TestRiskCompute_MediumRisk_BulkOperation(t *testing.T) {
	ctx := &risk.ComputeContext{
		Tool: &types.ToolRegistryEntry{
			ActionID: "bulk_update",
			RiskProfile: types.RiskProfile{
				MoneyMovement:   false,
				PrivilegeChange: false,
				Irreversible:    false,
				BulkOperation:   true,
				ResourceScope:   "tenant",
				DataClass:       "internal",
			},
		},
		Args:        json.RawMessage(`{"limit": 500}`),
		Environment: types.EnvStaging,
	}

	result := risk.Compute(ctx)

	// Bulk (20) + tenant scope (10) + internal data (5) + staging (5) = 40 = HIGH
	if result.Level != types.RiskHigh {
		t.Errorf("expected HIGH risk, got %s (score: %d)", result.Level, result.Score)
	}
}

func TestRiskCompute_OverrideLevel(t *testing.T) {
	ctx := &risk.ComputeContext{
		Tool: &types.ToolRegistryEntry{
			ActionID: "admin_action",
			RiskProfile: types.RiskProfile{
				BaseRiskLevel: "CRITICAL",
			},
		},
		Args:        json.RawMessage(`{}`),
		Environment: types.EnvDevelopment,
	}

	result := risk.Compute(ctx)

	if result.Level != types.RiskCritical {
		t.Errorf("expected CRITICAL (override), got %s", result.Level)
	}
}

func TestRiskCompute_NilTool(t *testing.T) {
	ctx := &risk.ComputeContext{
		Tool:        nil,
		Args:        json.RawMessage(`{}`),
		Environment: types.EnvDevelopment,
	}

	result := risk.Compute(ctx)

	if result.Level != types.RiskMedium {
		t.Errorf("expected MEDIUM for unknown tool, got %s", result.Level)
	}
}

func TestCompareRiskLevels(t *testing.T) {
	tests := []struct {
		a, b     types.RiskLevel
		expected bool
	}{
		{types.RiskCritical, types.RiskLow, true},
		{types.RiskHigh, types.RiskMedium, true},
		{types.RiskMedium, types.RiskMedium, true},
		{types.RiskLow, types.RiskHigh, false},
		{types.RiskMedium, types.RiskCritical, false},
	}

	for _, tt := range tests {
		result := risk.CompareRiskLevels(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("CompareRiskLevels(%s, %s) = %v, want %v", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestMaxRiskLevel(t *testing.T) {
	tests := []struct {
		a, b     types.RiskLevel
		expected types.RiskLevel
	}{
		{types.RiskLow, types.RiskHigh, types.RiskHigh},
		{types.RiskCritical, types.RiskMedium, types.RiskCritical},
		{types.RiskMedium, types.RiskMedium, types.RiskMedium},
	}

	for _, tt := range tests {
		result := risk.MaxRiskLevel(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("MaxRiskLevel(%s, %s) = %s, want %s", tt.a, tt.b, result, tt.expected)
		}
	}
}
