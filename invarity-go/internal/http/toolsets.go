// Package http provides HTTP handlers and routing for the Invarity Firewall.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"invarity/internal/auth"
	"invarity/internal/store"
	"invarity/internal/types"
)

// ToolsetsHandler handles toolset management endpoints.
type ToolsetsHandler struct {
	store    *store.DynamoDBStore
	s3Client *store.S3Client
	logger   *zap.Logger
}

// NewToolsetsHandler creates a new toolsets handler.
func NewToolsetsHandler(ddbStore *store.DynamoDBStore, s3Client *store.S3Client, logger *zap.Logger) *ToolsetsHandler {
	return &ToolsetsHandler{
		store:    ddbStore,
		s3Client: s3Client,
		logger:   logger,
	}
}

// RegisterToolsetRequest is the request body for POST /v1/tenants/{tenant_id}/toolsets.
type RegisterToolsetRequest struct {
	types.ToolsetManifest
}

// RegisterToolsetResponse is the response for POST /v1/tenants/{tenant_id}/toolsets.
type RegisterToolsetResponse struct {
	ToolsetID string `json:"toolset_id"`
	Revision  string `json:"revision"`
	ToolCount int    `json:"tool_count"`
	IsNew     bool   `json:"is_new"`
	S3Key     string `json:"s3_key"`
}

// HandleRegisterToolset handles POST /v1/tenants/{tenant_id}/toolsets.
// Registers a new toolset or returns existing if idempotent.
func (h *ToolsetsHandler) HandleRegisterToolset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Parse request
	var manifest types.ToolsetManifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "PARSE_ERROR", requestID)
		return
	}

	// Validate manifest
	if err := manifest.Validate(); err != nil {
		h.writeError(w, http.StatusBadRequest, "validation error: "+err.Error(), "VALIDATION_ERROR", requestID)
		return
	}

	// Verify all referenced tools exist
	for _, ref := range manifest.Tools {
		tool, err := h.store.GetToolRecord(ctx, tenantID, ref.ToolID, ref.Version)
		if err != nil {
			h.logger.Error("failed to verify tool reference", zap.Error(err))
			h.writeError(w, http.StatusInternalServerError, "failed to verify tool references", "STORE_ERROR", requestID)
			return
		}
		if tool == nil {
			h.writeError(w, http.StatusBadRequest, "tool not found: "+ref.ToolID+" version "+ref.Version, "TOOL_NOT_FOUND", requestID)
			return
		}
	}

	// Set timestamps and creator
	now := time.Now().UTC()
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = now
	}
	manifest.UpdatedAt = now
	if manifest.CreatedBy == "" {
		manifest.CreatedBy = authCtx.UserID
	}

	// Determine S3 key
	s3Key := store.ToolsetManifestKey(tenantID, manifest.ToolsetID, manifest.Revision)

	// Get name for metadata
	name := manifest.Name
	if name == "" {
		name = manifest.ToolsetID
	}

	// Try to register toolset in DynamoDB
	isNew, err := h.store.RegisterToolset(ctx, tenantID, manifest.ToolsetID, manifest.Revision, name, s3Key, authCtx.UserID, len(manifest.Tools))
	if err != nil {
		if isConflictError(err) {
			h.writeError(w, http.StatusConflict, err.Error(), "CONFLICT", requestID)
			return
		}
		h.logger.Error("failed to register toolset", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to register toolset", "STORE_ERROR", requestID)
		return
	}

	// If new, store manifest in S3
	if isNew && h.s3Client != nil {
		if err := h.s3Client.PutJSON(ctx, s3Key, manifest); err != nil {
			h.logger.Error("failed to store toolset manifest in S3", zap.Error(err), zap.String("s3_key", s3Key))
		}
	}

	h.logger.Info("toolset registered",
		zap.String("tenant_id", tenantID),
		zap.String("toolset_id", manifest.ToolsetID),
		zap.String("revision", manifest.Revision),
		zap.Int("tool_count", len(manifest.Tools)),
		zap.Bool("is_new", isNew),
	)

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}

	writeJSON(w, status, RegisterToolsetResponse{
		ToolsetID: manifest.ToolsetID,
		Revision:  manifest.Revision,
		ToolCount: len(manifest.Tools),
		IsNew:     isNew,
		S3Key:     s3Key,
	})
}

// ListToolsetsResponse is the response for GET /v1/tenants/{tenant_id}/toolsets.
type ListToolsetsResponse struct {
	Toolsets []ToolsetInfo `json:"toolsets"`
}

// ToolsetInfo is a summary of a toolset.
type ToolsetInfo struct {
	ToolsetID string `json:"toolset_id"`
	Revision  string `json:"revision"`
	Name      string `json:"name"`
	ToolCount int    `json:"tool_count"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
}

// HandleListToolsets handles GET /v1/tenants/{tenant_id}/toolsets.
func (h *ToolsetsHandler) HandleListToolsets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	toolsets, err := h.store.ListToolsets(ctx, tenantID, 100)
	if err != nil {
		h.logger.Error("failed to list toolsets", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to list toolsets", "STORE_ERROR", requestID)
		return
	}

	toolsetInfos := make([]ToolsetInfo, len(toolsets))
	for i, ts := range toolsets {
		toolsetInfos[i] = ToolsetInfo{
			ToolsetID: ts.ToolsetID,
			Revision:  ts.Revision,
			Name:      ts.Name,
			ToolCount: ts.ToolCount,
			Status:    ts.Status,
			CreatedBy: ts.CreatedBy,
		}
	}

	writeJSON(w, http.StatusOK, ListToolsetsResponse{
		Toolsets: toolsetInfos,
	})
}

// GetToolsetResponse is the response for GET /v1/tenants/{tenant_id}/toolsets/{toolset_id}/{revision}.
type GetToolsetResponse struct {
	types.ToolsetManifest
}

// HandleGetToolset handles GET /v1/tenants/{tenant_id}/toolsets/{toolset_id}/{revision}.
func (h *ToolsetsHandler) HandleGetToolset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")
	toolsetID := chi.URLParam(r, "toolset_id")
	revision := chi.URLParam(r, "revision")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Get toolset record from DynamoDB
	record, err := h.store.GetToolsetRecord(ctx, tenantID, toolsetID, revision)
	if err != nil {
		h.logger.Error("failed to get toolset", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to get toolset", "STORE_ERROR", requestID)
		return
	}
	if record == nil {
		h.writeError(w, http.StatusNotFound, "toolset not found", "NOT_FOUND", requestID)
		return
	}

	// Try to get full manifest from S3
	if h.s3Client != nil {
		var manifest types.ToolsetManifest
		if err := h.s3Client.GetJSON(ctx, record.S3Key, &manifest); err != nil {
			h.logger.Warn("failed to get toolset manifest from S3, returning metadata only", zap.Error(err))
			writeJSON(w, http.StatusOK, ToolsetInfo{
				ToolsetID: record.ToolsetID,
				Revision:  record.Revision,
				Name:      record.Name,
				ToolCount: record.ToolCount,
				Status:    record.Status,
				CreatedBy: record.CreatedBy,
			})
			return
		}
		writeJSON(w, http.StatusOK, manifest)
		return
	}

	// No S3 client, return metadata only
	writeJSON(w, http.StatusOK, ToolsetInfo{
		ToolsetID: record.ToolsetID,
		Revision:  record.Revision,
		Name:      record.Name,
		ToolCount: record.ToolCount,
		Status:    record.Status,
		CreatedBy: record.CreatedBy,
	})
}

// ApplyToolsetRequest is the request body for POST /v1/tenants/{tenant_id}/principals/{principal_id}/toolsets:apply.
type ApplyToolsetRequest struct {
	ToolsetID string `json:"toolset_id"`
	Revision  string `json:"revision"`
}

// ApplyToolsetResponse is the response for applying a toolset.
type ApplyToolsetResponse struct {
	PrincipalID string `json:"principal_id"`
	ToolsetID   string `json:"toolset_id"`
	Revision    string `json:"revision"`
	AppliedAt   string `json:"applied_at"`
}

// HandleApplyToolset handles POST /v1/tenants/{tenant_id}/principals/{principal_id}/toolsets:apply.
func (h *ToolsetsHandler) HandleApplyToolset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")
	principalID := chi.URLParam(r, "principal_id")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Parse request
	var req ApplyToolsetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "PARSE_ERROR", requestID)
		return
	}

	// Validate
	if req.ToolsetID == "" {
		h.writeError(w, http.StatusBadRequest, "toolset_id is required", "VALIDATION_ERROR", requestID)
		return
	}
	if req.Revision == "" {
		h.writeError(w, http.StatusBadRequest, "revision is required", "VALIDATION_ERROR", requestID)
		return
	}

	// Verify principal exists and belongs to tenant
	principal, err := h.store.GetPrincipal(ctx, principalID)
	if err != nil {
		h.logger.Error("failed to get principal", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to get principal", "STORE_ERROR", requestID)
		return
	}
	if principal == nil {
		h.writeError(w, http.StatusNotFound, "principal not found", "NOT_FOUND", requestID)
		return
	}
	if principal.TenantID != tenantID {
		h.writeError(w, http.StatusForbidden, "principal does not belong to tenant", "FORBIDDEN", requestID)
		return
	}

	// Verify toolset exists
	toolset, err := h.store.GetToolsetRecord(ctx, tenantID, req.ToolsetID, req.Revision)
	if err != nil {
		h.logger.Error("failed to get toolset", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to verify toolset", "STORE_ERROR", requestID)
		return
	}
	if toolset == nil {
		h.writeError(w, http.StatusBadRequest, "toolset not found: "+req.ToolsetID+" revision "+req.Revision, "TOOLSET_NOT_FOUND", requestID)
		return
	}

	// Apply toolset to principal
	if err := h.store.SetPrincipalActiveToolset(ctx, tenantID, principalID, req.ToolsetID, req.Revision, authCtx.UserID); err != nil {
		h.logger.Error("failed to apply toolset", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to apply toolset", "STORE_ERROR", requestID)
		return
	}

	h.logger.Info("toolset applied to principal",
		zap.String("tenant_id", tenantID),
		zap.String("principal_id", principalID),
		zap.String("toolset_id", req.ToolsetID),
		zap.String("revision", req.Revision),
		zap.String("assigned_by", authCtx.UserID),
	)

	writeJSON(w, http.StatusOK, ApplyToolsetResponse{
		PrincipalID: principalID,
		ToolsetID:   req.ToolsetID,
		Revision:    req.Revision,
		AppliedAt:   time.Now().UTC().Format(time.RFC3339),
	})
}

// writeError writes an error response.
func (h *ToolsetsHandler) writeError(w http.ResponseWriter, status int, message, code, requestID string) {
	resp := types.ErrorResponse{
		Error:     message,
		Code:      code,
		RequestID: requestID,
	}
	writeJSON(w, status, resp)
}
