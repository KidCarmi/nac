package pilot

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// ASTRA (pilot-aruba-rc3, session 0e816de7…): with accounting enabled, a
// stored accounting Start was contradicted by "NAC receives no RADIUS
// accounting". The note must state what NAC received, and why it still does
// not prove presence.
func TestLivenessWordingFollowsAccounting(t *testing.T) {
	now := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)
	sid := uuid.New()
	sess := domain.Session{ID: sid, State: domain.SessionActive, LastAuthAt: now.Add(-90 * time.Second)}
	acct := func(typ, reason string, at time.Time, session uuid.UUID) domain.AuthEvent {
		return domain.AuthEvent{SessionID: &session, Stage: domain.StageAccounting, Reason: reason, OccurredAt: at,
			Detail: map[string]string{"acct_status_type": typ, "acct_session_id": "1794252964825"}}
	}
	unobserved := Observed{Freshness: "unavailable"}

	// No accounting at all (the original lab setup).
	l := liveness(sess, unobserved, lastAccounting(sid, nil), now)
	if l.State != "unknown" || !strings.Contains(l.Note, "has received no RADIUS accounting") || l.Basis != "last authentication only" {
		t.Fatalf("no accounting: %+v", l)
	}

	// Accounting Start received: say so, keep "unknown", explain why.
	evs := []domain.AuthEvent{acct("Start", "accounting Start", now.Add(-60*time.Second), sid)}
	l = liveness(sess, unobserved, lastAccounting(sid, evs), now)
	if l.State != "unknown" || strings.Contains(l.Note, "no RADIUS accounting") ||
		!strings.Contains(l.Note, "received RADIUS accounting Start for this session 1m0s ago (Acct-Session-Id 1794252964825) and no Stop since") ||
		!strings.Contains(l.Note, "does not prove the endpoint is connected now") || l.Basis != "accounting Start, no Stop" {
		t.Fatalf("accounting Start: %+v", l)
	}

	// The latest accepted packet wins; ignored (stale) packets and other
	// sessions' packets never count.
	evs = append(evs,
		acct("Interim-Update", "accounting Interim-Update", now.Add(-10*time.Second), sid),
		acct("Start", "accounting Start ignored: Acct-Session-Id belongs to an earlier connection", now.Add(-5*time.Second), sid),
		acct("Start", "accounting Start", now.Add(-1*time.Second), uuid.New()))
	if a := lastAccounting(sid, evs); a == nil || a.Type != "Interim-Update" || !a.At.Equal(now.Add(-10*time.Second)) {
		t.Fatalf("latest accepted packet: %+v", a)
	}
	onlyIgnored := []domain.AuthEvent{acct("Start", "accounting Start ignored: stale Class", now, sid)}
	if a := lastAccounting(sid, onlyIgnored); a != nil {
		t.Fatalf("an ignored packet must not count as received accounting: %+v", a)
	}

	// A fresh observation still decides when there is one.
	l = liveness(sess, Observed{Freshness: "fresh", Status: "SUCCESS", Source: "eapi"}, lastAccounting(sid, evs), now)
	if l.State != "connected" {
		t.Fatalf("fresh observation: %+v", l)
	}
}
