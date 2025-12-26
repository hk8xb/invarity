// Package firewall implements the Invarity Firewall decision pipeline.
package firewall

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"invarity/internal/audit"
	"invarity/internal/config"
	"invarity/internal/constraints"
	"invarity/internal/llm"
	"invarity/internal/registry"
	"invarity/internal/types"
	"invarity/internal/util"
)

// Pipeline implements the Invarity Firewall decision pipeline.
// The new MVP pipeline follows these steps:
// S0: Canonicalize & bounds-check request
// S1: Schema Validation (deterministic)
// S2: Deterministic Constraints Evaluation
// S3: Intent Alignment Quorum (ALWAYS-ON)
// S4: Threat Sentinel (conditional: risk_tier >= MEDIUM)
// S5: Aggregate Decision (deterministic)
type Pipeline struct {
	cfg                  *config.Config
	logger               *zap.Logger
	registryStore        registry.Store
	auditStore           audit.Store
	schemaValidator      *registry.SchemaValidator
	constraintsEvaluator *constraints.Evaluator
	intentQuorum         *llm.IntentQuorum
	threatSentinel       *llm.ThreatSentinel
}

// PipelineConfig holds dependencies for the pipeline.
type PipelineConfig struct {
	Config        *config.Config
	Logger        *zap.Logger
	RegistryStore registry.Store
	AuditStore    audit.Store
	// All LLM clients use RunPod endpoints
	AlignmentClient *llm.Client // Intent alignment quorum
	ThreatClient    *llm.Client // Threat sentinel
}

// NewPipeline creates a new firewall pipeline.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	// Create intent quorum config with timeout from config
	var intentQuorumCfg *llm.IntentQuorumConfig
	if cfg.Config.IntentModelTimeout > 0 {
		intentQuorumCfg = &llm.IntentQuorumConfig{
			VoterTimeout: cfg.Config.IntentModelTimeout,
		}
	}

	return &Pipeline{
		cfg:                  cfg.Config,
		logger:               cfg.Logger,
		registryStore:        cfg.RegistryStore,
		auditStore:           cfg.AuditStore,
		schemaValidator:      registry.NewSchemaValidator(),
		constraintsEvaluator: constraints.NewEvaluator(),
		intentQuorum:         llm.NewIntentQuorum(cfg.AlignmentClient, intentQuorumCfg),
		threatSentinel:       llm.NewThreatSentinel(cfg.ThreatClient),
	}
}

// PipelineState holds the state as the request moves through the pipeline.
type PipelineState struct {
	Request      *types.ToolCallRequest
	RequestID    string
	Tool         *types.ToolRegistryEntry
	RiskTier     types.RiskTier
	Constraints  *types.ConstraintsResult
	Alignment    *types.IntentAlignmentResult
	Threat       *types.ThreatResult
	Timing       *types.PipelineTiming
	Reasons      []string
	Decision     types.Decision
	DecisionStep string // Which step made the decision
}

// Evaluate runs the full firewall decision pipeline.
func (p *Pipeline) Evaluate(ctx context.Context, req *types.ToolCallRequest) (*types.FirewallDecisionResponse, error) {
	totalStart := time.Now()

	state := &PipelineState{
		Request:   req,
		RequestID: req.RequestID,
		Timing:    &types.PipelineTiming{},
		Reasons:   make([]string, 0),
		RiskTier:  types.RiskTierLow, // Default
	}

	if state.RequestID == "" {
		state.RequestID = uuid.New().String()
	}

	logger := p.logger.With(zap.String("request_id", state.RequestID))

	// S0: Canonicalize & bounds-check
	if err := p.stepCanonicalize(ctx, state); err != nil {
		return p.buildErrorResponse(state, err, "S0_CANONICALIZE")
	}

	// S1: Schema Validation & Tool Lookup
	if err := p.stepSchemaValidation(ctx, state); err != nil {
		return p.buildDenyResponse(state, "S1_SCHEMA_VALIDATION", err.Error())
	}

	// Extract risk tier from tool
	p.extractRiskTier(state)

	// S2: Deterministic Constraints Evaluation
	if err := p.stepConstraintsEvaluation(ctx, state); err != nil {
		logger.Warn("constraints evaluation error", zap.Error(err))
	}
	if state.Constraints != nil && !state.Constraints.Passed {
		return p.buildDenyResponse(state, "S2_CONSTRAINTS", state.Constraints.Violations...)
	}

	// S3: Intent Alignment Quorum (ALWAYS-ON)
	if err := p.stepIntentAlignment(ctx, state); err != nil {
		logger.Warn("intent alignment quorum error", zap.Error(err))
		// On error, default to ESCALATE
		state.Alignment = &types.IntentAlignmentResult{
			Decision: types.IntentDecisionEscalate,
		}
		state.Reasons = append(state.Reasons, "intent_alignment_error")
	}
	// Check intent alignment decision
	if state.Alignment != nil && state.Alignment.Decision == types.IntentDecisionDeny {
		return p.buildDenyResponse(state, "S3_INTENT_ALIGNMENT", "intent_quorum_deny")
	}

	// S4: Threat Sentinel (conditional: risk_tier >= MEDIUM)
	if p.shouldRunThreatSentinel(state) {
		if err := p.stepThreatSentinel(ctx, state); err != nil {
			logger.Warn("threat sentinel error", zap.Error(err))
		}
		if state.Threat != nil && state.Threat.Label == types.ThreatMalicious {
			return p.buildDenyResponse(state, "S4_THREAT_SENTINEL", "threat_malicious")
		}
	}

	// S5: Aggregate Decision
	p.stepAggregateDecision(ctx, state)

	state.Timing.Total = types.Duration(time.Since(totalStart))

	return p.buildResponse(state)
}

// S0: Canonicalize & bounds-check request
func (p *Pipeline) stepCanonicalize(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.Canonicalize = types.Duration(time.Since(start))
	}()

	req := state.Request

	// Truncate user intent
	if len(req.UserIntent) > p.cfg.MaxIntentChars {
		req.UserIntent = util.TruncateString(req.UserIntent, p.cfg.MaxIntentChars)
		state.Reasons = append(state.Reasons, "intent_truncated")
	}

	// Truncate bounded context
	if req.BoundedContext != nil {
		for i, item := range req.BoundedContext.ConversationHistory {
			if len(item) > p.cfg.MaxContextChars/len(req.BoundedContext.ConversationHistory) {
				req.BoundedContext.ConversationHistory[i] = util.TruncateString(item, p.cfg.MaxContextChars/len(req.BoundedContext.ConversationHistory))
			}
		}
	}

	// Validate required fields
	if req.OrgID == "" {
		return fmt.Errorf("org_id is required")
	}
	if req.ToolCall.ActionID == "" {
		return fmt.Errorf("tool_call.action_id is required")
	}
	if req.UserIntent == "" {
		return fmt.Errorf("user_intent is required")
	}

	// Set defaults
	if req.Environment == "" {
		req.Environment = types.EnvDevelopment
	}
	if req.Timestamp.IsZero() {
		req.Timestamp = time.Now().UTC()
	}

	return nil
}

// S1: Schema Validation & Tool Lookup
func (p *Pipeline) stepSchemaValidation(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.SchemaValidate = types.Duration(time.Since(start))
	}()

	// Look up tool in registry
	tool, err := registry.ValidateToolExists(ctx, p.registryStore, state.Request.ToolCall)
	if err != nil {
		if err == registry.ErrToolNotFound {
			return fmt.Errorf("tool not registered: %s", state.Request.ToolCall.ActionID)
		}
		if err == registry.ErrVersionMismatch {
			return fmt.Errorf("version/schema hash mismatch for tool: %s", state.Request.ToolCall.ActionID)
		}
		return fmt.Errorf("registry lookup failed: %w", err)
	}

	state.Tool = tool

	// Check if tool is deprecated
	if tool.Deprecated {
		state.Reasons = append(state.Reasons, "tool_deprecated")
	}

	// Validate args against schema
	if err := p.schemaValidator.ValidateArgs(ctx, tool, state.Request.ToolCall.Args); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	return nil
}

// extractRiskTier extracts the risk tier from the tool's risk profile.
func (p *Pipeline) extractRiskTier(state *PipelineState) {
	if state.Tool == nil {
		state.RiskTier = types.RiskTierMedium // Default for unknown tools
		state.Reasons = append(state.Reasons, "unknown_tool_default_medium_risk")
		return
	}

	// Use BaseRiskLevel from risk profile
	switch state.Tool.RiskProfile.BaseRiskLevel {
	case "LOW":
		state.RiskTier = types.RiskTierLow
	case "MEDIUM":
		state.RiskTier = types.RiskTierMedium
	case "HIGH":
		state.RiskTier = types.RiskTierHigh
	case "CRITICAL":
		state.RiskTier = types.RiskTierCritical
	default:
		// Infer from risk profile flags if BaseRiskLevel not set
		if state.Tool.RiskProfile.MoneyMovement || state.Tool.RiskProfile.PrivilegeChange {
			state.RiskTier = types.RiskTierHigh
		} else if state.Tool.RiskProfile.Irreversible || state.Tool.RiskProfile.BulkOperation {
			state.RiskTier = types.RiskTierMedium
		} else {
			state.RiskTier = types.RiskTierLow
		}
	}
}

// S2: Deterministic Constraints Evaluation
func (p *Pipeline) stepConstraintsEvaluation(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.Constraints = types.Duration(time.Since(start))
	}()

	result, err := p.constraintsEvaluator.Evaluate(ctx, state.Tool, state.Request)
	if err != nil {
		return err
	}

	state.Constraints = &types.ConstraintsResult{
		Passed:       result.Passed,
		Violations:   result.Violations,
		MatchedRules: result.MatchedRules,
		Latency:      types.Duration(time.Since(start)),
	}

	// Add violations to reasons
	state.Reasons = append(state.Reasons, result.Violations...)

	return nil
}

// S3: Intent Alignment Quorum
func (p *Pipeline) stepIntentAlignment(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.Alignment = types.Duration(time.Since(start))
	}()

	result, err := p.intentQuorum.Run(ctx, &llm.IntentQuorumRequest{
		UserIntent:  state.Request.UserIntent,
		ToolCall:    state.Request.ToolCall,
		Tool:        state.Tool,
		Actor:       state.Request.Actor,
		Environment: state.Request.Environment,
		Context:     state.Request.BoundedContext,
	})

	if err != nil {
		return err
	}

	state.Alignment = result

	// Collect reasons from voters (for audit logging)
	for _, voter := range result.Voters {
		state.Reasons = append(state.Reasons, voter.Reasons...)
	}

	return nil
}

// S4: Threat Sentinel
func (p *Pipeline) stepThreatSentinel(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.ThreatSentinel = types.Duration(time.Since(start))
	}()

	result, err := p.threatSentinel.Run(ctx, &llm.ThreatRequest{
		UserIntent:  state.Request.UserIntent,
		ToolCall:    state.Request.ToolCall,
		Tool:        state.Tool,
		Actor:       state.Request.Actor,
		Environment: state.Request.Environment,
		Context:     state.Request.BoundedContext,
	})

	if err != nil {
		return err
	}

	state.Threat = result

	if len(result.ThreatTypes) > 0 {
		for _, tt := range result.ThreatTypes {
			state.Reasons = append(state.Reasons, "threat:"+tt)
		}
	}

	return nil
}

// S5: Aggregate Decision
func (p *Pipeline) stepAggregateDecision(ctx context.Context, state *PipelineState) {
	start := time.Now()
	defer func() {
		state.Timing.Aggregate = types.Duration(time.Since(start))
	}()

	// Check for ESCALATE signals
	escalate := false

	// Intent alignment escalate
	if state.Alignment != nil && state.Alignment.Decision == types.IntentDecisionEscalate {
		escalate = true
		state.Reasons = append(state.Reasons, "intent_alignment_escalate")
	}

	// Threat suspicious
	if state.Threat != nil && state.Threat.Label == types.ThreatSuspicious {
		escalate = true
		state.Reasons = append(state.Reasons, "threat_suspicious")
	}

	// High/Critical risk with requires_approval
	if state.Tool != nil && state.Tool.RiskProfile.RequiresApproval {
		if state.RiskTier == types.RiskTierHigh || state.RiskTier == types.RiskTierCritical {
			escalate = true
			state.Reasons = append(state.Reasons, "high_risk_requires_approval")
		}
	}

	if escalate {
		state.Decision = types.DecisionEscalate
		state.DecisionStep = "S5_AGGREGATE"
	} else {
		state.Decision = types.DecisionAllow
		state.DecisionStep = "S5_AGGREGATE"
	}

	// Deduplicate reasons
	state.Reasons = util.DedupeStrings(state.Reasons)
}

// shouldRunThreatSentinel determines if threat sentinel should run.
func (p *Pipeline) shouldRunThreatSentinel(state *PipelineState) bool {
	if !p.cfg.EnableThreatSentinel {
		return false
	}
	// Run for MEDIUM, HIGH, or CRITICAL risk tiers
	return state.RiskTier == types.RiskTierMedium ||
		state.RiskTier == types.RiskTierHigh ||
		state.RiskTier == types.RiskTierCritical
}

// buildDenyResponse creates a DENY response.
func (p *Pipeline) buildDenyResponse(state *PipelineState, step string, reasons ...string) (*types.FirewallDecisionResponse, error) {
	state.Decision = types.DecisionDeny
	state.DecisionStep = step
	state.Reasons = append(state.Reasons, reasons...)
	state.Reasons = util.DedupeStrings(state.Reasons)

	return p.buildResponse(state)
}

// buildErrorResponse creates an error response.
func (p *Pipeline) buildErrorResponse(state *PipelineState, err error, step string) (*types.FirewallDecisionResponse, error) {
	state.Decision = types.DecisionDeny
	state.DecisionStep = step
	state.Reasons = append(state.Reasons, "error:"+err.Error())

	return p.buildResponse(state)
}

// buildResponse creates the final response.
func (p *Pipeline) buildResponse(state *PipelineState) (*types.FirewallDecisionResponse, error) {
	// Write audit record
	auditWriter := audit.NewWriter(p.auditStore)

	resp := &types.FirewallDecisionResponse{
		RequestID:   state.RequestID,
		Decision:    state.Decision,
		RiskTier:    state.RiskTier,
		Reasons:     state.Reasons,
		Constraints: state.Constraints,
		Alignment:   state.Alignment,
		Threat:      state.Threat,
		Timing:      state.Timing,
		EvaluatedAt: time.Now().UTC(),
	}

	// Write audit
	auditID, err := auditWriter.WriteFromResponse(context.Background(), state.Request, resp, state.DecisionStep)
	if err != nil {
		p.logger.Error("failed to write audit record", zap.Error(err))
	}
	resp.AuditID = auditID

	return resp, nil
}
