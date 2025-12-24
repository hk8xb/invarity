// Package http provides HTTP handlers and routing for the Invarity Firewall.
package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"invarity/internal/firewall"
)

// Router wraps chi.Router with Invarity-specific configuration.
type Router struct {
	*chi.Mux
	logger   *zap.Logger
	pipeline *firewall.Pipeline
}

// RouterConfig holds configuration for creating a router.
type RouterConfig struct {
	Logger   *zap.Logger
	Pipeline *firewall.Pipeline
}

// NewRouter creates a new HTTP router with all routes configured.
func NewRouter(cfg RouterConfig) *Router {
	r := &Router{
		Mux:      chi.NewRouter(),
		logger:   cfg.Logger,
		pipeline: cfg.Pipeline,
	}

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(RequestLogger(cfg.Logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health endpoints (no auth)
	r.Get("/healthz", r.handleHealthz)
	r.Get("/readyz", r.handleReadyz)

	// API v1
	r.Route("/v1", func(v1 chi.Router) {
		// Firewall endpoints
		v1.Route("/firewall", func(fw chi.Router) {
			fw.Post("/evaluate", r.handleEvaluate)
		})
	})

	return r
}

// RequestLogger returns a middleware that logs requests.
func RequestLogger(logger *zap.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				logger.Info("request",
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", ww.Status()),
					zap.Int("bytes", ww.BytesWritten()),
					zap.Duration("duration", time.Since(start)),
					zap.String("request_id", middleware.GetReqID(r.Context())),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
