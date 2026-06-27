// Package server is the HTTP delivery adapter. It wires the domain services to
// a net/http router with a layered middleware stack and translates between the
// transport (JSON/HTTP) and the core. The router uses Go's standard ServeMux
// (method+pattern routing) to avoid a third-party router dependency.
package server

import (
	"context"
	"net/http"
	"time"

	"log/slog"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/endpoint"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/internal/policy"
)

// Service interfaces decouple the HTTP layer from concrete service types,
// which keeps handlers testable with lightweight fakes.

type endpointService interface {
	Create(ctx context.Context, in endpoint.CreateInput) (*domain.Endpoint, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Endpoint, error)
	Update(ctx context.Context, id uuid.UUID, in endpoint.UpdateInput) (*domain.Endpoint, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f domain.EndpointFilter, p domain.Page) ([]domain.Endpoint, error)
}

type policyService interface {
	Create(ctx context.Context, in policy.Input) (*domain.Policy, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Policy, error)
	Update(ctx context.Context, id uuid.UUID, in policy.Input) (*domain.Policy, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, p domain.Page) ([]domain.Policy, error)
}

type accessService interface {
	Evaluate(ctx context.Context, in access.EvaluateInput) (domain.AccessDecision, error)
}

// Deps is the explicit dependency set for the server. Constructing it in the
// composition root (cmd/nacd) keeps wiring in one place and makes the server
// trivially testable.
type Deps struct {
	Config    *config.Config
	Logger    *slog.Logger
	Metrics   *observability.Metrics
	Tokens    *auth.TokenIssuer
	Endpoints endpointService
	Policies  policyService
	Access    accessService
	Operators domain.OperatorRepository
	AuditRepo domain.AuditRepository
	Audit     *audit.Recorder
	// Ready checks critical dependencies for the readiness probe.
	Ready func(ctx context.Context) error
	// Now is injectable for deterministic tests; defaults to time.Now.
	Now func() time.Time
}

// Server holds the resolved dependencies and the constructed HTTP handler.
type Server struct {
	cfg       *config.Config
	logger    *slog.Logger
	metrics   *observability.Metrics
	tokens    *auth.TokenIssuer
	endpoints endpointService
	policies  policyService
	access    accessService
	operators domain.OperatorRepository
	auditRepo domain.AuditRepository
	audit     *audit.Recorder
	ready     func(ctx context.Context) error
	now       func() time.Time
	limiter   *rateLimiter
	handler   http.Handler
}

// New constructs a Server and builds its routing table.
func New(d Deps) *Server {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	ready := d.Ready
	if ready == nil {
		ready = func(context.Context) error { return nil }
	}
	s := &Server{
		cfg:       d.Config,
		logger:    d.Logger,
		metrics:   d.Metrics,
		tokens:    d.Tokens,
		endpoints: d.Endpoints,
		policies:  d.Policies,
		access:    d.Access,
		operators: d.Operators,
		auditRepo: d.AuditRepo,
		audit:     d.Audit,
		ready:     ready,
		now:       now,
		limiter:   newRateLimiter(d.Config.HTTP.RateLimitPerMin, now),
	}
	s.handler = s.buildHandler()
	return s
}

// Handler returns the fully-wrapped http.Handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Logger exposes the server logger for the composition root.
func (s *Server) Logger() *slog.Logger { return s.logger }
