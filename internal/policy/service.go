// Package policy implements the policy lifecycle service. Policies drive the
// access-decision engine, so validation here is strict: malformed conditions
// or actions are rejected before they can ever influence a live access
// decision. Mutations invalidate the policy cache to keep the decision path
// consistent.
package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/pkg/validation"
)

const (
	maxPageLimit     = 200
	defaultPageLimit = 50
	maxNameLen       = 128
	maxDescLen       = 1024
	maxConditions    = 20
	maxValues        = 50
)

// Service holds policy business logic.
type Service struct {
	repo  domain.PolicyRepository
	cache domain.PolicyCache
	now   func() time.Time
}

// NewService constructs a policy Service. cache may be nil; callers should
// pass a no-op cache rather than nil to avoid nil checks, but the service
// tolerates nil for safety.
func NewService(repo domain.PolicyRepository, cache domain.PolicyCache) *Service {
	return &Service{repo: repo, cache: cache, now: time.Now}
}

// Input is the validated create/update payload for a policy.
type Input struct {
	Name        string
	Description string
	Priority    int
	Enabled     bool
	Conditions  []domain.Condition
	Action      domain.PolicyAction
	Segment     string
}

// Create validates and persists a new policy, invalidating the cache.
func (s *Service) Create(ctx context.Context, in Input) (*domain.Policy, error) {
	if err := validatePolicy(in); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	p := &domain.Policy{
		ID:          uuid.New(),
		Name:        in.Name,
		Description: in.Description,
		Priority:    in.Priority,
		Enabled:     in.Enabled,
		Conditions:  in.Conditions,
		Action:      in.Action,
		Segment:     in.Segment,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	s.invalidate(ctx)
	return p, nil
}

// Get returns a policy by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Policy, error) {
	return s.repo.GetByID(ctx, id)
}

// Update validates and persists changes to an existing policy.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*domain.Policy, error) {
	if err := validatePolicy(in); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	existing.Name = in.Name
	existing.Description = in.Description
	existing.Priority = in.Priority
	existing.Enabled = in.Enabled
	existing.Conditions = in.Conditions
	existing.Action = in.Action
	existing.Segment = in.Segment
	existing.UpdatedAt = s.now().UTC()
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	s.invalidate(ctx)
	return existing, nil
}

// Delete removes a policy and invalidates the cache.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidate(ctx)
	return nil
}

// List returns a page of policies.
func (s *Service) List(ctx context.Context, p domain.Page) ([]domain.Policy, error) {
	return s.repo.List(ctx, clampPage(p))
}

// EnabledPolicies returns enabled policies in priority order, served from the
// cache when available. This is the function the access service calls on the
// hot path.
func (s *Service) EnabledPolicies(ctx context.Context) ([]domain.Policy, error) {
	if s.cache != nil {
		if cached, ok := s.cache.GetEnabled(ctx); ok {
			return cached, nil
		}
	}
	policies, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		s.cache.SetEnabled(ctx, policies)
	}
	return policies, nil
}

func (s *Service) invalidate(ctx context.Context) {
	if s.cache != nil {
		s.cache.Invalidate(ctx)
	}
}

func validatePolicy(in Input) error {
	ve := validation.NewErrors()
	if !validation.NonEmpty(in.Name) || !validation.MaxLen(in.Name, maxNameLen) {
		ve.Add("name", "required, max 128 characters")
	}
	if !validation.MaxLen(in.Description, maxDescLen) {
		ve.Add("description", "exceeds maximum length")
	}
	if !in.Action.Valid() {
		ve.Add("action", "must be allow, deny, or quarantine")
	}
	if in.Priority < 0 {
		ve.Add("priority", "must be non-negative")
	}
	if len(in.Conditions) > maxConditions {
		ve.Add("conditions", "too many conditions")
	}
	for i, c := range in.Conditions {
		if !validation.NonEmpty(c.Field) || !validation.MaxLen(c.Field, 128) {
			ve.Add(fmt.Sprintf("conditions[%d].field", i), "required, max 128 characters")
		}
		if !c.Operator.Valid() {
			ve.Add(fmt.Sprintf("conditions[%d].operator", i), "invalid operator")
		}
		if len(c.Values) > maxValues {
			ve.Add(fmt.Sprintf("conditions[%d].values", i), "too many values")
		}
		switch c.Operator {
		case domain.OpEquals, domain.OpNotEqual:
			if len(c.Values) != 1 {
				ve.Add(fmt.Sprintf("conditions[%d].values", i), "eq/ne require exactly one value")
			}
		case domain.OpIn:
			if len(c.Values) == 0 {
				ve.Add(fmt.Sprintf("conditions[%d].values", i), "in requires at least one value")
			}
		}
	}
	if ve.HasErrors() {
		return fmt.Errorf("%w: %s", domain.ErrInvalidInput, ve.Error())
	}
	return nil
}

func clampPage(p domain.Page) domain.Page {
	if p.Limit <= 0 {
		p.Limit = defaultPageLimit
	}
	if p.Limit > maxPageLimit {
		p.Limit = maxPageLimit
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}
