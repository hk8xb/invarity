// Package types contains shared types for the Invarity Firewall.
package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// ToolManifestV3 represents a tool manifest following schema v3.
// This is the canonical format stored in S3 and used for tool registration.
type ToolManifestV3 struct {
	// Schema version (must be "3")
	SchemaVersion string `json:"schema_version"`

	// Core identity
	ToolID      string `json:"tool_id"`
	Version     string `json:"version"`      // Semantic version (e.g., "1.0.0")
	SchemaHash  string `json:"schema_hash"`  // SHA256 of args_schema canonical JSON
	Name        string `json:"name"`         // Human-readable name
	Description string `json:"description"`  // Tool description

	// Arguments schema (JSON Schema draft-2020-12)
	ArgsSchema json.RawMessage `json:"args_schema"`

	// Deterministic constraints (evaluated at runtime)
	Constraints ToolConstraintsV3 `json:"constraints"`

	// Risk profile (used for routing decisions)
	RiskProfile RiskProfileV3 `json:"risk_profile"`

	// Metadata
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Deprecated bool      `json:"deprecated,omitempty"`
	DeprecatedMsg string `json:"deprecated_msg,omitempty"`

	// Optional: additional metadata
	Tags     []string       `json:"tags,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToolConstraintsV3 defines deterministic constraints for a tool (schema v3).
// These are evaluated at runtime and must pass for the tool call to proceed.
type ToolConstraintsV3 struct {
	// Environment restrictions
	AllowedEnvs []string `json:"allowed_envs,omitempty"` // Environments where tool can run
	DeniedEnvs  []string `json:"denied_envs,omitempty"`  // Environments where tool is blocked

	// Role restrictions
	AllowedRoles []string `json:"allowed_roles,omitempty"` // Roles that can use this tool
	DeniedRoles  []string `json:"denied_roles,omitempty"`  // Roles that cannot use this tool

	// Value limits
	MaxAmount    *float64 `json:"max_amount,omitempty"`     // Maximum monetary amount
	MaxBatchSize *int     `json:"max_batch_size,omitempty"` // Maximum batch/bulk size (max_bulk)
	AmountLimit  *float64 `json:"amount_limit,omitempty"`   // Alternative amount limit field

	// Field requirements
	RequiredArgs       []string `json:"required_args,omitempty"`        // Fields that must be present in args
	DisallowWildcards  bool     `json:"disallow_wildcards,omitempty"`   // Reject wildcard patterns in args
	RequiresJustification bool  `json:"requires_justification,omitempty"` // Requires justification field

	// Denial patterns
	DeniedArgPatterns []string `json:"denied_arg_patterns,omitempty"` // Patterns that will cause denial
}

// RiskProfileV3 defines the risk characteristics of a tool (schema v3).
type RiskProfileV3 struct {
	// Base risk level (PRIMARY - used for routing)
	BaseRiskLevel string `json:"base_risk_level"` // LOW, MEDIUM, HIGH, CRITICAL

	// Risk flags
	MoneyMovement   bool `json:"money_movement,omitempty"`
	PrivilegeChange bool `json:"privilege_change,omitempty"`
	Irreversible    bool `json:"irreversible,omitempty"`
	BulkOperation   bool `json:"bulk_operation,omitempty"`

	// Scope
	ResourceScope string `json:"resource_scope,omitempty"` // "single", "tenant", "global"
	DataClass     string `json:"data_class,omitempty"`     // "public", "internal", "confidential", "restricted"

	// Approval
	RequiresApproval bool `json:"requires_approval,omitempty"`
}

// Validate validates the tool manifest.
func (m *ToolManifestV3) Validate() error {
	if m.SchemaVersion != "3" {
		return fmt.Errorf("schema_version must be '3', got '%s'", m.SchemaVersion)
	}
	if m.ToolID == "" {
		return fmt.Errorf("tool_id is required")
	}
	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(m.ArgsSchema) == 0 {
		return fmt.Errorf("args_schema is required")
	}

	// Validate risk level
	validRiskLevels := map[string]bool{
		"LOW": true, "MEDIUM": true, "HIGH": true, "CRITICAL": true,
	}
	if m.RiskProfile.BaseRiskLevel != "" && !validRiskLevels[m.RiskProfile.BaseRiskLevel] {
		return fmt.Errorf("base_risk_level must be one of: LOW, MEDIUM, HIGH, CRITICAL")
	}

	return nil
}

// ToToolRegistryEntry converts a v3 manifest to the internal registry entry format.
func (m *ToolManifestV3) ToToolRegistryEntry() *ToolRegistryEntry {
	return &ToolRegistryEntry{
		ActionID:    m.ToolID,
		Version:     m.Version,
		SchemaHash:  m.SchemaHash,
		Name:        m.Name,
		Description: m.Description,
		Schema:      m.ArgsSchema,
		Constraints: ToolConstraints{
			AllowedEnvs:       m.Constraints.AllowedEnvs,
			DeniedEnvs:        m.Constraints.DeniedEnvs,
			AllowedRoles:      m.Constraints.AllowedRoles,
			DeniedRoles:       m.Constraints.DeniedRoles,
			MaxAmount:         m.Constraints.MaxAmount,
			MaxBatchSize:      m.Constraints.MaxBatchSize,
			RequiredFields:    m.Constraints.RequiredArgs,
			DeniedArgPatterns: m.Constraints.DeniedArgPatterns,
		},
		RiskProfile: RiskProfile{
			BaseRiskLevel:    m.RiskProfile.BaseRiskLevel,
			MoneyMovement:    m.RiskProfile.MoneyMovement,
			PrivilegeChange:  m.RiskProfile.PrivilegeChange,
			Irreversible:     m.RiskProfile.Irreversible,
			BulkOperation:    m.RiskProfile.BulkOperation,
			ResourceScope:    m.RiskProfile.ResourceScope,
			DataClass:        m.RiskProfile.DataClass,
			RequiresApproval: m.RiskProfile.RequiresApproval,
		},
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		Deprecated:    m.Deprecated,
		DeprecatedMsg: m.DeprecatedMsg,
	}
}

// ToolMetadata represents the DynamoDB metadata for a tool (without full manifest).
type ToolMetadata struct {
	TenantID   string    `json:"tenant_id" dynamodbav:"tenant_id"`
	ToolID     string    `json:"tool_id" dynamodbav:"tool_id"`
	Version    string    `json:"version" dynamodbav:"version"`
	SchemaHash string    `json:"schema_hash" dynamodbav:"schema_hash"`
	Name       string    `json:"name" dynamodbav:"name"`
	S3Key      string    `json:"s3_key" dynamodbav:"s3_key"`
	RiskLevel  string    `json:"risk_level" dynamodbav:"risk_level"`
	Status     string    `json:"status" dynamodbav:"status"` // "active", "deprecated"
	CreatedAt  time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" dynamodbav:"updated_at"`
}

// ToolRef represents a reference to a specific tool version.
type ToolRef struct {
	ToolID  string `json:"tool_id"`
	Version string `json:"version"`
}

// ToolsetManifest represents a toolset that groups tools together.
type ToolsetManifest struct {
	// Core identity
	ToolsetID string `json:"toolset_id"`
	Revision  string `json:"revision"` // String revision (e.g., "1", "2", or "v1.0.0")

	// Tool references
	Tools []ToolRef `json:"tools"`

	// Metadata
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	CreatedBy   string         `json:"created_by,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Validate validates the toolset manifest.
func (m *ToolsetManifest) Validate() error {
	if m.ToolsetID == "" {
		return fmt.Errorf("toolset_id is required")
	}
	if m.Revision == "" {
		return fmt.Errorf("revision is required")
	}
	if len(m.Tools) == 0 {
		return fmt.Errorf("tools array must not be empty")
	}

	// Validate each tool reference
	for i, ref := range m.Tools {
		if ref.ToolID == "" {
			return fmt.Errorf("tools[%d].tool_id is required", i)
		}
		if ref.Version == "" {
			return fmt.Errorf("tools[%d].version is required", i)
		}
	}

	return nil
}

// ToolsetMetadata represents the DynamoDB metadata for a toolset.
type ToolsetMetadata struct {
	TenantID    string    `json:"tenant_id" dynamodbav:"tenant_id"`
	ToolsetID   string    `json:"toolset_id" dynamodbav:"toolset_id"`
	Revision    string    `json:"revision" dynamodbav:"revision"`
	Name        string    `json:"name" dynamodbav:"name"`
	ToolCount   int       `json:"tool_count" dynamodbav:"tool_count"`
	S3Key       string    `json:"s3_key" dynamodbav:"s3_key"`
	Status      string    `json:"status" dynamodbav:"status"` // "active", "archived"
	CreatedAt   time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" dynamodbav:"updated_at"`
	CreatedBy   string    `json:"created_by" dynamodbav:"created_by"`
}

// PrincipalActiveToolset represents the active toolset assignment for a principal.
type PrincipalActiveToolset struct {
	PrincipalID       string    `json:"principal_id" dynamodbav:"principal_id"`
	ActiveToolsetID   string    `json:"active_toolset_id" dynamodbav:"active_toolset_id"`
	ActiveToolsetRev  string    `json:"active_toolset_revision" dynamodbav:"active_toolset_revision"`
	AssignedAt        time.Time `json:"assigned_at" dynamodbav:"assigned_at"`
	AssignedBy        string    `json:"assigned_by" dynamodbav:"assigned_by"`
}
