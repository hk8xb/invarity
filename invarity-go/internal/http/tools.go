// Package http provides HTTP handlers and routing for the Invarity Firewall.
package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"invarity/internal/auth"
	"invarity/internal/store"
	"invarity/internal/types"
	"invarity/internal/util"
)

// ToolsHandler handles tool management endpoints.
type ToolsHandler struct {
	store    *store.DynamoDBStore
	s3Client *store.S3Client
	logger   *zap.Logger
}

// NewToolsHandler creates a new tools handler.
func NewToolsHandler(ddbStore *store.DynamoDBStore, s3Client *store.S3Client, logger *zap.Logger) *ToolsHandler {
	return &ToolsHandler{
		store:    ddbStore,
		s3Client: s3Client,
		logger:   logger,
	}
}

// RegisterToolRequest is the request body for POST /v1/tenants/{tenant_id}/tools.
type RegisterToolRequest struct {
	types.ToolManifestV3
}

// RegisterToolResponse is the response for POST /v1/tenants/{tenant_id}/tools.
type RegisterToolResponse struct {
	ToolID     string `json:"tool_id"`
	Version    string `json:"version"`
	SchemaHash string `json:"schema_hash"`
	IsNew      bool   `json:"is_new"`
	S3Key      string `json:"s3_key"`
}

// HandleRegisterTool handles POST /v1/tenants/{tenant_id}/tools.
// Registers a new tool or returns existing if idempotent.
func (h *ToolsHandler) HandleRegisterTool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Parse request
	var manifest types.ToolManifestV3
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "PARSE_ERROR", requestID)
		return
	}

	// Set schema version if not provided
	if manifest.SchemaVersion == "" {
		manifest.SchemaVersion = "3"
	}

	// Validate manifest
	if err := manifest.Validate(); err != nil {
		h.writeError(w, http.StatusBadRequest, "validation error: "+err.Error(), "VALIDATION_ERROR", requestID)
		return
	}

	// Compute schema hash if not provided
	if manifest.SchemaHash == "" {
		canonical, err := util.CanonicalJSON(manifest.ArgsSchema)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "failed to canonicalize args_schema", "SCHEMA_ERROR", requestID)
			return
		}
		hash := sha256.Sum256(canonical)
		manifest.SchemaHash = hex.EncodeToString(hash[:])
	}

	// Set timestamps
	now := time.Now().UTC()
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = now
	}
	manifest.UpdatedAt = now

	// Determine S3 key
	s3Key := store.ToolManifestKey(tenantID, manifest.ToolID, manifest.Version)

	// Get risk level for metadata
	riskLevel := manifest.RiskProfile.BaseRiskLevel
	if riskLevel == "" {
		riskLevel = "LOW"
	}

	// Try to upsert tool metadata in DynamoDB
	isNew, err := h.store.UpsertTool(ctx, tenantID, manifest.ToolID, manifest.Version, manifest.SchemaHash, manifest.Name, riskLevel, s3Key)
	if err != nil {
		// Check if it's a conflict error
		if isConflictError(err) {
			h.writeError(w, http.StatusConflict, err.Error(), "CONFLICT", requestID)
			return
		}
		h.logger.Error("failed to upsert tool", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to register tool", "STORE_ERROR", requestID)
		return
	}

	// If new, store manifest in S3
	if isNew && h.s3Client != nil {
		if err := h.s3Client.PutJSON(ctx, s3Key, manifest); err != nil {
			h.logger.Error("failed to store manifest in S3", zap.Error(err), zap.String("s3_key", s3Key))
			// Note: We don't fail the request since DynamoDB has the metadata
			// In production, you might want to handle this differently
		}
	}

	h.logger.Info("tool registered",
		zap.String("tenant_id", tenantID),
		zap.String("tool_id", manifest.ToolID),
		zap.String("version", manifest.Version),
		zap.Bool("is_new", isNew),
	)

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}

	writeJSON(w, status, RegisterToolResponse{
		ToolID:     manifest.ToolID,
		Version:    manifest.Version,
		SchemaHash: manifest.SchemaHash,
		IsNew:      isNew,
		S3Key:      s3Key,
	})
}

// ListToolsResponse is the response for GET /v1/tenants/{tenant_id}/tools.
type ListToolsResponse struct {
	Tools []ToolInfo `json:"tools"`
}

// ToolInfo is a summary of a tool.
type ToolInfo struct {
	ToolID     string `json:"tool_id"`
	Version    string `json:"version"`
	SchemaHash string `json:"schema_hash"`
	Name       string `json:"name"`
	RiskLevel  string `json:"risk_level"`
	Status     string `json:"status"`
}

// HandleListTools handles GET /v1/tenants/{tenant_id}/tools.
func (h *ToolsHandler) HandleListTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	tools, err := h.store.ListTools(ctx, tenantID, 100)
	if err != nil {
		h.logger.Error("failed to list tools", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to list tools", "STORE_ERROR", requestID)
		return
	}

	toolInfos := make([]ToolInfo, len(tools))
	for i, t := range tools {
		toolInfos[i] = ToolInfo{
			ToolID:     t.ToolID,
			Version:    t.Version,
			SchemaHash: t.SchemaHash,
			Name:       t.Name,
			RiskLevel:  t.RiskLevel,
			Status:     t.Status,
		}
	}

	writeJSON(w, http.StatusOK, ListToolsResponse{
		Tools: toolInfos,
	})
}

// GetToolResponse is the response for GET /v1/tenants/{tenant_id}/tools/{tool_id}/{version}.
type GetToolResponse struct {
	types.ToolManifestV3
}

// HandleGetTool handles GET /v1/tenants/{tenant_id}/tools/{tool_id}/{version}.
func (h *ToolsHandler) HandleGetTool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := middleware.GetReqID(ctx)
	tenantID := chi.URLParam(r, "tenant_id")
	toolID := chi.URLParam(r, "tool_id")
	version := chi.URLParam(r, "version")

	authCtx := auth.GetAuthContext(ctx)
	if authCtx == nil {
		h.writeError(w, http.StatusUnauthorized, "authentication required", "AUTH_REQUIRED", requestID)
		return
	}

	// Get tool record from DynamoDB
	record, err := h.store.GetToolRecord(ctx, tenantID, toolID, version)
	if err != nil {
		h.logger.Error("failed to get tool", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "failed to get tool", "STORE_ERROR", requestID)
		return
	}
	if record == nil {
		h.writeError(w, http.StatusNotFound, "tool not found", "NOT_FOUND", requestID)
		return
	}

	// Try to get full manifest from S3
	if h.s3Client != nil {
		var manifest types.ToolManifestV3
		if err := h.s3Client.GetJSON(ctx, record.S3Key, &manifest); err != nil {
			h.logger.Warn("failed to get manifest from S3, returning metadata only", zap.Error(err))
			// Fall back to metadata-only response
			writeJSON(w, http.StatusOK, ToolInfo{
				ToolID:     record.ToolID,
				Version:    record.Version,
				SchemaHash: record.SchemaHash,
				Name:       record.Name,
				RiskLevel:  record.RiskLevel,
				Status:     record.Status,
			})
			return
		}
		writeJSON(w, http.StatusOK, manifest)
		return
	}

	// No S3 client, return metadata only
	writeJSON(w, http.StatusOK, ToolInfo{
		ToolID:     record.ToolID,
		Version:    record.Version,
		SchemaHash: record.SchemaHash,
		Name:       record.Name,
		RiskLevel:  record.RiskLevel,
		Status:     record.Status,
	})
}

// writeError writes an error response.
func (h *ToolsHandler) writeError(w http.ResponseWriter, status int, message, code, requestID string) {
	resp := types.ErrorResponse{
		Error:     message,
		Code:      code,
		RequestID: requestID,
	}
	writeJSON(w, status, resp)
}

// isConflictError checks if an error is a conflict error.
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	// Check if error message contains "conflict"
	return len(err.Error()) >= 8 && err.Error()[:8] == "conflict"
}
