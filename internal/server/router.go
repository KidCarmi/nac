package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	openapi "github.com/kidcarmi/nac/api/openapi"
	"github.com/kidcarmi/nac/internal/auth"
)

// buildHandler constructs the routing table and wraps it in the global
// middleware stack. Route patterns use Go 1.22+ method-aware ServeMux syntax.
//
// Middleware ordering (outermost first) is deliberate:
//  1. recoverer        — a panic anywhere below still yields a clean 500
//  2. trace+logger     — every subsequent log line carries correlation IDs
//  3. securityHeaders  — applied to all responses, including errors
//  4. observe          — metrics/access log wrap the handler timing
//  5. rateLimit        — cheap rejection before body parsing
//  6. limitBody        — cap memory before any handler reads the body
//
// Authentication and authorization are applied per-route, not globally,
// because the public endpoints (health, login, openapi) and the
// service-token endpoint (access evaluation) have different requirements.
func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// ---- Public, unauthenticated system endpoints ----
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /version", s.handleVersion)
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openapi.Spec())
	})
	// Metrics endpoint for Prometheus scraping.
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.metrics.Registry(), promhttp.HandlerOpts{}))

	// ---- Authentication ----
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// ---- Operator-authenticated API (JWT + RBAC) ----
	authed := func(perm auth.Permission, h http.HandlerFunc) http.Handler {
		// requireOperator establishes the principal; requirePermission enforces
		// RBAC for the specific capability.
		return s.requireOperator(s.requirePermission(perm, h))
	}

	mux.Handle("POST /api/v1/endpoints", authed(auth.PermEndpointWrite, s.handleCreateEndpoint))
	mux.Handle("GET /api/v1/endpoints", authed(auth.PermEndpointRead, s.handleListEndpoints))
	mux.Handle("GET /api/v1/endpoints/{id}", authed(auth.PermEndpointRead, s.handleGetEndpoint))
	mux.Handle("PUT /api/v1/endpoints/{id}", authed(auth.PermEndpointWrite, s.handleUpdateEndpoint))
	mux.Handle("DELETE /api/v1/endpoints/{id}", authed(auth.PermEndpointWrite, s.handleDeleteEndpoint))

	mux.Handle("POST /api/v1/policies", authed(auth.PermPolicyWrite, s.handleCreatePolicy))
	mux.Handle("GET /api/v1/policies", authed(auth.PermPolicyRead, s.handleListPolicies))
	mux.Handle("GET /api/v1/policies/{id}", authed(auth.PermPolicyRead, s.handleGetPolicy))
	mux.Handle("PUT /api/v1/policies/{id}", authed(auth.PermPolicyWrite, s.handleUpdatePolicy))
	mux.Handle("DELETE /api/v1/policies/{id}", authed(auth.PermPolicyWrite, s.handleDeletePolicy))

	mux.Handle("GET /api/v1/audit", authed(auth.PermAuditRead, s.handleListAudit))

	// ---- Versioned policy: the /policies routes above edit the draft ----
	if s.versions != nil {
		mux.Handle("GET /api/v1/policy-inputs", authed(auth.PermPolicyRead, s.handlePolicyInputs))
		mux.Handle("GET /api/v1/policy-draft", authed(auth.PermPolicyRead, s.handleGetDraft))
		mux.Handle("PUT /api/v1/policy-draft/settings", authed(auth.PermPolicyWrite, s.handleDraftSettings))
		mux.Handle("GET /api/v1/policy-draft/preview", authed(auth.PermPolicyRead, s.handlePreviewDraft))
		mux.Handle("POST /api/v1/policy-draft/publish", authed(auth.PermPolicyWrite, s.handlePublishDraft))
		mux.Handle("GET /api/v1/policy-versions", authed(auth.PermPolicyRead, s.handleListVersions))
		mux.Handle("GET /api/v1/policy-versions/{number}", authed(auth.PermPolicyRead, s.handleGetVersion))
		mux.Handle("POST /api/v1/policy-versions/{number}/restore", authed(auth.PermPolicyWrite, s.handleRestoreVersion))
		mux.Handle("GET /api/v1/policy-activations", authed(auth.PermPolicyRead, s.handleListActivations))
	}

	// ---- Service-token endpoint for network enforcement points ----
	mux.Handle("POST /api/v1/access/evaluate", s.requireServiceToken(s.handleEvaluateAccess))

	// Global middleware stack (outermost first).
	return chain(mux,
		recoverer,
		traceMiddleware(s.logger),
		securityHeaders,
		s.observe,
		s.rateLimit,
		s.limitBody,
	)
}
