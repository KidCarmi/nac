package endpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// memRepo is an in-memory EndpointRepository for service-level unit tests.
type memRepo struct {
	byID  map[uuid.UUID]*domain.Endpoint
	byMAC map[string]uuid.UUID
}

func newMemRepo() *memRepo {
	return &memRepo{byID: map[uuid.UUID]*domain.Endpoint{}, byMAC: map[string]uuid.UUID{}}
}

func (m *memRepo) Create(_ context.Context, e *domain.Endpoint) error {
	if _, ok := m.byMAC[e.MAC]; ok {
		return domain.ErrConflict
	}
	cp := *e
	m.byID[e.ID] = &cp
	m.byMAC[e.MAC] = e.ID
	return nil
}
func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Endpoint, error) {
	e, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *e
	return &cp, nil
}
func (m *memRepo) GetByMAC(_ context.Context, mac string) (*domain.Endpoint, error) {
	id, ok := m.byMAC[mac]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return m.GetByID(context.Background(), id)
}
func (m *memRepo) Update(_ context.Context, e *domain.Endpoint) error {
	if _, ok := m.byID[e.ID]; !ok {
		return domain.ErrNotFound
	}
	cp := *e
	m.byID[e.ID] = &cp
	return nil
}
func (m *memRepo) Delete(_ context.Context, id uuid.UUID) error {
	e, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	delete(m.byMAC, e.MAC)
	delete(m.byID, id)
	return nil
}
func (m *memRepo) List(_ context.Context, _ domain.EndpointFilter, _ domain.Page) ([]domain.Endpoint, error) {
	out := make([]domain.Endpoint, 0, len(m.byID))
	for _, e := range m.byID {
		out = append(out, *e)
	}
	return out, nil
}

func TestCreate_NormalizesMACAndDefaults(t *testing.T) {
	svc := NewService(newMemRepo())
	e, err := svc.Create(context.Background(), CreateInput{MAC: "AA-BB-CC-DD-EE-FF"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("MAC not normalized: %q", e.MAC)
	}
	if e.AccessStatus != domain.AccessUnknown {
		t.Fatalf("new endpoint must default to unknown access, got %q", e.AccessStatus)
	}
	if e.Posture != domain.PostureUnknown {
		t.Fatalf("posture should default to unknown, got %q", e.Posture)
	}
}

func TestCreate_RejectsInvalidMAC(t *testing.T) {
	svc := NewService(newMemRepo())
	_, err := svc.Create(context.Background(), CreateInput{MAC: "not-a-mac"})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid input error, got %v", err)
	}
}

func TestCreate_RejectsBadIP(t *testing.T) {
	svc := NewService(newMemRepo())
	_, err := svc.Create(context.Background(), CreateInput{MAC: "aa:bb:cc:dd:ee:ff", IP: "999.999.0.1"})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid IP error, got %v", err)
	}
}

func TestCreate_DuplicateMACConflicts(t *testing.T) {
	svc := NewService(newMemRepo())
	in := CreateInput{MAC: "aa:bb:cc:dd:ee:ff"}
	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.Create(context.Background(), in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict on duplicate MAC, got %v", err)
	}
}

func TestUpdate_AppliesAccessStatus(t *testing.T) {
	svc := NewService(newMemRepo())
	e, _ := svc.Create(context.Background(), CreateInput{MAC: "aa:bb:cc:dd:ee:ff"})
	updated, err := svc.Update(context.Background(), e.ID, UpdateInput{AccessStatus: domain.AccessAuthorized})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.AccessStatus != domain.AccessAuthorized {
		t.Fatalf("expected authorized, got %q", updated.AccessStatus)
	}
}

func TestUpdate_RejectsBadAccessStatus(t *testing.T) {
	svc := NewService(newMemRepo())
	e, _ := svc.Create(context.Background(), CreateInput{MAC: "aa:bb:cc:dd:ee:ff"})
	_, err := svc.Update(context.Background(), e.ID, UpdateInput{AccessStatus: domain.AccessStatus("banished")})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestGet_NotFound(t *testing.T) {
	svc := NewService(newMemRepo())
	_, err := svc.Get(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
