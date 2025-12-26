package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Role represents a tenant membership role.
type Role string

const (
	RoleOwner     Role = "owner"
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

// Scope represents an authorization scope.
type Scope string

const (
	// Tenant management scopes
	ScopeTenantRead   Scope = "tenant:read"
	ScopeTenantWrite  Scope = "tenant:write"
	ScopeTenantDelete Scope = "tenant:delete"

	// Principal management scopes
	ScopePrincipalsRead  Scope = "principals:read"
	ScopePrincipalsWrite Scope = "principals:write"

	// Token management scopes
	ScopeTokensRead  Scope = "tokens:read"
	ScopeTokensWrite Scope = "tokens:write"

	// Tool/Toolset management scopes
	ScopeToolsRead  Scope = "tools:read"
	ScopeToolsWrite Scope = "tools:write"

	// Member management scopes
	ScopeMembersRead  Scope = "members:read"
	ScopeMembersWrite Scope = "members:write"

	// Audit scopes
	ScopeAuditRead Scope = "audit:read"
)

// RoleScopes maps roles to their allowed scopes.
var RoleScopes = map[Role][]Scope{
	RoleOwner: {
		ScopeTenantRead, ScopeTenantWrite, ScopeTenantDelete,
		ScopePrincipalsRead, ScopePrincipalsWrite,
		ScopeTokensRead, ScopeTokensWrite,
		ScopeToolsRead, ScopeToolsWrite,
		ScopeMembersRead, ScopeMembersWrite,
		ScopeAuditRead,
	},
	RoleAdmin: {
		ScopeTenantRead, ScopeTenantWrite,
		ScopePrincipalsRead, ScopePrincipalsWrite,
		ScopeTokensRead, ScopeTokensWrite,
		ScopeToolsRead, ScopeToolsWrite,
		ScopeMembersRead, ScopeMembersWrite,
		ScopeAuditRead,
	},
	RoleDeveloper: {
		ScopeTenantRead,
		ScopePrincipalsRead,
		ScopeTokensRead,
		ScopeToolsRead, ScopeToolsWrite,
		ScopeAuditRead,
	},
	RoleViewer: {
		ScopeTenantRead,
		ScopePrincipalsRead,
		ScopeToolsRead,
		ScopeAuditRead,
	},
}

// HasScope checks if a role has a specific scope.
func (r Role) HasScope(scope Scope) bool {
	scopes, ok := RoleScopes[r]
	if !ok {
		return false
	}
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// GetScopes returns all scopes for a role.
func (r Role) GetScopes() []Scope {
	return RoleScopes[r]
}

// TenantContext holds tenant-specific authorization info.
type TenantContext struct {
	TenantID string
	Role     Role
	Scopes   []Scope
}

// tenantContextKey is the context key for tenant context.
type tenantContextKey int

const tenantCtxKey tenantContextKey = iota

// WithTenantContext adds tenant context to a context.
func WithTenantContext(ctx context.Context, tc *TenantContext) context.Context {
	return context.WithValue(ctx, tenantCtxKey, tc)
}

// GetTenantContext retrieves tenant context from a context.
func GetTenantContext(ctx context.Context) *TenantContext {
	tc, ok := ctx.Value(tenantCtxKey).(*TenantContext)
	if !ok {
		return nil
	}
	return tc
}

// MembershipChecker is an interface for checking tenant membership.
type MembershipChecker interface {
	GetMembership(ctx context.Context, tenantID, userID string) (*Membership, error)
}

// Membership represents a user's membership in a tenant.
type Membership struct {
	TenantID string
	UserID   string
	Role     Role
	Status   string // "active", "pending", "revoked"
}

// TenantAuthMiddleware creates middleware that checks tenant membership.
type TenantAuthMiddleware struct {
	checker MembershipChecker
}

// NewTenantAuthMiddleware creates a new tenant auth middleware.
func NewTenantAuthMiddleware(checker MembershipChecker) *TenantAuthMiddleware {
	return &TenantAuthMiddleware{checker: checker}
}

// RequireTenantMembership returns middleware that requires active tenant membership.
// It expects the route to have a {tenant_id} URL parameter.
func (m *TenantAuthMiddleware) RequireTenantMembership(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := chi.URLParam(r, "tenant_id")
		if tenantID == "" {
			http.Error(w, `{"error":"tenant_id required","code":"BAD_REQUEST"}`, http.StatusBadRequest)
			return
		}

		auth := GetAuthContext(r.Context())
		if auth == nil {
			http.Error(w, `{"error":"authentication required","code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}

		membership, err := m.checker.GetMembership(r.Context(), tenantID, auth.UserID)
		if err != nil {
			http.Error(w, `{"error":"failed to check membership","code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
			return
		}

		if membership == nil || membership.Status != "active" {
			http.Error(w, `{"error":"not a member of this tenant","code":"FORBIDDEN"}`, http.StatusForbidden)
			return
		}

		// Add tenant context
		tenantCtx := &TenantContext{
			TenantID: tenantID,
			Role:     membership.Role,
			Scopes:   membership.Role.GetScopes(),
		}

		ctx := WithTenantContext(r.Context(), tenantCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireScope returns middleware that requires a specific scope.
// Must be used after RequireTenantMembership.
func RequireScope(scope Scope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc := GetTenantContext(r.Context())
			if tc == nil {
				http.Error(w, `{"error":"tenant context required","code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
				return
			}

			if !tc.Role.HasScope(scope) {
				http.Error(w, fmt.Sprintf(`{"error":"missing required scope: %s","code":"FORBIDDEN"}`, scope), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyScope returns middleware that requires any of the specified scopes.
func RequireAnyScope(scopes ...Scope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc := GetTenantContext(r.Context())
			if tc == nil {
				http.Error(w, `{"error":"tenant context required","code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
				return
			}

			for _, scope := range scopes {
				if tc.Role.HasScope(scope) {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, `{"error":"insufficient permissions","code":"FORBIDDEN"}`, http.StatusForbidden)
		})
	}
}
