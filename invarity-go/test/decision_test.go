package test

import (
	"testing"

	"invarity/internal/types"
)

// TestDecisionPrecedence verifies that DENY > ESCALATE > ALLOW
func TestDecisionPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		decisions []types.Decision
		expected  types.Decision
	}{
		{
			name:      "all allow",
			decisions: []types.Decision{types.DecisionAllow, types.DecisionAllow, types.DecisionAllow},
			expected:  types.DecisionAllow,
		},
		{
			name:      "one deny dominates",
			decisions: []types.Decision{types.DecisionAllow, types.DecisionDeny, types.DecisionAllow},
			expected:  types.DecisionDeny,
		},
		{
			name:      "deny overrides escalate",
			decisions: []types.Decision{types.DecisionEscalate, types.DecisionDeny, types.DecisionAllow},
			expected:  types.DecisionDeny,
		},
		{
			name:      "escalate overrides allow",
			decisions: []types.Decision{types.DecisionAllow, types.DecisionEscalate, types.DecisionAllow},
			expected:  types.DecisionEscalate,
		},
		{
			name:      "all escalate",
			decisions: []types.Decision{types.DecisionEscalate, types.DecisionEscalate},
			expected:  types.DecisionEscalate,
		},
		{
			name:      "single deny",
			decisions: []types.Decision{types.DecisionDeny},
			expected:  types.DecisionDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := aggregateDecisions(tt.decisions)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

// aggregateDecisions implements the decision precedence logic
// This matches the logic in the firewall pipeline
func aggregateDecisions(decisions []types.Decision) types.Decision {
	hasDeny := false
	hasEscalate := false

	for _, d := range decisions {
		switch d {
		case types.DecisionDeny:
			hasDeny = true
		case types.DecisionEscalate:
			hasEscalate = true
		}
	}

	if hasDeny {
		return types.DecisionDeny
	}
	if hasEscalate {
		return types.DecisionEscalate
	}
	return types.DecisionAllow
}

// TestDecisionValues verifies the decision constants
func TestDecisionValues(t *testing.T) {
	if types.DecisionAllow != "ALLOW" {
		t.Errorf("DecisionAllow = %s, want ALLOW", types.DecisionAllow)
	}
	if types.DecisionEscalate != "ESCALATE" {
		t.Errorf("DecisionEscalate = %s, want ESCALATE", types.DecisionEscalate)
	}
	if types.DecisionDeny != "DENY" {
		t.Errorf("DecisionDeny = %s, want DENY", types.DecisionDeny)
	}
}

// TestPolicyStatusValues verifies policy status constants
func TestPolicyStatusValues(t *testing.T) {
	statuses := []struct {
		status   types.PolicyStatus
		expected string
	}{
		{types.PolicyStatusDeny, "DENY"},
		{types.PolicyStatusCovered, "COVERED"},
		{types.PolicyStatusRequiresFact, "REQUIRES_FACTS"},
		{types.PolicyStatusUncovered, "UNCOVERED"},
	}

	for _, s := range statuses {
		if string(s.status) != s.expected {
			t.Errorf("PolicyStatus = %s, want %s", s.status, s.expected)
		}
	}
}

// TestThreatLabelValues verifies threat label constants
func TestThreatLabelValues(t *testing.T) {
	labels := []struct {
		label    types.ThreatLabel
		expected string
	}{
		{types.ThreatClear, "CLEAR"},
		{types.ThreatSuspicious, "SUSPICIOUS"},
		{types.ThreatMalicious, "MALICIOUS"},
	}

	for _, l := range labels {
		if string(l.label) != l.expected {
			t.Errorf("ThreatLabel = %s, want %s", l.label, l.expected)
		}
	}
}
