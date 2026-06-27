//go:build integration

// Package integration contains tests that exercise the real PostgreSQL
// adapters end to end. They are excluded from the default `go test ./...` run
// by the `integration` build tag and require a reachable database supplied via
// NAC_TEST_DB_DSN. Run them with:
//
//	NAC_TEST_DB_DSN=postgres://nac:nac@localhost:5432/nac_test?sslmode=disable \
//	  go test -tags=integration ./test/integration/...
//
// The compose stack in deployments/compose brings up a suitable database.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

func dsn(t *testing.T) string {
	t.Helper()
	d := os.Getenv("NAC_TEST_DB_DSN")
	if d == "" {
		t.Skip("NAC_TEST_DB_DSN not set; skipping integration test")
	}
	return d
}

func setup(t *testing.T) (*postgres.EndpointRepo, *postgres.PolicyRepo, func()) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, config.DBConfig{DSN: dsn(t)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Clean slate for deterministic assertions.
	for _, table := range []string{"endpoints", "policies", "audit_events"} {
		if _, err := pool.Exec(ctx, "TRUNCATE "+table+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	return postgres.NewEndpointRepo(pool), postgres.NewPolicyRepo(pool), pool.Close
}

func TestEndpointRoundTrip(t *testing.T) {
	repo, _, closeFn := setup(t)
	defer closeFn()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	e := &domain.Endpoint{
		ID: uuid.New(), MAC: "aa:bb:cc:dd:ee:ff", Hostname: "laptop-1",
		DeviceType: "laptop", AccessStatus: domain.AccessUnknown, Posture: domain.PostureCompliant,
		Attributes: map[string]string{"owner": "alice"},
		LastSeen:   now, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByMAC(ctx, e.MAC)
	if err != nil {
		t.Fatalf("get by mac: %v", err)
	}
	if got.Hostname != "laptop-1" || got.Attributes["owner"] != "alice" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Duplicate MAC must conflict.
	dup := *e
	dup.ID = uuid.New()
	if err := repo.Create(ctx, &dup); err == nil {
		t.Fatalf("expected conflict on duplicate MAC")
	}
}

func TestPolicyEnabledOrdering(t *testing.T) {
	_, repo, closeFn := setup(t)
	defer closeFn()
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(name string, prio int, enabled bool) *domain.Policy {
		return &domain.Policy{
			ID: uuid.New(), Name: name, Priority: prio, Enabled: enabled,
			Action: domain.ActionAllow, Conditions: []domain.Condition{}, CreatedAt: now, UpdatedAt: now,
		}
	}
	for _, p := range []*domain.Policy{mk("c", 30, true), mk("a", 10, true), mk("b", 20, false)} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("create %s: %v", p.Name, err)
		}
	}
	enabled, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled policies, got %d", len(enabled))
	}
	if enabled[0].Name != "a" || enabled[1].Name != "c" {
		t.Fatalf("expected priority order [a,c], got [%s,%s]", enabled[0].Name, enabled[1].Name)
	}
}
