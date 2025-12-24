// Package http provides HTTP handlers and routing for the Invarity Firewall.
package http

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"invarity/internal/types"
)

// handleHealthz handles the liveness probe.
func (r *Router) handleHealthz(w http.ResponseWriter, req *http.Request) {
	resp := types.HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleReadyz handles the readiness probe.
func (r *Router) handleReadyz(w http.ResponseWriter, req *http.Request) {
	checks := make(map[string]string)

	// TODO: Add actual readiness checks
	// - Registry store connectivity
	// - Policy store connectivity
	// - LLM endpoint connectivity
	checks["registry"] = "ok"
	checks["policy"] = "ok"
	checks["llm"] = "ok"

	allOk := true
	for _, status := range checks {
		if status != "ok" {
			allOk = false
			break
		}
	}

	status := "ok"
	httpStatus := http.StatusOK
	if !allOk {
		status = "not_ready"
		httpStatus = http.StatusServiceUnavailable
	}

	resp := types.HealthResponse{
		Status:    status,
		Timestamp: time.Now().UTC(),
		Checks:    checks,
	}
	writeJSON(w, httpStatus, resp)
}

// handleEvaluate handles the firewall evaluation endpoint.
func (r *Router) handleEvaluate(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	requestID := middleware.GetReqID(ctx)

	// Read and validate request body
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20)) // 1MB limit
	if err != nil {
		r.writeError(w, http.StatusBadRequest, "failed to read request body", "READ_ERROR", requestID)
		return
	}

	var toolCallReq types.ToolCallRequest
	if err := json.Unmarshal(body, &toolCallReq); err != nil {
		r.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "PARSE_ERROR", requestID)
		return
	}

	// Set request ID if not provided
	if toolCallReq.RequestID == "" {
		toolCallReq.RequestID = requestID
	}

	// Validate required fields
	if toolCallReq.OrgID == "" {
		r.writeError(w, http.StatusBadRequest, "org_id is required", "VALIDATION_ERROR", requestID)
		return
	}
	if toolCallReq.ToolCall.ActionID == "" {
		r.writeError(w, http.StatusBadRequest, "tool_call.action_id is required", "VALIDATION_ERROR", requestID)
		return
	}
	if toolCallReq.UserIntent == "" {
		r.writeError(w, http.StatusBadRequest, "user_intent is required", "VALIDATION_ERROR", requestID)
		return
	}

	// Run pipeline
	resp, err := r.pipeline.Evaluate(ctx, &toolCallReq)
	if err != nil {
		r.logger.Error("pipeline evaluation failed",
			zap.Error(err),
			zap.String("request_id", requestID),
		)
		r.writeError(w, http.StatusInternalServerError, "evaluation failed: "+err.Error(), "PIPELINE_ERROR", requestID)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// writeError writes an error response.
func (r *Router) writeError(w http.ResponseWriter, status int, message, code, requestID string) {
	resp := types.ErrorResponse{
		Error:     message,
		Code:      code,
		RequestID: requestID,
	}
	writeJSON(w, status, resp)
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
