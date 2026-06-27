package access

import (
	"context"
	"errors"

	"github.com/kidcarmi/nac/internal/domain"
)

// policySource supplies the enabled policy set. The policy.Service satisfies
// this interface; defining it here keeps the access service decoupled from the
// policy package and avoids an import cycle.
type policySource interface {
	EnabledPolicies(ctx context.Context) ([]domain.Policy, error)
}

// endpointSource resolves an endpoint by MAC, used to enrich evaluation with
// the endpoint's known posture and attributes. A miss is tolerated: an unknown
// device is still evaluated (typically into quarantine/deny by policy).
type endpointSource interface {
	GetByMAC(ctx context.Context, mac string) (*domain.Endpoint, error)
}

// Service evaluates access requests from network enforcement points.
type Service struct {
	engine    *Engine
	policies  policySource
	endpoints endpointSource
}

// NewService wires the engine to its data sources.
func NewService(engine *Engine, policies policySource, endpoints endpointSource) *Service {
	return &Service{engine: engine, policies: policies, endpoints: endpoints}
}

// EvaluateInput is an access request from an enforcement point. The MAC is
// required and already-normalized by the caller; the remaining fields override
// or supplement what is on record for the endpoint (e.g. a freshly observed IP
// or a posture result from an inline assessment).
type EvaluateInput struct {
	MAC        string
	IP         string
	Posture    domain.PostureStatus
	Attributes map[string]string
}

// Evaluate resolves the endpoint, merges request-time signals, and runs the
// policy engine. It never returns ErrNotFound for an unknown endpoint — an
// unknown device is a legitimate (and important) thing to make a decision
// about — but it does propagate infrastructure errors from the policy source.
func (s *Service) Evaluate(ctx context.Context, in EvaluateInput) (domain.AccessDecision, error) {
	ep := domain.Endpoint{MAC: in.MAC, Posture: domain.PostureUnknown}

	if s.endpoints != nil {
		switch known, err := s.endpoints.GetByMAC(ctx, in.MAC); {
		case err == nil:
			ep = *known
		case errors.Is(err, domain.ErrNotFound):
			// Leave ep as the minimal unknown endpoint.
		default:
			return domain.AccessDecision{}, err
		}
	}

	// Request-time signals override stored values when present.
	if in.IP != "" {
		ep.IP = in.IP
	}
	if in.Posture != "" {
		ep.Posture = in.Posture
	}
	if len(in.Attributes) > 0 {
		merged := make(map[string]string, len(ep.Attributes)+len(in.Attributes))
		for k, v := range ep.Attributes {
			merged[k] = v
		}
		for k, v := range in.Attributes {
			merged[k] = v
		}
		ep.Attributes = merged
	}

	policies, err := s.policies.EnabledPolicies(ctx)
	if err != nil {
		return domain.AccessDecision{}, err
	}
	return s.engine.Evaluate(ep, policies), nil
}
