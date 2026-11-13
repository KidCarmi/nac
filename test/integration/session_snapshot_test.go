//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/pilot"
	"github.com/kidcarmi/nac/internal/radius"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// Session detail and list must describe ONE committed moment (owner review of
// PR #5, after CI caught GET /sessions/{id} returning the session before a
// CoA was observed together with the CoA after it).
//
// These tests are deterministic: postgres.WithSnapshotHook runs a callback
// inside the read transaction right after the statement that fixes the
// snapshot. The callback commits a real writer (an operator Quarantine or
// Restore, a CoA state update, a RADIUS session replacement) and, as a
// control, shows that a separate READ COMMITTED query issued at that same
// point DOES see the write: that is exactly what the old, statement-by-
// statement read did next. The response under test is then checked as
// returned, without a second GET that could mask it.

type snapshotRig struct {
	h    *harness
	veos *fakeVEOS
	repo *postgres.PilotRepo // separate pool: the READ COMMITTED control reads
	acct func(status, id string)
	auth func(acctID string) string
}

func newSnapshotRig(t *testing.T) *snapshotRig {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	t.Cleanup(h.stop)
	veos := newFakeVEOS(t)
	pool, err := postgres.Connect(context.Background(), config.DBConfig{DSN: dsn(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := &snapshotRig{h: h, veos: veos, repo: postgres.NewPilotRepo(pool, nil)}

	var sw map[string]any
	h.do("POST", "/api/v1/switches", map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"coa_port": veos.port(), "segment_vlans": map[string]int{"corp": 10, "quarantine": 20}, "access_ports": []string{"Ethernet1"}}, 201, &sw, true)
	var ep map[string]any
	h.do("POST", "/api/v1/endpoints", map[string]any{"mac": endpointMAC, "hostname": "h1"}, 201, &ep, true)
	h.do("POST", "/api/v1/identity-bindings", map[string]any{"endpoint_id": ep["id"], "issuer": caIssuer, "serial": certSerial}, 201, nil, true)
	h.do("POST", "/api/v1/policies", map[string]any{"name": "bound-corp", "priority": 10, "enabled": true, "action": "allow", "segment": "corp",
		"conditions": []map[string]any{{"field": "identity.bound", "operator": "eq", "values": []string{"true"}}}}, 201, nil, true)
	var pv map[string]any
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &pv, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": pv["draft_digest"]}, 201, nil, true)

	r.acct = func(status, id string) {
		h.radiusCall("/api/v1/radius/accounting", "127.0.0.1", map[string]string{"Acct-Status-Type": status, "Acct-Session-Id": id,
			"Calling-Station-Id": "00-50-00-00-02-00", "NAS-Port-Id": "Ethernet1", "NAS-IP-Address": "127.0.0.1"}, 204)
	}
	r.auth = func(acctID string) string {
		a := authAttrs()
		a["Acct-Session-Id"] = acctID
		return frVal(h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", a, 200), "reply:Tunnel-Private-Group-Id")
	}
	return r
}

// activeSession authenticates the endpoint and returns its session ID.
func (r *snapshotRig) activeSession(t *testing.T, acctID string) uuid.UUID {
	t.Helper()
	if v := r.auth(acctID); v != "10" {
		t.Fatalf("baseline authorization: VLAN %q", v)
	}
	r.acct("Start", acctID)
	var list struct{ Sessions []map[string]any }
	r.h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
	if len(list.Sessions) != 1 {
		t.Fatalf("want one active session, got %d", len(list.Sessions))
	}
	return uuid.MustParse(list.Sessions[0]["id"].(string))
}

// once wraps a hook so it acts on the first snapshot only.
func once(t *testing.T, want string, fn func()) func(string) {
	var o sync.Once
	return func(stage string) {
		if stage != want {
			t.Errorf("snapshot hook stage %q, want %q", stage, want)
			return
		}
		o.Do(fn)
	}
}

// coherent checks the invariants a single snapshot guarantees, on the
// response as encoded for the API.
func coherent(t *testing.T, d *pilot.SessionDetail) {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var api struct {
		LastCoA  *domain.CoARequest  `json:"last_coa"`
		CoAs     []domain.CoARequest `json:"coa_requests"`
		Override string              `json:"override"`
		Timeline []pilot.TimelineEntry
	}
	if err := json.Unmarshal(raw, &api); err != nil {
		t.Fatal(err)
	}
	if (api.LastCoA == nil) != (len(api.CoAs) == 0) {
		t.Fatalf("last_coa %v but %d coa_requests", api.LastCoA, len(api.CoAs))
	}
	known := map[uuid.UUID]domain.CoARequest{}
	for _, c := range api.CoAs {
		known[c.ID] = c
	}
	if api.LastCoA != nil {
		last := api.CoAs[len(api.CoAs)-1]
		if api.LastCoA.ID != last.ID || api.LastCoA.State != last.State || !api.LastCoA.UpdatedAt.Equal(last.UpdatedAt) {
			t.Fatalf("last_coa %s/%s is not the newest coa_requests entry %s/%s", api.LastCoA.ID, api.LastCoA.State, last.ID, last.State)
		}
	}
	coaRows := 0
	for _, e := range api.Timeline {
		if e.CoAID == nil {
			continue
		}
		c, ok := known[*e.CoAID]
		if !ok {
			t.Fatalf("timeline %s entry references CoA %s that is not in coa_requests", e.Kind, *e.CoAID)
		}
		if e.Kind == "coa" {
			coaRows++
			if e.Result != c.State {
				t.Fatalf("timeline shows CoA %s as %s, coa_requests as %s", c.ID, e.Result, c.State)
			}
		}
	}
	if coaRows != len(api.CoAs) {
		t.Fatalf("timeline has %d CoA rows for %d coa_requests", coaRows, len(api.CoAs))
	}
}

func TestSessionSnapshot_QuarantineCommittedMidRead(t *testing.T) {
	r := newSnapshotRig(t)
	ctx := context.Background()
	s1 := r.activeSession(t, "acct-1")
	r.veos.mu.Lock()
	r.veos.silent = true // the CoA exchange is not under test here
	r.veos.mu.Unlock()

	var coaID uuid.UUID
	hook := once(t, "session", func() {
		c, err := r.h.app.Pilot().RequestCoA(ctx, s1, "quarantine", adminEmail)
		if err != nil {
			t.Fatalf("quarantine: %v", err)
		}
		coaID = c.ID
		// Control: the next READ COMMITTED statement would see it.
		if coas, _ := r.repo.SessionCoAs(ctx, s1); len(coas) != 1 {
			t.Fatalf("control: a separate read sees %d CoAs, want the committed 1", len(coas))
		}
		if s, _ := r.repo.GetSession(ctx, s1); s.Override != "quarantine" {
			t.Fatalf("control: override %q not committed", s.Override)
		}
	})
	d, err := r.h.app.Pilot().SessionDetail(postgres.WithSnapshotHook(ctx, hook), s1)
	if err != nil {
		t.Fatal(err)
	}
	if coaID == uuid.Nil {
		t.Fatal("hook did not run")
	}
	coherent(t, d)
	if d.Override != "" || d.LastCoA != nil || len(d.CoAs) != 0 {
		t.Fatalf("response mixes the pre-Quarantine session with the Quarantine: override=%q last_coa=%v coa_requests=%d",
			d.Override, d.LastCoA, len(d.CoAs))
	}
	for _, e := range d.Timeline {
		if e.Stage == domain.StageOperator {
			t.Fatalf("timeline shows the operator action committed after the snapshot: %s", e.Summary)
		}
	}
	for _, l := range d.Explanation {
		if strings.Contains(l, "Operator override") {
			t.Fatalf("explanation describes an override the session row did not have: %s", l)
		}
	}

	// A later read sees the whole action, coherently.
	r.h.app.Pilot().Wait()
	d, err = r.h.app.Pilot().SessionDetail(ctx, s1)
	if err != nil {
		t.Fatal(err)
	}
	coherent(t, d)
	if d.Override != "quarantine" || d.LastCoA == nil || d.LastCoA.ID != coaID {
		t.Fatalf("after the action: override=%q last_coa=%v", d.Override, d.LastCoA)
	}
}

func TestSessionSnapshot_RestoreCommittedMidRead(t *testing.T) {
	r := newSnapshotRig(t)
	ctx := context.Background()
	s1 := r.activeSession(t, "acct-1")
	// A real Quarantine first: the switch ACKs and re-authenticates.
	r.veos.mu.Lock()
	r.veos.onACK = func(*radius.Packet) { r.auth("acct-1") }
	r.veos.mu.Unlock()
	q, err := r.h.app.Pilot().RequestCoA(ctx, s1, "quarantine", adminEmail)
	if err != nil {
		t.Fatal(err)
	}
	r.h.app.Pilot().Wait()
	before, err := r.h.app.Pilot().SessionDetail(ctx, s1)
	if err != nil {
		t.Fatal(err)
	}
	if before.Override != "quarantine" || before.LastCoA == nil || before.LastCoA.State != domain.CoAObserved {
		t.Fatalf("setup: override=%q last_coa=%v", before.Override, before.LastCoA)
	}
	r.veos.mu.Lock()
	r.veos.onACK, r.veos.silent = nil, true
	r.veos.mu.Unlock()

	hook := once(t, "session", func() {
		if _, err := r.h.app.Pilot().RequestCoA(ctx, s1, "restore", adminEmail); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if coas, _ := r.repo.SessionCoAs(ctx, s1); len(coas) != 2 {
			t.Fatalf("control: a separate read sees %d CoAs, want 2", len(coas))
		}
		if s, _ := r.repo.GetSession(ctx, s1); s.Override != "" {
			t.Fatalf("control: override %q, want the committed restore", s.Override)
		}
	})
	d, err := r.h.app.Pilot().SessionDetail(postgres.WithSnapshotHook(ctx, hook), s1)
	if err != nil {
		t.Fatal(err)
	}
	coherent(t, d)
	if d.Override != "quarantine" || len(d.CoAs) != 1 || d.LastCoA.ID != q.ID || d.LastCoA.State != domain.CoAObserved ||
		d.Requested.VLAN == nil || *d.Requested.VLAN != 20 {
		t.Fatalf("response mixes the quarantined session with the Restore: override=%q coa_requests=%d last_coa=%s/%s requested=%v",
			d.Override, len(d.CoAs), d.LastCoA.ID, d.LastCoA.State, deref(d.Requested.VLAN))
	}
	r.h.app.Pilot().Wait()
}

// The CI failure on PR #5: the switch re-authenticates (the RADIUS path
// writes the session, then marks the CoA observed) while the detail is read.
// last_coa, coa_requests, the timeline, requested and last_decision must all
// be from before the re-authentication, or all from after it.
func TestSessionSnapshot_CoAObservedMidRead(t *testing.T) {
	r := newSnapshotRig(t)
	ctx := context.Background()
	s1 := r.activeSession(t, "acct-1")
	c, err := r.h.app.Pilot().RequestCoA(ctx, s1, "quarantine", adminEmail) // ACKed; no re-authentication yet
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "CoA acked", func() bool {
		cur, err := r.repo.GetCoA(ctx, c.ID)
		return err == nil && cur.State == domain.CoAAcked
	})
	before, err := r.h.app.Pilot().SessionDetail(ctx, s1)
	if err != nil {
		t.Fatal(err)
	}

	hook := once(t, "session", func() {
		if v := r.auth("acct-1"); v != "20" {
			t.Fatalf("re-authentication: VLAN %q", v)
		}
		if cur, _ := r.repo.GetCoA(ctx, c.ID); cur.State != domain.CoAObserved {
			t.Fatalf("control: CoA %s, want observed committed", cur.State)
		}
	})
	d, err := r.h.app.Pilot().SessionDetail(postgres.WithSnapshotHook(ctx, hook), s1)
	if err != nil {
		t.Fatal(err)
	}
	coherent(t, d)
	if d.LastCoA.State != domain.CoAAcked || d.CoAs[len(d.CoAs)-1].State != domain.CoAAcked {
		t.Fatalf("CoA state from after the snapshot: last_coa %s, coa_requests %s", d.LastCoA.State, d.CoAs[len(d.CoAs)-1].State)
	}
	if d.Requested.VLAN == nil || *d.Requested.VLAN != 10 || *d.LastDecisionID != *before.LastDecisionID ||
		d.LastDecision.ID != *before.LastDecisionID || len(d.Decisions) != len(before.Decisions) {
		t.Fatalf("session/decisions from after the snapshot: requested %v, last decision %s (before %s), %d decisions (before %d)",
			deref(d.Requested.VLAN), d.LastDecision.ID, *before.LastDecisionID, len(d.Decisions), len(before.Decisions))
	}
	r.h.app.Pilot().Wait()
}

// Session replacement (accounting Stop, then a new authentication on the same
// port) committed while the OLD session is read: the response is the old
// session as it was, not "active" with the replacement's Stop in its timeline.
func TestSessionSnapshot_ReplacementCommittedMidRead(t *testing.T) {
	r := newSnapshotRig(t)
	ctx := context.Background()
	s1 := r.activeSession(t, "acct-1")
	hook := once(t, "session", func() {
		r.acct("Stop", "acct-1")
		r.auth("acct-2")
		r.acct("Start", "acct-2")
		if s, _ := r.repo.GetSession(ctx, s1); s.State != domain.SessionEnded {
			t.Fatalf("control: old session %s, want ended committed", s.State)
		}
	})
	d, err := r.h.app.Pilot().SessionDetail(postgres.WithSnapshotHook(ctx, hook), s1)
	if err != nil {
		t.Fatal(err)
	}
	coherent(t, d)
	if d.State != domain.SessionActive || d.EndedAt != nil || d.Liveness.State == "ended" {
		t.Fatalf("old session: state %s ended_at %v liveness %s", d.State, d.EndedAt, d.Liveness.State)
	}
	for _, e := range d.Timeline {
		if strings.Contains(e.Summary, "Stop") || strings.Contains(e.Summary, "acct-2") {
			t.Fatalf("timeline holds an event committed after the snapshot: %s", e.Summary)
		}
	}
	if d.Liveness.Basis != "accounting Start, no Stop" {
		t.Fatalf("liveness basis %q is not from the snapshot", d.Liveness.Basis)
	}
}

// The list path uses the same contract: rows, and each row's last_coa and
// override, from one snapshot.
func TestSessionSnapshot_ListQuarantineCommittedMidRead(t *testing.T) {
	r := newSnapshotRig(t)
	ctx := context.Background()
	s1 := r.activeSession(t, "acct-1")
	r.veos.mu.Lock()
	r.veos.silent = true
	r.veos.mu.Unlock()
	hook := once(t, "sessions", func() {
		if _, err := r.h.app.Pilot().RequestCoA(ctx, s1, "quarantine", adminEmail); err != nil {
			t.Fatal(err)
		}
		if coas, _ := r.repo.SessionCoAs(ctx, s1); len(coas) != 1 {
			t.Fatalf("control: %d CoAs", len(coas))
		}
	})
	rows, err := r.h.app.Pilot().ListSessions(postgres.WithSnapshotHook(ctx, hook), pilot.SessionQuery{State: domain.SessionActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != s1 {
		t.Fatalf("rows: %d", len(rows))
	}
	if rows[0].Override != "" || rows[0].LastCoA != nil {
		t.Fatalf("list row mixes the session before the Quarantine with the Quarantine: override=%q last_coa=%v", rows[0].Override, rows[0].LastCoA)
	}
	r.h.app.Pilot().Wait()
}

// Writer ordering: Authorize inserts the decision, then updates the session
// in a second statement. A snapshot taken between them holds a decision the
// session does not name yet; last_decision and the explanation must follow
// the session row (the state NAC is enforcing), not the newest decision row.
func TestSessionSnapshot_DecisionCommittedBeforeSessionUpdate(t *testing.T) {
	r := newSnapshotRig(t)
	ctx := context.Background()
	s1 := r.activeSession(t, "acct-1")
	sess, err := r.repo.GetSession(ctx, s1)
	if err != nil {
		t.Fatal(err)
	}
	cur, err := r.repo.GetDecision(ctx, *sess.LastDecisionID)
	if err != nil {
		t.Fatal(err)
	}
	// The committed intermediate state: Authorize's InsertDecision only.
	next := *cur
	next.ID, next.OccurredAt = uuid.New(), cur.OccurredAt.Add(1e9)
	vlan := 20
	next.Segment, next.VLAN, next.Reason = "quarantine", &vlan, "intermediate decision not yet applied to the session"
	if err := r.repo.InsertDecision(ctx, &next); err != nil {
		t.Fatal(err)
	}
	d, err := r.h.app.Pilot().SessionDetail(ctx, s1)
	if err != nil {
		t.Fatal(err)
	}
	coherent(t, d)
	if len(d.Decisions) != 2 || d.LastDecision == nil || d.LastDecision.ID != cur.ID || *d.Requested.VLAN != 10 {
		t.Fatalf("last_decision %v (want the session's %s), requested VLAN %v, %d decisions", d.LastDecision, cur.ID, deref(d.Requested.VLAN), len(d.Decisions))
	}
	for _, l := range d.Explanation {
		if strings.Contains(l, next.Reason) {
			t.Fatalf("explanation presents the unapplied decision as current: %s", l)
		}
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
