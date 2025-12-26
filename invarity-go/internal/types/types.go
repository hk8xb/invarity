// Package types contains shared types for the Invarity Firewall.
package types

import (
	"encoding/json"
	"time"
)

// Decision represents the firewall's final verdict.
type Decision string

const (
	DecisionAllow    Decision = "ALLOW"
	DecisionEscalate Decision = "ESCALATE"
	DecisionDeny     Decision = "DENY"
)

// RiskLevel represents the computed risk level of a tool call.
type RiskLevel string

const (
	RiskLow      RiskLevel = "LOW"
	RiskMedium   RiskLevel = "MEDIUM"
	RiskHigh     RiskLevel = "HIGH"
	RiskCritical RiskLevel = "CRITICAL"
)

// RiskLevelValue returns a numeric value for comparison.
func (r RiskLevel) Value() int {
	switch r {
	case RiskLow:
		return 1
	case RiskMedium:
		return 2
	case RiskHigh:
		return 3
	case RiskCritical:
		return 4
	default:
		return 0
	}
}

// PolicyStatus represents the result of policy evaluation.
type PolicyStatus string

const (
	PolicyStatusDeny         PolicyStatus = "DENY"
	PolicyStatusCovered      PolicyStatus = "COVERED"
	PolicyStatusRequiresFact PolicyStatus = "REQUIRES_FACTS"
	PolicyStatusUncovered    PolicyStatus = "UNCOVERED"
)

// ThreatLabel represents the threat classification.
type ThreatLabel string

const (
	ThreatClear      ThreatLabel = "CLEAR"
	ThreatSuspicious ThreatLabel = "SUSPICIOUS"
	ThreatMalicious  ThreatLabel = "MALICIOUS"
)

// Vote represents an alignment voter's decision.
type Vote string

const (
	VoteAllow    Vote = "ALLOW"
	VoteEscalate Vote = "ESCALATE"
	VoteDeny     Vote = "DENY"
)

// IntentVote represents an intent alignment voter's decision.
// This is distinct from Vote to support the SAFE/DENY/ABSTAIN semantics.
type IntentVote string

const (
	IntentVoteSafe    IntentVote = "SAFE"
	IntentVoteDeny    IntentVote = "DENY"
	IntentVoteAbstain IntentVote = "ABSTAIN"
)

// IntentDecision represents the final intent alignment decision.
type IntentDecision string

const (
	IntentDecisionSafe     IntentDecision = "SAFE"
	IntentDecisionEscalate IntentDecision = "ESCALATE"
	IntentDecisionDeny     IntentDecision = "DENY"
)

// Environment represents the deployment environment.
type Environment string

const (
	EnvProduction  Environment = "production"
	EnvStaging     Environment = "staging"
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
)

// Actor represents the entity initiating the tool call.
type Actor struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Type  string `json:"type,omitempty"` // "user", "agent", "system"
	OrgID string `json:"org_id"`
}

// ToolCall represents a proposed tool invocation.
type ToolCall struct {
	ActionID       string          `json:"action_id"`
	Version        string          `json:"version,omitempty"`
	SchemaHash     string          `json:"schema_hash,omitempty"`
	Args           json.RawMessage `json:"args"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// BoundedContext contains optional context snippets for evaluation.
type BoundedContext struct {
	ConversationHistory []string `json:"conversation_history,omitempty"`
	RelevantDocuments   []string `json:"relevant_documents,omitempty"`
	SystemState         string   `json:"system_state,omitempty"`
}

// ToolCallRequest is the input to the firewall evaluation endpoint.
type ToolCallRequest struct {
	RequestID      string          `json:"request_id,omitempty"`
	OrgID          string          `json:"org_id"`               // Deprecated: use TenantID
	TenantID       string          `json:"tenant_id,omitempty"`  // Tenant context
	PrincipalID    string          `json:"principal_id,omitempty"` // Principal (agent) making the call
	Actor          Actor           `json:"actor"`
	Environment    Environment     `json:"env"`
	UserIntent     string          `json:"user_intent"`
	ToolCall       ToolCall        `json:"tool_call"`
	BoundedContext *BoundedContext `json:"bounded_context,omitempty"`
	FuzzyContext   bool            `json:"fuzzy_context,omitempty"`
	Timestamp      time.Time       `json:"timestamp,omitempty"`
}

// IntentVoterResult represents a single intent voter's result.
type IntentVoterResult struct {
	VoterID    string     `json:"voter_id"`
	Vote       IntentVote `json:"vote"`
	Confidence float64    `json:"confidence"`
	Reasons    []string   `json:"reasons"`
	Latency    Duration   `json:"latency_ms"`
}

// IntentAlignmentResult represents the aggregated intent alignment quorum result.
type IntentAlignmentResult struct {
	Voters   []IntentVoterResult `json:"voters"`
	Decision IntentDecision      `json:"decision"`
	Latency  Duration            `json:"latency_ms"`
}

// IntentContext contains the normalized context for intent evaluation.
type IntentContext struct {
	IntentSummary      string          `json:"intent_summary"`
	ToolName           string          `json:"tool_name"`
	ToolDescription    string          `json:"tool_description"`
	Args               json.RawMessage `json:"args"`
	Operation          string          `json:"operation,omitempty"`
	ResourceScope      string          `json:"resource_scope,omitempty"`
	SideEffectScope    string          `json:"side_effect_scope,omitempty"`
	Bulk               bool            `json:"bulk,omitempty"`
	RequiredFields     []string        `json:"required_fields,omitempty"`
}

// ThreatResult represents the threat sentinel result.
type ThreatResult struct {
	Label       ThreatLabel `json:"label"`
	ThreatTypes []string    `json:"types,omitempty"`
	Confidence  float64     `json:"confidence"`
	Latency     Duration    `json:"latency_ms"`
}

// ConstraintsResult represents the deterministic constraint evaluation result.
type ConstraintsResult struct {
	Passed       bool     `json:"passed"`
	Violations   []string `json:"violations,omitempty"`
	MatchedRules []string `json:"matched_rules,omitempty"`
	Latency      Duration `json:"latency_ms"`
}

// ArbiterResult represents the policy arbiter result.
type ArbiterResult struct {
	DerivedFacts []DerivedFact `json:"derived_facts"`
	ClausesUsed  []string      `json:"clauses_used"`
	Confidence   float64       `json:"confidence"`
	Latency      Duration      `json:"latency_ms"`
}

// DerivedFact represents a fact derived by the arbiter.
type DerivedFact struct {
	Key        string  `json:"key"`
	Value      any     `json:"value"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source,omitempty"`
}

// PolicyResult represents policy evaluation result.
type PolicyResult struct {
	Version      string       `json:"version"`
	Status       PolicyStatus `json:"status"`
	MatchedRules []string     `json:"matched_rules,omitempty"`
	RequiresFact bool         `json:"requires_facts,omitempty"`
}

// FirewallDecisionResponse is the output of the firewall evaluation.
type FirewallDecisionResponse struct {
	RequestID   string                 `json:"request_id"`
	AuditID     string                 `json:"audit_id"`
	Decision    Decision               `json:"decision"`
	RiskTier    RiskTier               `json:"risk_tier"`
	Reasons     []string               `json:"reasons"`
	Constraints *ConstraintsResult     `json:"constraints,omitempty"`
	Alignment   *IntentAlignmentResult `json:"alignment,omitempty"`
	Threat      *ThreatResult          `json:"threat,omitempty"`
	Timing      *PipelineTiming        `json:"timing,omitempty"`
	EvaluatedAt time.Time              `json:"evaluated_at"`
}

// PipelineTiming tracks latency for each pipeline step.
type PipelineTiming struct {
	Total          Duration `json:"total_ms"`
	Canonicalize   Duration `json:"canonicalize_ms"`
	SchemaValidate Duration `json:"schema_validate_ms"`
	Constraints    Duration `json:"constraints_ms"`
	Alignment      Duration `json:"alignment_ms"`
	ThreatSentinel Duration `json:"threat_sentinel_ms,omitempty"`
	Aggregate      Duration `json:"aggregate_ms"`
}

// Duration is a time.Duration that marshals to milliseconds.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).Milliseconds())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var ms int64
	if err := json.Unmarshal(b, &ms); err != nil {
		return err
	}
	*d = Duration(time.Duration(ms) * time.Millisecond)
	return nil
}

// RiskTier represents the base risk tier of a tool (used for routing decisions).
type RiskTier string

const (
	RiskTierLow      RiskTier = "LOW"
	RiskTierMedium   RiskTier = "MEDIUM"
	RiskTierHigh     RiskTier = "HIGH"
	RiskTierCritical RiskTier = "CRITICAL"
)

// ToolConstraints defines deterministic constraints for a tool.
// These are evaluated at runtime and must pass for the tool call to proceed.
type ToolConstraints struct {
	AllowedEnvs       []string `json:"allowed_envs,omitempty"`        // Environments where tool can run
	DeniedEnvs        []string `json:"denied_envs,omitempty"`         // Environments where tool is blocked
	AllowedRoles      []string `json:"allowed_roles,omitempty"`       // Roles that can use this tool
	DeniedRoles       []string `json:"denied_roles,omitempty"`        // Roles that cannot use this tool
	MaxAmount         *float64 `json:"max_amount,omitempty"`          // Maximum monetary amount
	MaxBatchSize      *int     `json:"max_batch_size,omitempty"`      // Maximum batch/bulk size
	RequiredFields    []string `json:"required_fields,omitempty"`     // Fields that must be present in args
	DeniedArgPatterns []string `json:"denied_arg_patterns,omitempty"` // Patterns that will cause denial
}

// RiskProfile defines the risk characteristics of a tool.
// Note: BaseRiskLevel is the primary field used for risk tier routing.
type RiskProfile struct {
	MoneyMovement    bool     `json:"money_movement"`
	PrivilegeChange  bool     `json:"privilege_change"`
	Irreversible     bool     `json:"irreversible"`
	BulkOperation    bool     `json:"bulk_operation"`
	ResourceScope    string   `json:"resource_scope,omitempty"` // "single", "tenant", "global"
	DataClass        string   `json:"data_class,omitempty"`     // "public", "internal", "confidential", "restricted"
	AllowedEnvs      []string `json:"allowed_envs,omitempty"`   // Deprecated: use ToolConstraints.AllowedEnvs
	RequiresApproval bool     `json:"requires_approval"`
	BaseRiskLevel    string   `json:"base_risk_level,omitempty"` // LOW, MEDIUM, HIGH, CRITICAL
}

// ToolRegistryEntry represents a registered tool.
type ToolRegistryEntry struct {
	ActionID      string          `json:"action_id"`
	Version       string          `json:"version"`
	SchemaHash    string          `json:"schema_hash"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Schema        json.RawMessage `json:"schema"` // JSON Schema for args
	Constraints   ToolConstraints `json:"constraints"`
	RiskProfile   RiskProfile     `json:"risk_profile"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Deprecated    bool            `json:"deprecated"`
	DeprecatedMsg string          `json:"deprecated_msg,omitempty"`
}

// PolicyBundle represents a compiled policy bundle.
type PolicyBundle struct {
	OrgID       string       `json:"org_id"`
	Version     string       `json:"version"`
	Rules       []PolicyRule `json:"rules"`
	ClauseIndex []string     `json:"clause_index,omitempty"`
	CompiledAt  time.Time    `json:"compiled_at"`
}

// PolicyRule represents a single policy rule.
type PolicyRule struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Priority    int             `json:"priority"`
	Conditions  json.RawMessage `json:"conditions"` // DSL conditions
	Effect      string          `json:"effect"`     // "allow", "deny", "escalate"
	RequiresFct []string        `json:"requires_facts,omitempty"`
}

// AuditRecord represents a complete audit trail entry.
type AuditRecord struct {
	AuditID      string                 `json:"audit_id"`
	RequestID    string                 `json:"request_id"`
	OrgID        string                 `json:"org_id"`
	Actor        Actor                  `json:"actor"`
	Environment  Environment            `json:"env"`
	ToolCall     ToolCall               `json:"tool_call"`
	UserIntent   string                 `json:"user_intent"`
	Decision     Decision               `json:"decision"`
	RiskTier     RiskTier               `json:"risk_tier"`
	Reasons      []string               `json:"reasons"`
	Constraints  *ConstraintsResult     `json:"constraints,omitempty"`
	Alignment    *IntentAlignmentResult `json:"alignment,omitempty"`
	Threat       *ThreatResult          `json:"threat,omitempty"`
	Timing       *PipelineTiming        `json:"timing,omitempty"`
	PipelineStep string                 `json:"pipeline_step"` // Where decision was made
	CreatedAt    time.Time              `json:"created_at"`
	Metadata     map[string]any         `json:"metadata,omitempty"`
}

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
	Details   any    `json:"details,omitempty"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status    string            `json:"status"`
	Version   string            `json:"version,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}
