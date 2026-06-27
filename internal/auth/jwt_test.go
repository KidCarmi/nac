package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

func newTestIssuer(t *testing.T, now func() time.Time) *TokenIssuer {
	t.Helper()
	ti, err := NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), "nac", "nac-api", time.Hour)
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}
	return ti.withClock(now)
}

func TestNewTokenIssuer_RejectsShortKey(t *testing.T) {
	if _, err := NewTokenIssuer([]byte("short"), "nac", "nac-api", time.Hour); err == nil {
		t.Fatalf("expected rejection of short key")
	}
}

func TestIssueAndParse_RoundTrip(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	ti := newTestIssuer(t, clock)
	op := &domain.Operator{ID: uuid.New(), Email: "a@b.c", Role: domain.RoleOperator}

	tok, exp, err := ti.Issue(op)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !exp.Equal(clock().Add(time.Hour)) {
		t.Fatalf("unexpected expiry %v", exp)
	}
	p, err := ti.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.ID != op.ID || p.Email != op.Email || p.Role != op.Role {
		t.Fatalf("principal mismatch: %+v", p)
	}
}

func TestParse_RejectsExpired(t *testing.T) {
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ti := newTestIssuer(t, func() time.Time { return issued })
	op := &domain.Operator{ID: uuid.New(), Email: "a@b.c", Role: domain.RoleViewer}
	tok, _, _ := ti.Issue(op)

	// Advance the clock beyond expiry.
	ti.withClock(func() time.Time { return issued.Add(2 * time.Hour) })
	if _, err := ti.Parse(tok); err == nil {
		t.Fatalf("expected expired token to be rejected")
	}
}

func TestParse_RejectsWrongKey(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	ti := newTestIssuer(t, clock)
	op := &domain.Operator{ID: uuid.New(), Email: "a@b.c", Role: domain.RoleAdmin}
	tok, _, _ := ti.Issue(op)

	other, _ := NewTokenIssuer([]byte("ffffffffffffffffffffffffffffffff"), "nac", "nac-api", time.Hour)
	other.withClock(clock)
	if _, err := other.Parse(tok); err == nil {
		t.Fatalf("expected token signed with different key to be rejected")
	}
}

func TestParse_RejectsAlgNone(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	ti := newTestIssuer(t, clock)
	// A hand-crafted alg=none token (header.payload. with empty signature).
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJzdWIiOiJ4Iiwicm9sZSI6ImFkbWluIn0."
	if _, err := ti.Parse(none); err == nil {
		t.Fatalf("expected alg=none token to be rejected")
	}
}
