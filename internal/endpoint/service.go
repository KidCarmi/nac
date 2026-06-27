// Package endpoint implements the endpoint lifecycle service: the business
// rules for creating, updating, and listing network endpoints. All input is
// validated and normalized here so that every delivery adapter benefits from
// the same guarantees.
package endpoint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/pkg/validation"
)

const (
	maxPageLimit     = 200
	defaultPageLimit = 50
	maxHostnameLen   = 253
	maxAttributes    = 50
	maxAttrKeyLen    = 64
	maxAttrValueLen  = 512
)

// Service holds endpoint business logic.
type Service struct {
	repo domain.EndpointRepository
	now  func() time.Time
}

// NewService constructs an endpoint Service.
func NewService(repo domain.EndpointRepository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// CreateInput is the validated input for creating an endpoint.
type CreateInput struct {
	MAC        string
	IP         string
	Hostname   string
	DeviceType string
	Posture    domain.PostureStatus
	Attributes map[string]string
}

// Create validates input, normalizes the MAC, and persists a new endpoint.
func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Endpoint, error) {
	ve := validation.NewErrors()

	mac, err := validation.NormalizeMAC(in.MAC)
	if err != nil {
		ve.Add("mac", "must be a valid MAC address")
	}
	validateCommon(ve, in.IP, in.Hostname, in.DeviceType, in.Posture, in.Attributes)
	if ve.HasErrors() {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalidInput, ve.Error())
	}

	posture := in.Posture
	if posture == "" {
		posture = domain.PostureUnknown
	}
	now := s.now().UTC()
	e := &domain.Endpoint{
		ID:           uuid.New(),
		MAC:          mac,
		IP:           in.IP,
		Hostname:     in.Hostname,
		DeviceType:   in.DeviceType,
		AccessStatus: domain.AccessUnknown, // new endpoints start unauthorized
		Posture:      posture,
		Attributes:   in.Attributes,
		LastSeen:     now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.Create(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// Get returns an endpoint by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Endpoint, error) {
	return s.repo.GetByID(ctx, id)
}

// UpdateInput carries mutable endpoint fields.
type UpdateInput struct {
	IP           string
	Hostname     string
	DeviceType   string
	Posture      domain.PostureStatus
	AccessStatus domain.AccessStatus
	Attributes   map[string]string
}

// Update applies validated changes to an existing endpoint. The MAC address is
// immutable (it is the endpoint's identity); changing it would be a delete +
// create and is intentionally not supported here.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*domain.Endpoint, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	ve := validation.NewErrors()
	validateCommon(ve, in.IP, in.Hostname, in.DeviceType, in.Posture, in.Attributes)
	if in.AccessStatus != "" && !in.AccessStatus.Valid() {
		ve.Add("access_status", "invalid access status")
	}
	if ve.HasErrors() {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalidInput, ve.Error())
	}

	existing.IP = in.IP
	existing.Hostname = in.Hostname
	existing.DeviceType = in.DeviceType
	if in.Posture != "" {
		existing.Posture = in.Posture
	}
	if in.AccessStatus != "" {
		existing.AccessStatus = in.AccessStatus
	}
	existing.Attributes = in.Attributes
	existing.UpdatedAt = s.now().UTC()

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// Delete removes an endpoint by ID.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// List returns a page of endpoints matching the filter.
func (s *Service) List(ctx context.Context, f domain.EndpointFilter, p domain.Page) ([]domain.Endpoint, error) {
	if f.AccessStatus != "" && !f.AccessStatus.Valid() {
		return nil, fmt.Errorf("%w: invalid access_status filter", domain.ErrInvalidInput)
	}
	return s.repo.List(ctx, f, clampPage(p))
}

func validateCommon(ve *validation.Errors, ip, hostname, deviceType string, posture domain.PostureStatus, attrs map[string]string) {
	if ip != "" && !validation.IsIP(ip) {
		ve.Add("ip", "must be a valid IP address")
	}
	if hostname != "" && !validation.MaxLen(hostname, maxHostnameLen) {
		ve.Add("hostname", "exceeds maximum length")
	}
	if !validation.MaxLen(deviceType, 128) {
		ve.Add("device_type", "exceeds maximum length")
	}
	if posture != "" && !posture.Valid() {
		ve.Add("posture", "invalid posture status")
	}
	if len(attrs) > maxAttributes {
		ve.Add("attributes", "too many attributes")
	}
	for k, v := range attrs {
		if !validation.MaxLen(k, maxAttrKeyLen) || !validation.NonEmpty(k) {
			ve.Add("attributes", "attribute key invalid or too long")
			break
		}
		if !validation.MaxLen(v, maxAttrValueLen) {
			ve.Add("attributes", "attribute value too long")
			break
		}
	}
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

// IsNotFound is a convenience predicate used by adapters.
func IsNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }
