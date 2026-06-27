package policy

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

type memRepo struct {
	byID map[uuid.UUID]*domain.Policy
}

func newMemRepo() *memRepo { return &memRepo{byID: map[uuid.UUID]*domain.Policy{}} }

func (m *memRepo) Create(_ context.Context, p *domain.Policy) error {
	cp := *p
	m.byID[p.ID] = &cp
	return nil
}
func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Policy, error) {
	p, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *p
	return &cp, nil
}
func (m *memRepo) Update(_ context.Context, p *domain.Policy) error {
	if _, ok := m.byID[p.ID]; !ok {
		return domain.ErrNotFound
	}
	cp := *p
	m.byID[p.ID] = &cp
	return nil
}
func (m *memRepo) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.byID[id]; !ok {
		return domain.ErrNotFound
	}
	delete(m.byID, id)
	return nil
}
func (m *memRepo) List(_ context.Context, _ domain.Page) ([]domain.Policy, error) {
	return m.enabledOrAll(false), nil
}
func (m *memRepo) ListEnabled(_ context.Context) ([]domain.Policy, error) {
	return m.enabledOrAll(true), nil
}
func (m *memRepo) enabledOrAll(onlyEnabled bool) []domain.Policy {
	out := []domain.Policy{}
	for _, p := range m.byID {
		if onlyEnabled && !p.Enabled {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}

// countingCache records invalidations to assert cache coherence.
type countingCache struct {
	policies      []domain.Policy
	hasValue      bool
	invalidations int
}

func (c *countingCache) GetEnabled(context.Context) ([]domain.Policy, bool) {
	return c.policies, c.hasValue
}
func (c *countingCache) SetEnabled(_ context.Context, p []domain.Policy) {
	c.policies = p
	c.hasValue = true
}
func (c *countingCache) Invalidate(context.Context) {
	c.hasValue = false
	c.invalidations++
}

func validInput() Input {
	return Input{
		Name:     "allow-laptops",
		Priority: 10,
		Enabled:  true,
		Action:   domain.ActionAllow,
		Segment:  "corp",
		Conditions: []domain.Condition{
			{Field: "device_type", Operator: domain.OpEquals, Values: []string{"laptop"}},
		},
	}
}

func TestCreate_Valid(t *testing.T) {
	cache := &countingCache{}
	svc := NewService(newMemRepo(), cache)
	p, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == uuid.Nil {
		t.Fatalf("expected generated ID")
	}
	if cache.invalidations != 1 {
		t.Fatalf("expected cache invalidation on create, got %d", cache.invalidations)
	}
}

func TestCreate_RejectsInvalid(t *testing.T) {
	svc := NewService(newMemRepo(), &countingCache{})
	cases := map[string]func(*Input){
		"empty name":     func(in *Input) { in.Name = "" },
		"bad action":     func(in *Input) { in.Action = "nuke" },
		"eq two values":  func(in *Input) { in.Conditions[0].Values = []string{"a", "b"} },
		"in zero values": func(in *Input) { in.Conditions[0].Operator = domain.OpIn; in.Conditions[0].Values = nil },
		"bad operator":   func(in *Input) { in.Conditions[0].Operator = "regex" },
		"neg priority":   func(in *Input) { in.Priority = -1 },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			mut(&in)
			if _, err := svc.Create(context.Background(), in); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}

func TestEnabledPolicies_UsesCache(t *testing.T) {
	repo := newMemRepo()
	cache := &countingCache{}
	svc := NewService(repo, cache)
	if _, err := svc.Create(context.Background(), validInput()); err != nil {
		t.Fatalf("create: %v", err)
	}
	// First call populates cache from repo.
	first, err := svc.EnabledPolicies(context.Background())
	if err != nil || len(first) != 1 {
		t.Fatalf("expected 1 enabled policy, got %d (%v)", len(first), err)
	}
	if !cache.hasValue {
		t.Fatalf("expected cache to be populated")
	}
	// Mutating the repo directly should not be visible until invalidation,
	// proving the cache is actually consulted.
	repo.byID = map[uuid.UUID]*domain.Policy{}
	second, _ := svc.EnabledPolicies(context.Background())
	if len(second) != 1 {
		t.Fatalf("expected cached result of 1, got %d", len(second))
	}
}

func TestDelete_InvalidatesCache(t *testing.T) {
	repo := newMemRepo()
	cache := &countingCache{}
	svc := NewService(repo, cache)
	p, _ := svc.Create(context.Background(), validInput())
	cache.invalidations = 0
	if err := svc.Delete(context.Background(), p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cache.invalidations != 1 {
		t.Fatalf("expected invalidation on delete")
	}
}
