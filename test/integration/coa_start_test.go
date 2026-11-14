//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/radius"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// W2 (owner review of PR #5/#6): an operator CoA, the override it implies
// and its timeline event must commit together, and nothing may be sent to
// the switch before that commit. Before the fix, RequestCoA committed the
// CoA ("requested") and then wrote the override in a second statement: a
// failure in between left a requested CoA that was never sent and that
// blocked every later CoA on the session until nacd restarted.
//
// postgres.WithCoAStartHook runs a callback at fixed points of StartCoA, so
// failures and competing writers are injected deterministically.

type coaRig struct {
	h    *harness
	veos *fakeVEOS
	pool *pgxpool.Pool // separate pool: what other connections see
	repo *postgres.PilotRepo
	auth func(acctID string) string
}

func newCoARig(t *testing.T) *coaRig {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	t.Cleanup(h.stop)
	veos := newFakeVEOS(t)
	pool, err := postgres.Connect(context.Background(), config.DBConfig{DSN: dsn(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := &coaRig{h: h, veos: veos, pool: pool, repo: postgres.NewPilotRepo(pool, nil)}
	h.do("POST", "/api/v1/switches", map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"coa_port": veos.port(), "segment_vlans": map[string]int{"corp": 10, "quarantine": 20}, "access_ports": []string{"Ethernet1"}}, 201, nil, true)
	var ep map[string]any
	h.do("POST", "/api/v1/endpoints", map[string]any{"mac": endpointMAC, "hostname": "h1"}, 201, &ep, true)
	h.do("POST", "/api/v1/identity-bindings", map[string]any{"endpoint_id": ep["id"], "issuer": caIssuer, "serial": certSerial}, 201, nil, true)
	h.do("POST", "/api/v1/policies", map[string]any{"name": "bound-corp", "priority": 10, "enabled": true, "action": "allow", "segment": "corp",
		"conditions": []map[string]any{{"field": "identity.bound", "operator": "eq", "values": []string{"true"}}}}, 201, nil, true)
	var pv map[string]any
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &pv, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": pv["draft_digest"]}, 201, nil, true)
	r.auth = func(acctID string) string {
		a := authAttrs()
		a["Acct-Session-Id"] = acctID
		return frVal(h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", a, 200), "reply:Tunnel-Private-Group-Id")
	}
	return r
}

func (r *coaRig) session(t *testing.T) uuid.UUID {
	t.Helper()
	if v := r.auth("acct-1"); v != "10" {
		t.Fatalf("baseline: VLAN %q", v)
	}
	var list struct{ Sessions []map[string]any }
	r.h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
	return uuid.MustParse(list.Sessions[0]["id"].(string))
}

func (r *coaRig) packets() int {
	r.veos.mu.Lock()
	defer r.veos.mu.Unlock()
	return len(r.veos.got)
}

// committed is what any other connection sees for the session.
type committed struct {
	override string
	coas     int
	opEvents int
}

func (r *coaRig) committed(t *testing.T, id uuid.UUID) committed {
	t.Helper()
	ctx := context.Background()
	var c committed
	if err := r.pool.QueryRow(ctx, `SELECT override FROM radius_sessions WHERE id=$1`, id).Scan(&c.override); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM coa_requests WHERE session_id=$1`, id).Scan(&c.coas); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM auth_events WHERE session_id=$1 AND stage=$2`, id, domain.StageOperator).Scan(&c.opEvents); err != nil {
		t.Fatal(err)
	}
	return c
}

// A failure at any point of the start - injected, or a real connection loss
// - leaves nothing: no CoA, no override, no event, no packet, and the next
// operator action is not blocked.
func TestCoAStart_FailureLeavesNothingAndNeverBlocks(t *testing.T) {
	r := newCoARig(t)
	ctx := context.Background()
	s1 := r.session(t)
	r.veos.mu.Lock()
	r.veos.silent = true
	r.veos.mu.Unlock()

	injected := errors.New("injected failure")
	// killTx terminates the backend of the open StartCoA transaction (the
	// only connection idle in a transaction while the hook runs): a real
	// database failure, not a Go error.
	killTx := func() error {
		var n int
		if err := r.pool.QueryRow(ctx, `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity
			WHERE datname = current_database() AND state LIKE 'idle in transaction%' AND pid <> pg_backend_pid()`).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("expected exactly one open transaction to terminate")
		}
		return nil
	}
	cases := []struct {
		name, stage string
		fault       func() error
	}{
		{"error after lock", "locked", func() error { return injected }},
		{"error after CoA insert", "coa_inserted", func() error { return injected }},
		{"error after override write", "override_set", func() error { return injected }},
		{"error immediately before COMMIT", "event_inserted", func() error { return injected }},
		{"connection killed after CoA insert", "coa_inserted", killTx},
		{"connection killed before COMMIT", "event_inserted", killTx},
	}
	for _, tc := range cases {
		hook := func(stage string) error {
			if stage == tc.stage {
				return tc.fault()
			}
			return nil
		}
		if _, err := r.h.app.Pilot().RequestCoA(postgres.WithCoAStartHook(ctx, hook), s1, domain.CoAQuarantine, adminEmail); err == nil {
			t.Fatalf("%s: RequestCoA succeeded", tc.name)
		}
		r.h.app.Pilot().Wait()
		if got := r.committed(t, s1); got != (committed{}) {
			t.Fatalf("%s: partial start committed: %+v", tc.name, got)
		}
		if n := r.packets(); n != 0 {
			t.Fatalf("%s: %d CoA packet(s) sent for an uncommitted start", tc.name, n)
		}
	}

	// Not stuck: the next action starts normally and is sent.
	c, err := r.h.app.Pilot().RequestCoA(ctx, s1, domain.CoAQuarantine, adminEmail)
	if err != nil {
		t.Fatalf("after failed starts the session is blocked: %v", err)
	}
	waitFor(t, "CoA sent", func() bool { return r.packets() > 0 })
	if got := r.committed(t, s1); got != (committed{override: "quarantine", coas: 1, opEvents: 1}) {
		t.Fatalf("after a successful start: %+v", got)
	}
	r.h.app.Pilot().Wait()
	if cur, _ := r.repo.GetCoA(ctx, c.ID); cur.State == domain.CoARequested {
		t.Fatal("CoA left in requested")
	}
}

// Nothing reaches the switch before COMMIT, and no other connection can see
// the CoA or the override until then.
func TestCoAStart_NothingSentOrVisibleBeforeCommit(t *testing.T) {
	r := newCoARig(t)
	ctx := context.Background()
	s1 := r.session(t)
	r.veos.mu.Lock()
	r.veos.silent = true // records the request without answering, so the CoA ends quickly
	r.veos.mu.Unlock()
	var atCommit committed
	var sentBefore = -1
	hook := func(stage string) error {
		if stage == "event_inserted" {
			atCommit = r.committed(t, s1)
			time.Sleep(200 * time.Millisecond) // a sender racing the commit would have sent by now
			sentBefore = r.packets()
		}
		return nil
	}
	if _, err := r.h.app.Pilot().RequestCoA(postgres.WithCoAStartHook(ctx, hook), s1, domain.CoAQuarantine, adminEmail); err != nil {
		t.Fatal(err)
	}
	if sentBefore != 0 || atCommit != (committed{}) {
		t.Fatalf("before COMMIT: %d packet(s) sent, visible to others %+v", sentBefore, atCommit)
	}
	waitFor(t, "CoA sent after commit", func() bool { return r.packets() >= 1 })
	r.h.app.Pilot().Wait()
}

// Stale preparation: an operator action is prepared from the session, then a
// competing Quarantine commits and completes before this action takes the
// row lock. The action must be prepared again from the new state: it may
// never record or send a CoA computed from the old one.
func TestCoAStart_StalePreparationIsRedone(t *testing.T) {
	r := newCoARig(t)
	ctx := context.Background()
	s1 := r.session(t)
	r.veos.mu.Lock()
	r.veos.onACK = func(*radius.Packet) { r.auth("acct-1") }
	r.veos.mu.Unlock()

	var locked atomic.Int32
	var competed sync.Once
	hook := func(stage string) error {
		switch stage {
		case "prepared":
			competed.Do(func() {
				// Prepared as reauthenticate with no override: corp VLAN 10.
				// Now another operator quarantines and the switch completes it.
				if _, err := r.h.app.Pilot().RequestCoA(ctx, s1, domain.CoAQuarantine, "other@nac.local"); err != nil {
					t.Errorf("competing quarantine: %v", err)
				}
				r.h.app.Pilot().Wait()
			})
		case "locked":
			locked.Add(1)
		}
		return nil
	}
	c, err := r.h.app.Pilot().RequestCoA(postgres.WithCoAStartHook(ctx, hook), s1, domain.CoAReauthenticate, adminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if c.RequestedVLAN == nil || *c.RequestedVLAN != 20 || c.RequestedSegment != "quarantine" {
		t.Fatalf("reauthenticate recorded from the stale session: segment %q VLAN %v", c.RequestedSegment, c.RequestedVLAN)
	}
	if locked.Load() != 2 {
		t.Fatalf("the stale preparation was not detected under the lock (locked %d time(s))", locked.Load())
	}
	if got := r.committed(t, s1); got.override != "quarantine" || got.coas != 2 || got.opEvents != 2 {
		t.Fatalf("after both actions: %+v", got)
	}
	r.h.app.Pilot().Wait()
}

// Many operators at once: exactly one start wins, and the committed state is
// exactly that one action (one CoA, its override, one event).
func TestCoAStart_ConcurrentActionsOneWinner(t *testing.T) {
	r := newCoARig(t)
	ctx := context.Background()
	s1 := r.session(t)
	r.veos.mu.Lock()
	r.veos.silent = true // keep the winner in flight
	r.veos.mu.Unlock()

	const n = 8
	var (
		wg             sync.WaitGroup
		gate           = make(chan struct{})
		wins, conflict atomic.Int32
	)
	for i := 0; i < n; i++ {
		kind := domain.CoAQuarantine
		if i%2 == 1 {
			kind = domain.CoAReauthenticate
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, err := r.h.app.Pilot().RequestCoA(ctx, s1, kind, adminEmail)
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, domain.ErrConflict):
				conflict.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(gate)
	wg.Wait()
	if wins.Load() != 1 || conflict.Load() != n-1 {
		t.Fatalf("%d winner(s), %d conflict(s)", wins.Load(), conflict.Load())
	}
	got := r.committed(t, s1)
	if got.coas != 1 || got.opEvents != 1 {
		t.Fatalf("committed %+v, want exactly one action", got)
	}
	var kind string
	if err := r.pool.QueryRow(ctx, `SELECT kind FROM coa_requests WHERE session_id=$1`, s1).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{domain.CoAQuarantine: "quarantine", domain.CoAReauthenticate: ""}[kind]; got.override != want {
		t.Fatalf("winner %s but override %q", kind, got.override)
	}
	r.h.app.Pilot().Wait()
}
