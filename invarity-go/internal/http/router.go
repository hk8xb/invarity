// Package http provides HTTP handlers and routing for the Invarity Firewall.
package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"invarity/internal/auth"
	"invarity/internal/firewall"
	"invarity/internal/store"
)

// Router wraps chi.Router with Invarity-specific configuration.
type Router struct {
	*chi.Mux
	logger            *zap.Logger
	pipeline          *firewall.Pipeline
	cognitoVerifier   *auth.CognitoVerifier
	store             *store.DynamoDBStore
	s3Client          *store.S3Client
	onboardingHandler *OnboardingHandler
	toolsHandler      *ToolsHandler
	toolsetsHandler   *ToolsetsHandler
	tenantAuth        *auth.TenantAuthMiddleware
}

// RouterConfig holds configuration for creating a router.
type RouterConfig struct {
	Logger             *zap.Logger
	Pipeline           *firewall.Pipeline
	CognitoVerifier    *auth.CognitoVerifier  // Optional: for control plane auth
	Store              *store.DynamoDBStore   // Optional: for control plane endpoints
	S3Client           *store.S3Client        // Optional: for storing manifests
	EnableControlPlane bool                   // Whether to enable control plane endpoints
}

// NewRouter creates a new HTTP router with all routes configured.
func NewRouter(cfg RouterConfig) *Router {
	r := &Router{
		Mux:             chi.NewRouter(),
		logger:          cfg.Logger,
		pipeline:        cfg.Pipeline,
		cognitoVerifier: cfg.CognitoVerifier,
		store:           cfg.Store,
		s3Client:        cfg.S3Client,
	}

	// Initialize control plane handlers if enabled
	if cfg.EnableControlPlane && cfg.Store != nil {
		r.onboardingHandler = NewOnboardingHandler(cfg.Store, cfg.Logger)
		r.tenantAuth = auth.NewTenantAuthMiddleware(cfg.Store)
		r.toolsHandler = NewToolsHandler(cfg.Store, cfg.S3Client, cfg.Logger)
		r.toolsetsHandler = NewToolsetsHandler(cfg.Store, cfg.S3Client, cfg.Logger)
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
		// Firewall endpoints (no auth required for now - uses API keys in request)
		v1.Route("/firewall", func(fw chi.Router) {
			fw.Post("/evaluate", r.handleEvaluate)
		})

		// Control plane endpoints (Cognito auth required)
		if cfg.EnableControlPlane && r.cognitoVerifier != nil && r.onboardingHandler != nil {
			// Onboarding endpoints - require Cognito auth
			v1.Route("/onboarding", func(onb chi.Router) {
				onb.Use(r.cognitoVerifier.Middleware)
				onb.Post("/bootstrap", r.onboardingHandler.HandleBootstrap)
			})

			// User profile endpoint
			v1.Route("/me", func(me chi.Router) {
				me.Use(r.cognitoVerifier.Middleware)
				me.Get("/", r.onboardingHandler.HandleMe)
			})

			// Tenant-scoped endpoints - require Cognito auth + tenant membership
			v1.Route("/tenants/{tenant_id}", func(tenant chi.Router) {
				tenant.Use(r.cognitoVerifier.Middleware)
				tenant.Use(r.tenantAuth.RequireTenantMembership)

				// Principals (agents)
				tenant.Route("/principals", func(principals chi.Router) {
					principals.With(auth.RequireScope(auth.ScopePrincipalsRead)).Get("/", r.onboardingHandler.HandleListPrincipals)
					principals.With(auth.RequireScope(auth.ScopePrincipalsWrite)).Post("/", r.onboardingHandler.HandleCreatePrincipal)

					// Apply toolset to principal
					principals.With(auth.RequireScope(auth.ScopePrincipalsWrite)).Post("/{principal_id}/toolsets:apply", r.toolsetsHandler.HandleApplyToolset)
				})

				// Tools (tenant-scoped)
				tenant.Route("/tools", func(tools chi.Router) {
					tools.With(auth.RequireScope(auth.ScopeToolsRead)).Get("/", r.toolsHandler.HandleListTools)
					tools.With(auth.RequireScope(auth.ScopeToolsWrite)).Post("/", r.toolsHandler.HandleRegisterTool)
					tools.With(auth.RequireScope(auth.ScopeToolsRead)).Get("/{tool_id}/{version}", r.toolsHandler.HandleGetTool)
				})

				// Toolsets (tenant-scoped)
				tenant.Route("/toolsets", func(toolsets chi.Router) {
					toolsets.With(auth.RequireScope(auth.ScopeToolsetsRead)).Get("/", r.toolsetsHandler.HandleListToolsets)
					toolsets.With(auth.RequireScope(auth.ScopeToolsetsWrite)).Post("/", r.toolsetsHandler.HandleRegisterToolset)
					toolsets.With(auth.RequireScope(auth.ScopeToolsetsRead)).Get("/{toolset_id}/{revision}", r.toolsetsHandler.HandleGetToolset)
				})
			})
		}
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
