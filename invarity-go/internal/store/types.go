// Package store provides data access layer for DynamoDB tables.
package store

import (
	"time"

	"invarity/internal/auth"
)

// Tenant represents a tenant in the system.
type Tenant struct {
	TenantID    string    `json:"tenant_id" dynamodbav:"tenant_id"`
	Name        string    `json:"name" dynamodbav:"name"`
	Status      string    `json:"status" dynamodbav:"status"` // "active", "suspended", "deleted"
	Plan        string    `json:"plan" dynamodbav:"plan"`     // "free", "pro", "enterprise"
	CreatedAt   time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" dynamodbav:"updated_at"`
	CreatedBy   string    `json:"created_by" dynamodbav:"created_by"` // user_id of creator
	Settings    string    `json:"settings,omitempty" dynamodbav:"settings,omitempty"` // JSON blob
}

// User represents a user in the system (thin mirror of Cognito).
type User struct {
	UserID      string    `json:"user_id" dynamodbav:"user_id"`
	Email       string    `json:"email" dynamodbav:"email"`
	DisplayName string    `json:"display_name,omitempty" dynamodbav:"display_name,omitempty"`
	Status      string    `json:"status" dynamodbav:"status"` // "active", "suspended"
	CreatedAt   time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" dynamodbav:"updated_at"`
	LastLoginAt time.Time `json:"last_login_at,omitempty" dynamodbav:"last_login_at,omitempty"`
}

// TenantMembership represents a user's membership in a tenant.
type TenantMembership struct {
	TenantID  string    `json:"tenant_id" dynamodbav:"tenant_id"`
	UserID    string    `json:"user_id" dynamodbav:"user_id"`
	Role      auth.Role `json:"role" dynamodbav:"role"`
	Status    string    `json:"status" dynamodbav:"status"` // "active", "pending", "revoked"
	InvitedBy string    `json:"invited_by,omitempty" dynamodbav:"invited_by,omitempty"`
	CreatedAt time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt time.Time `json:"updated_at" dynamodbav:"updated_at"`
}

// Principal represents an agent principal within a tenant.
type Principal struct {
	PrincipalID string    `json:"principal_id" dynamodbav:"principal_id"`
	TenantID    string    `json:"tenant_id" dynamodbav:"tenant_id"`
	Name        string    `json:"name" dynamodbav:"name"`
	Description string    `json:"description,omitempty" dynamodbav:"description,omitempty"`
	Type        string    `json:"type" dynamodbav:"type"` // "agent", "service"
	Status      string    `json:"status" dynamodbav:"status"` // "active", "suspended", "deleted"
	CreatedAt   time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" dynamodbav:"updated_at"`
	CreatedBy   string    `json:"created_by" dynamodbav:"created_by"` // user_id
	Metadata    string    `json:"metadata,omitempty" dynamodbav:"metadata,omitempty"` // JSON blob
}

// Token represents an API token for CLI/agent access.
type Token struct {
	TokenID      string    `json:"token_id" dynamodbav:"token_id"`
	TenantID     string    `json:"tenant_id" dynamodbav:"tenant_id"`
	PrincipalID  string    `json:"principal_id,omitempty" dynamodbav:"principal_id,omitempty"` // For agent tokens
	KeyHash      string    `json:"-" dynamodbav:"key_hash"` // SHA256 hash of token, never exposed
	KeyPrefix    string    `json:"key_prefix" dynamodbav:"key_prefix"` // First 8 chars for identification
	Name         string    `json:"name" dynamodbav:"name"`
	Type         string    `json:"type" dynamodbav:"type"` // "developer", "agent"
	Scopes       []string  `json:"scopes" dynamodbav:"scopes"`
	Status       string    `json:"status" dynamodbav:"status"` // "active", "revoked", "expired"
	CreatedAt    time.Time `json:"created_at" dynamodbav:"created_at"`
	ExpiresAt    time.Time `json:"expires_at,omitempty" dynamodbav:"expires_at,omitempty"`
	LastUsedAt   time.Time `json:"last_used_at,omitempty" dynamodbav:"last_used_at,omitempty"`
	CreatedBy    string    `json:"created_by" dynamodbav:"created_by"` // user_id
	RevokedAt    time.Time `json:"revoked_at,omitempty" dynamodbav:"revoked_at,omitempty"`
	RevokedBy    string    `json:"revoked_by,omitempty" dynamodbav:"revoked_by,omitempty"`
}

// TenantWithRole represents a tenant with the user's role for list responses.
type TenantWithRole struct {
	TenantID string    `json:"tenant_id"`
	Name     string    `json:"name"`
	Role     auth.Role `json:"role"`
}
