// Package http provides HTTP handlers and routing for the Invarity Firewall.
package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"invarity/internal/auth"
	"invarity/internal/store"
	"invarity/internal/types"
)

// OnboardingHandler handles onboarding-related endpoints.
type OnboardingHandler struct {
	store  *store.DynamoDBStore
	logger *zap.Logger
}

// NewOnboardingHandler creates a new onboarding handler.
func NewOnboardingHandler(store *store.DynamoDBStore, logger *zap.Logger) *OnboardingHandler {
	return &OnboardingHandler{
		store:  store,
		logger: logger,
	}
}

// BootstrapRequest is the request body for POST /v1/onboarding/bootstrap.
type BootstrapRequest struct {
	TenantName string `json:"tenant_name"`
}

// BootstrapResponse is the response for POST /v1/onboarding/bootstrap.
type BootstrapResponse struct {
	TenantID   string `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Role       string `json:"role"`
	IsNew      bool   `json:"is_new"`
}

// HandleBootstrap handles POST /v1/onboarding/bootstrap.
// Creates a new tenant with the caller as owner (idempotent).
func (h *OnboardingHandler) HandleBootstrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Parse request
	var req BootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "PARSE_ERROR", requestID)
		return
	}

	// Validate
	if req.TenantName == "" {
		h.writeError(w, http.StatusBadRequest, "tenant_name is required", "VALIDATION_ERROR", requestID)
		return
	}

	// Check if user already owns a tenant (idempotent)
	existingTenant, err := h.store.GetUserOwnedTenant(ctx, authCtx.UserID)
	if err != nil {
		h.logger.Error("failed to check existing tenant", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to check existing tenant", "STORE_ERROR", requestID)
		return
	}

	if existingTenant != nil {
		// Return existing tenant
		writeJSON(w, http.StatusOK, BootstrapResponse{
			TenantID:   existingTenant.TenantID,
			TenantName: existingTenant.Name,
			Role:       string(auth.RoleOwner),
			IsNew:      false,
		})
		return
	}

	// Ensure user exists
	_, err = h.store.GetOrCreateUser(ctx, authCtx.UserID, authCtx.Email)
	if err != nil {
		h.logger.Error("failed to ensure user exists", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to create user", "STORE_ERROR", requestID)
		return
	}

	// Create tenant
	tenant, err := h.store.CreateTenant(ctx, req.TenantName, authCtx.UserID)
	if err != nil {
		h.logger.Error("failed to create tenant", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to create tenant", "STORE_ERROR", requestID)
		return
	}

	// Create owner membership
	_, err = h.store.CreateMembership(ctx, tenant.TenantID, authCtx.UserID, auth.RoleOwner, authCtx.UserID)
	if err != nil {
		h.logger.Error("failed to create membership", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to create membership", "STORE_ERROR", requestID)
		return
	}

	h.logger.Info("tenant bootstrapped",
		zap.String("tenant_id", tenant.TenantID),
		zap.String("user_id", authCtx.UserID),
	)

	writeJSON(w, http.StatusCreated, BootstrapResponse{
		TenantID:   tenant.TenantID,
		TenantName: tenant.Name,
		Role:       string(auth.RoleOwner),
		IsNew:      true,
	})
}

// MeResponse is the response for GET /v1/me.
type MeResponse struct {
	UserID  string              `json:"user_id"`
	Email   string              `json:"email"`
	Tenants []TenantMembership  `json:"tenants"`
}

// TenantMembership represents a tenant the user belongs to.
type TenantMembership struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

// HandleMe handles GET /v1/me.
// Returns the current user's profile and their tenants.
func (h *OnboardingHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Ensure user exists
	user, err := h.store.GetOrCreateUser(ctx, authCtx.UserID, authCtx.Email)
	if err != nil {
		h.logger.Error("failed to get/create user", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to get user", "STORE_ERROR", requestID)
		return
	}

	// Get user's tenants
	tenants, err := h.store.ListTenantsForUser(ctx, authCtx.UserID)
	if err != nil {
		h.logger.Error("failed to list tenants", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to list tenants", "STORE_ERROR", requestID)
		return
	}

	// Convert to response format
	tenantList := make([]TenantMembership, len(tenants))
	for i, t := range tenants {
		tenantList[i] = TenantMembership{
			TenantID: t.TenantID,
			Name:     t.Name,
			Role:     string(t.Role),
		}
	}

	writeJSON(w, http.StatusOK, MeResponse{
		UserID:  user.UserID,
		Email:   user.Email,
		Tenants: tenantList,
	})
}

// CreatePrincipalRequest is the request body for POST /v1/tenants/{tenant_id}/principals.
type CreatePrincipalRequest struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "agent" or "service"
	Description string `json:"description,omitempty"`
}

// CreatePrincipalResponse is the response for POST /v1/tenants/{tenant_id}/principals.
type CreatePrincipalResponse struct {
	PrincipalID string `json:"principal_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
}

// HandleCreatePrincipal handles POST /v1/tenants/{tenant_id}/principals.
// Creates a new principal (agent) under a tenant.
func (h *OnboardingHandler) HandleCreatePrincipal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Parse request
	var req CreatePrincipalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "PARSE_ERROR", requestID)
		return
	}

	// Validate
	if req.Name == "" {
		h.writeError(w, http.StatusBadRequest, "name is required", "VALIDATION_ERROR", requestID)
		return
	}
	if req.Type == "" {
		req.Type = "agent" // Default to agent
	}
	if req.Type != "agent" && req.Type != "service" {
		h.writeError(w, http.StatusBadRequest, "type must be 'agent' or 'service'", "VALIDATION_ERROR", requestID)
		return
	}

	// Create principal
	principal, err := h.store.CreatePrincipal(ctx, tenantID, req.Name, req.Type, authCtx.UserID)
	if err != nil {
		h.logger.Error("failed to create principal", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to create principal", "STORE_ERROR", requestID)
		return
	}

	h.logger.Info("principal created",
		zap.String("principal_id", principal.PrincipalID),
		zap.String("tenant_id", tenantID),
		zap.String("created_by", authCtx.UserID),
	)

	writeJSON(w, http.StatusCreated, CreatePrincipalResponse{
		PrincipalID: principal.PrincipalID,
		Name:        principal.Name,
		Type:        principal.Type,
		Status:      principal.Status,
	})
}

// ListPrincipalsResponse is the response for GET /v1/tenants/{tenant_id}/principals.
type ListPrincipalsResponse struct {
	Principals []PrincipalInfo `json:"principals"`
}

// PrincipalInfo is a summary of a principal.
type PrincipalInfo struct {
	PrincipalID string `json:"principal_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
}

// HandleListPrincipals handles GET /v1/tenants/{tenant_id}/principals.
func (h *OnboardingHandler) HandleListPrincipals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	principals, err := h.store.ListPrincipals(ctx, tenantID)
	if err != nil {
		h.logger.Error("failed to list principals", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to list principals", "STORE_ERROR", requestID)
		return
	}

	principalList := make([]PrincipalInfo, len(principals))
	for i, p := range principals {
		principalList[i] = PrincipalInfo{
			PrincipalID: p.PrincipalID,
			Name:        p.Name,
			Type:        p.Type,
			Status:      p.Status,
		}
	}

	writeJSON(w, http.StatusOK, ListPrincipalsResponse{
		Principals: principalList,
	})
}

// writeError writes an error response (for OnboardingHandler).
func (h *OnboardingHandler) writeError(w http.ResponseWriter, status int, message, code, requestID string) {
	resp := types.ErrorResponse{
		Error:     message,
		Code:      code,
		RequestID: requestID,
	}
	writeJSON(w, status, resp)
}
