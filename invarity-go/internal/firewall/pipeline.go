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
	"invarity/internal/llm"
	"invarity/internal/policy"
	"invarity/internal/registry"
	"invarity/internal/risk"
	"invarity/internal/types"
	"invarity/internal/util"
)

// Pipeline implements the Invarity Firewall decision pipeline.
// The pipeline follows these steps:
// S0: Canonicalize & bounds-check request
// S1: Schema Validation (deterministic)
// S2: Base Risk Compute (deterministic)
// S3: Policy DSL Pass 1 (deterministic)
// S4: Intention Alignment Quorum (ALWAYS-ON)
// S5: Threat Sentinel (conditional: base_risk >= MEDIUM)
// S6: Policy Arbiter (conditional: base_risk >= MEDIUM AND policy needs facts)
// S7: Policy DSL Pass 2 (conditional: if arbiter ran)
// S8: Aggregate Decision (deterministic)
type Pipeline struct {
	cfg             *config.Config
	logger          *zap.Logger
	registryStore   registry.Store
	policyStore     policy.Store
	auditStore      audit.Store
	schemaValidator *registry.SchemaValidator
	policyEvaluator *policy.Evaluator
	intentQuorum    *llm.IntentQuorum
	threatSentinel  *llm.ThreatSentinel
	policyArbiter   *llm.PolicyArbiter
}

// PipelineConfig holds dependencies for the pipeline.
type PipelineConfig struct {
	Config        *config.Config
	Logger        *zap.Logger
	RegistryStore registry.Store
	PolicyStore   policy.Store
	AuditStore    audit.Store
	// All LLM clients use RunPod endpoints
	AlignmentClient *llm.Client // Intent alignment quorum
	ThreatClient    *llm.Client // Threat sentinel
	ArbiterClient   *llm.Client // Policy arbiter
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
		cfg:             cfg.Config,
		logger:          cfg.Logger,
		registryStore:   cfg.RegistryStore,
		policyStore:     cfg.PolicyStore,
		auditStore:      cfg.AuditStore,
		schemaValidator: registry.NewSchemaValidator(),
		policyEvaluator: policy.NewEvaluator(cfg.PolicyStore),
		intentQuorum:    llm.NewIntentQuorum(cfg.AlignmentClient, intentQuorumCfg),
		threatSentinel:  llm.NewThreatSentinel(cfg.ThreatClient),
		policyArbiter:   llm.NewPolicyArbiter(cfg.ArbiterClient),
	}
}

// PipelineState holds the state as the request moves through the pipeline.
type PipelineState struct {
	Request       *types.ToolCallRequest
	RequestID     string
	Tool          *types.ToolRegistryEntry
	RiskResult    *risk.ComputeResult
	PolicyResult1 *policy.EvaluationResult
	PolicyResult2 *policy.EvaluationResult
	Alignment     *types.IntentAlignmentResult
	Threat        *types.ThreatResult
	Arbiter       *types.ArbiterResult
	Timing        *types.PipelineTiming
	Reasons       []string
	Decision      types.Decision
	DecisionStep  string // Which step made the decision
}

// Evaluate runs the full firewall decision pipeline.
func (p *Pipeline) Evaluate(ctx context.Context, req *types.ToolCallRequest) (*types.FirewallDecisionResponse, error) {
	totalStart := time.Now()

	state := &PipelineState{
		Request:   req,
		RequestID: req.RequestID,
		Timing:    &types.PipelineTiming{},
		Reasons:   make([]string, 0),
	}

	if state.RequestID == "" {
		state.RequestID = uuid.New().String()
	}

	logger := p.logger.With(zap.String("request_id", state.RequestID))

	// S0: Canonicalize & bounds-check
	if err := p.stepCanonicalize(ctx, state); err != nil {
		return p.buildErrorResponse(state, err, "S0_CANONICALIZE")
	}

	// S1: Schema Validation
	if err := p.stepSchemaValidation(ctx, state); err != nil {
		return p.buildDenyResponse(state, "S1_SCHEMA_VALIDATION", err.Error())
	}

	// S2: Base Risk Compute
	p.stepRiskCompute(ctx, state)

	// S3: Policy DSL Pass 1
	if err := p.stepPolicyPass1(ctx, state); err != nil {
		logger.Warn("policy pass 1 error", zap.Error(err))
	}
	if state.PolicyResult1 != nil && state.PolicyResult1.Status == types.PolicyStatusDeny {
		return p.buildDenyResponse(state, "S3_POLICY_PASS1", state.PolicyResult1.DenyReasons...)
	}

	// S4: Intent Alignment Quorum (ALWAYS-ON)
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
		return p.buildDenyResponse(state, "S4_INTENT_ALIGNMENT", "intent_quorum_deny")
	}

	// S5: Threat Sentinel (conditional: base_risk >= MEDIUM)
	if p.shouldRunThreatSentinel(state) {
		if err := p.stepThreatSentinel(ctx, state); err != nil {
			logger.Warn("threat sentinel error", zap.Error(err))
		}
		if state.Threat != nil && state.Threat.Label == types.ThreatMalicious {
			return p.buildDenyResponse(state, "S5_THREAT_SENTINEL", "threat_malicious")
		}
	}

	// S6: Policy Arbiter (conditional)
	if p.shouldRunPolicyArbiter(state) {
		if err := p.stepPolicyArbiter(ctx, state); err != nil {
			logger.Warn("policy arbiter error", zap.Error(err))
		}
	}

	// S7: Policy DSL Pass 2 (if arbiter ran)
	if state.Arbiter != nil {
		if err := p.stepPolicyPass2(ctx, state); err != nil {
			logger.Warn("policy pass 2 error", zap.Error(err))
		}
		if state.PolicyResult2 != nil && state.PolicyResult2.Status == types.PolicyStatusDeny {
			return p.buildDenyResponse(state, "S7_POLICY_PASS2", state.PolicyResult2.DenyReasons...)
		}
	}

	// S8: Aggregate Decision
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

// S1: Schema Validation
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

// S2: Base Risk Compute
func (p *Pipeline) stepRiskCompute(ctx context.Context, state *PipelineState) {
	start := time.Now()
	defer func() {
		state.Timing.RiskCompute = types.Duration(time.Since(start))
	}()

	state.RiskResult = risk.Compute(&risk.ComputeContext{
		Tool:        state.Tool,
		Args:        state.Request.ToolCall.Args,
		Environment: state.Request.Environment,
		Actor:       state.Request.Actor,
	})

	state.Reasons = append(state.Reasons, state.RiskResult.Factors...)
}

// S3: Policy DSL Pass 1
func (p *Pipeline) stepPolicyPass1(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.PolicyPass1 = types.Duration(time.Since(start))
	}()

	result, err := p.policyEvaluator.Evaluate(ctx, &policy.EvaluationContext{
		OrgID:       state.Request.OrgID,
		Actor:       state.Request.Actor,
		Environment: state.Request.Environment,
		ToolCall:    state.Request.ToolCall,
		Tool:        state.Tool,
		RiskLevel:   state.RiskResult.Level,
		UserIntent:  state.Request.UserIntent,
	})

	if err != nil {
		return err
	}

	state.PolicyResult1 = result
	return nil
}

// S4: Intent Alignment Quorum
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

// S5: Threat Sentinel
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

// S6: Policy Arbiter
func (p *Pipeline) stepPolicyArbiter(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.Arbiter = types.Duration(time.Since(start))
	}()

	var requiredFacts []string
	var policyClauses []string

	if state.PolicyResult1 != nil {
		requiredFacts = state.PolicyResult1.RequiresFact
		policyClauses = state.PolicyResult1.MatchedRules
	}

	result, err := p.policyArbiter.Run(ctx, &llm.ArbiterRequest{
		UserIntent:    state.Request.UserIntent,
		ToolCall:      state.Request.ToolCall,
		Tool:          state.Tool,
		Actor:         state.Request.Actor,
		Environment:   state.Request.Environment,
		Context:       state.Request.BoundedContext,
		RequiredFacts: requiredFacts,
		PolicyClauses: policyClauses,
	})

	if err != nil {
		return err
	}

	state.Arbiter = result
	return nil
}

// S7: Policy DSL Pass 2
func (p *Pipeline) stepPolicyPass2(ctx context.Context, state *PipelineState) error {
	start := time.Now()
	defer func() {
		state.Timing.PolicyPass2 = types.Duration(time.Since(start))
	}()

	// Convert derived facts to map
	derivedFacts := make(map[string]any)
	if state.Arbiter != nil {
		for _, fact := range state.Arbiter.DerivedFacts {
			derivedFacts[fact.Key] = fact.Value
		}
	}

	result, err := p.policyEvaluator.EvaluateWithFacts(ctx, &policy.EvaluationContext{
		OrgID:       state.Request.OrgID,
		Actor:       state.Request.Actor,
		Environment: state.Request.Environment,
		ToolCall:    state.Request.ToolCall,
		Tool:        state.Tool,
		RiskLevel:   state.RiskResult.Level,
		UserIntent:  state.Request.UserIntent,
		DerivedFact: derivedFacts,
	})

	if err != nil {
		return err
	}

	state.PolicyResult2 = result
	return nil
}

// S8: Aggregate Decision
func (p *Pipeline) stepAggregateDecision(ctx context.Context, state *PipelineState) {
	start := time.Now()
	defer func() {
		state.Timing.Aggregate = types.Duration(time.Since(start))
	}()

	// Check for any DENY signals
	// (Already handled above, but double-check)

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

	// Policy uncovered or requires facts that weren't derived
	policyResult := state.PolicyResult1
	if state.PolicyResult2 != nil {
		policyResult = state.PolicyResult2
	}
	if policyResult != nil {
		if policyResult.Status == types.PolicyStatusUncovered {
			escalate = true
			state.Reasons = append(state.Reasons, "policy_uncovered")
		}
		if policyResult.Status == types.PolicyStatusRequiresFact {
			escalate = true
			state.Reasons = append(state.Reasons, "policy_requires_facts")
		}
	}

	// High/Critical risk with requires_approval
	if state.Tool != nil && state.Tool.RiskProfile.RequiresApproval {
		if state.RiskResult.Level == types.RiskHigh || state.RiskResult.Level == types.RiskCritical {
			escalate = true
			state.Reasons = append(state.Reasons, "high_risk_requires_approval")
		}
	}

	if escalate {
		state.Decision = types.DecisionEscalate
		state.DecisionStep = "S8_AGGREGATE"
	} else {
		state.Decision = types.DecisionAllow
		state.DecisionStep = "S8_AGGREGATE"
	}

	// Deduplicate reasons
	state.Reasons = util.DedupeStrings(state.Reasons)
}

// shouldRunThreatSentinel determines if threat sentinel should run.
func (p *Pipeline) shouldRunThreatSentinel(state *PipelineState) bool {
	if !p.cfg.EnableThreatSentinel {
		return false
	}
	return risk.CompareRiskLevels(state.RiskResult.Level, types.RiskMedium)
}

// shouldRunPolicyArbiter determines if policy arbiter should run.
func (p *Pipeline) shouldRunPolicyArbiter(state *PipelineState) bool {
	if !p.cfg.EnablePolicyArbiter {
		return false
	}

	// Must be medium+ risk
	if !risk.CompareRiskLevels(state.RiskResult.Level, types.RiskMedium) {
		return false
	}

	// Check if policy needs facts or context is fuzzy
	if state.Request.FuzzyContext {
		return true
	}

	if state.PolicyResult1 != nil {
		if state.PolicyResult1.Status == types.PolicyStatusRequiresFact {
			return true
		}
		if state.PolicyResult1.Status == types.PolicyStatusUncovered {
			return true
		}
	}

	return false
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

	riskLevel := types.RiskLow
	if state.RiskResult != nil {
		riskLevel = state.RiskResult.Level
	}

	resp := &types.FirewallDecisionResponse{
		RequestID: state.RequestID,
		Decision:  state.Decision,
		BaseRisk:  riskLevel,
		Reasons:   state.Reasons,
		Timing:    state.Timing,
		EvaluatedAt: time.Now().UTC(),
	}

	// Add policy result
	if state.PolicyResult2 != nil {
		resp.Policy = &types.PolicyResult{
			Version:      "1.0.0", // TODO: Get from bundle
			Status:       state.PolicyResult2.Status,
			MatchedRules: state.PolicyResult2.MatchedRules,
			RequiresFact: len(state.PolicyResult2.RequiresFact) > 0,
		}
	} else if state.PolicyResult1 != nil {
		resp.Policy = &types.PolicyResult{
			Version:      "1.0.0",
			Status:       state.PolicyResult1.Status,
			MatchedRules: state.PolicyResult1.MatchedRules,
			RequiresFact: len(state.PolicyResult1.RequiresFact) > 0,
		}
	}

	// Add intent alignment result
	resp.Alignment = state.Alignment

	// Add threat result
	resp.Threat = state.Threat

	// Add arbiter result
	resp.Arbiter = state.Arbiter

	// Write audit
	auditID, err := auditWriter.WriteFromResponse(context.Background(), state.Request, resp, state.DecisionStep)
	if err != nil {
		p.logger.Error("failed to write audit record", zap.Error(err))
	}
	resp.AuditID = auditID

	return resp, nil
}
