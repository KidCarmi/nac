package server

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/pkg/problem"
)

// requestIDFrom returns the per-request correlation ID (the span ID from the
// trace context), or "" if absent.
func requestIDFrom(r *http.Request) string {
	if tc, ok := observability.TraceFrom(r.Context()); ok {
		return tc.SpanID
	}
	return ""
}

// statusRecorder captures the response status code for logging and metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// traceMiddleware establishes the W3C trace context, attaches a request-scoped
// logger, propagates the remote address for audit, and echoes the traceparent.
func traceMiddleware(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc := observability.FromTraceparent(r.Header.Get("traceparent"))
			ctx := observability.WithTrace(r.Context(), tc)

			logger := base.With(
				"request_id", tc.SpanID,
				"trace_id", tc.TraceID,
				"method", r.Method,
				"path", r.URL.Path,
			)
			ctx = observability.WithLogger(ctx, logger)
			ctx = audit.WithRemoteAddr(ctx, clientIP(r))

			w.Header().Set("traceparent", tc.Traceparent())
			w.Header().Set("X-Request-Id", tc.SpanID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// securityHeaders applies conservative security response headers suitable for
// a JSON API. A strict CSP is set because the API serves no HTML; it blocks
// any accidental script execution if a browser ever renders a response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// recoverer converts panics into 500 problem responses and logs the stack,
// preventing a single bad request from crashing the server.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				observability.LoggerFrom(r.Context()).Error("panic recovered", "panic", rec, "path", r.URL.Path)
				problem.New(http.StatusInternalServerError, "Internal server error", "An unexpected error occurred.").
					WithInstance(requestIDFrom(r)).Write(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// observe records request metrics and an access log line. The route label is
// supplied by the router via routePattern to keep cardinality bounded (raw
// paths with IDs would explode the metric series count).
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := routePattern(r)
		dur := s.now().Sub(start)
		s.metrics.HTTPRequests.WithLabelValues(r.Method, route, statusClass(rec.status)).Inc()
		s.metrics.HTTPDuration.WithLabelValues(r.Method, route).Observe(dur.Seconds())

		observability.LoggerFrom(r.Context()).Info("request",
			"status", rec.status,
			"route", route,
			"duration_ms", dur.Milliseconds(),
			"remote", clientIP(r),
		)
	})
}

// limitBody caps the request body size to defend against memory-exhaustion.
func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.HTTP.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// requireOperator authenticates a bearer JWT and stores the principal. On
// failure it records an auth-failure metric and returns 401 without detail.
func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			s.authFail(w, r, "missing_bearer")
			return
		}
		principal, err := s.tokens.Parse(tok)
		if err != nil {
			s.authFail(w, r, "invalid_token")
			return
		}
		ctx := auth.WithPrincipal(r.Context(), principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requirePermission enforces RBAC for an authenticated principal.
func (s *Server) requirePermission(perm auth.Permission, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.PrincipalFrom(r.Context())
		if !ok {
			writeProblem(w, r, domain.ErrUnauthorized)
			return
		}
		if !p.Can(perm) {
			writeProblem(w, r, domain.ErrForbidden)
			return
		}
		next(w, r)
	}
}

// requireServiceToken authenticates trusted network enforcement points calling
// the access-evaluation endpoint. The shared token is compared in constant
// time. This is intentionally separate from operator auth: enforcement points
// are machines, not users, and must never present operator credentials.
func (s *Server) requireServiceToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presented := bearerToken(r)
		if presented == "" {
			// FreeRADIUS rlm_rest authenticates with HTTP Basic; the token is
			// the password (the username is ignored).
			if _, pw, ok := r.BasicAuth(); ok {
				presented = pw
			}
		}
		expected := s.cfg.Auth.ServiceToken
		if expected == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
			s.authFail(w, r, "invalid_service_token")
			return
		}
		next(w, r)
	}
}

func (s *Server) authFail(w http.ResponseWriter, r *http.Request, reason string) {
	s.metrics.AuthFailures.WithLabelValues(reason).Inc()
	observability.LoggerFrom(r.Context()).Warn("authentication failed", "reason", reason, "remote", clientIP(r))
	w.Header().Set("WWW-Authenticate", `Bearer realm="nac"`)
	writeProblem(w, r, domain.ErrUnauthorized)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// rateLimiter is a simple fixed-window in-memory limiter keyed by client IP.
// It bounds abuse from a single source. For multi-instance deployments a
// shared Redis limiter is the documented next step (see docs/security and the
// scalability strategy); this in-process limiter is a safe, dependency-free
// default that degrades gracefully.
type rateLimiter struct {
	mu       sync.Mutex
	counts   map[string]int
	window   time.Time
	limit    int
	duration time.Duration
	now      func() time.Time
}

func newRateLimiter(limitPerMin int, now func() time.Time) *rateLimiter {
	return &rateLimiter{
		counts:   make(map[string]int),
		window:   now(),
		limit:    limitPerMin,
		duration: time.Minute,
		now:      now,
	}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.window) >= l.duration {
		l.counts = make(map[string]int)
		l.window = now
	}
	l.counts[key]++
	return l.counts[key] <= l.limit
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow(clientIP(r)) {
			problem.New(http.StatusTooManyRequests, "Too many requests", "Rate limit exceeded; retry later.").
				WithInstance(requestIDFrom(r)).Write(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts a best-effort client IP. It trusts X-Forwarded-For only
// for its first hop; deployments behind a proxy should pair this with a
// trusted-proxy configuration (documented in the admin guide).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

// routePattern returns the matched ServeMux route pattern (e.g.
// "GET /api/v1/endpoints/{id}") for use as a bounded-cardinality metric label.
// Go's ServeMux populates r.Pattern on a match; unmatched requests fall back
// to a constant so 404 floods cannot explode the metric series.
func routePattern(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}

// chain applies middlewares in order so the first listed runs outermost.
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
