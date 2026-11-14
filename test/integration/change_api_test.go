//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// Change requests through the real API (slice A, milestone 2): one shared
// service behind the handlers, revision-bound mutations, recorded
// validation, drift reporting, transactional audit and no side effects.

type apiResp struct {
	code int
	etag string
	body map[string]any
	raw  string
}

func (h *harness) call(method, path, ifMatch string, body any) apiResp {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+h.token)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := apiResp{code: resp.StatusCode, etag: resp.Header.Get("ETag"), raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func testPool(t *testing.T) *pgxpool.Pool {
	pool, err := postgres.Connect(context.Background(), config.DBConfig{DSN: dsn(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func auditActions(t *testing.T, pool *pgxpool.Pool, id string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT action FROM audit_events WHERE resource_type='change_request' AND resource_id=$1
		ORDER BY occurred_at, action`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		out = append(out, a)
	}
	return out
}

var corpRule = map[string]any{"name": "bound-corp", "priority": 10, "enabled": true, "action": "allow", "segment": "corp",
	"conditions": []map[string]any{{"field": "identity.bound", "operator": "eq", "values": []string{"true"}}}}

func TestChangeAPI_Lifecycle(t *testing.T) {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	defer h.stop()
	pool := testPool(t)
	h.do("POST", "/api/v1/switches", map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"segment_vlans": map[string]int{"corp": 10, "quarantine": 20}, "access_ports": []string{"Ethernet1"}}, 201, nil, true)

	r := h.call("POST", "/api/v1/change-requests", "", map[string]any{"title": "bound devices to corp", "intent": "pilot",
		"content": map[string]any{"rules": []any{corpRule}, "default_action": "deny"}})
	if r.code != 201 || r.etag != `"1"` || r.body["workflow"] != "review_only" || r.body["state"] != "draft" ||
		!strings.Contains(r.body["label"].(string), "Review-only") {
		t.Fatalf("create: %d %s", r.code, r.raw)
	}
	id := r.body["id"].(string)
	base := "/api/v1/change-requests/" + id

	edit := map[string]any{"rules": []any{corpRule, map[string]any{"name": "guests", "priority": 20, "enabled": true, "action": "quarantine",
		"segment": "quarantine", "conditions": []any{}}}, "default_action": "deny"}
	if r = h.call("PUT", base+"/content", "", edit); r.code != 428 {
		t.Fatalf("edit without If-Match: %d", r.code)
	}
	if r = h.call("PUT", base+"/content", `"1"`, edit); r.code != 200 || r.etag != `"2"` || r.body["revision"] != float64(2) {
		t.Fatalf("edit: %d %s", r.code, r.raw)
	}
	if r = h.call("PUT", base+"/content", `"1"`, edit); r.code != 409 || !strings.Contains(r.raw, "revision 2") {
		t.Fatalf("stale edit: %d %s", r.code, r.raw)
	}
	if r = h.call("POST", base+"/validations", `"1"`, nil); r.code != 409 {
		t.Fatalf("validating a non-current revision: %d %s", r.code, r.raw)
	}
	r = h.call("POST", base+"/validations", `"2"`, nil)
	if r.code != 201 || r.body["validation"].(map[string]any)["valid"] != true {
		t.Fatalf("validate: %d %s", r.code, r.raw)
	}
	if cr := r.body["change_request"].(map[string]any); cr["state"] != "validated" {
		t.Fatalf("state after a valid validation: %v", cr["state"])
	}
	if r = h.call("GET", base+"/revisions", "", nil); r.code != 200 || len(r.body["revisions"].([]any)) != 2 {
		t.Fatalf("revisions: %d %s", r.code, r.raw)
	}
	if r = h.call("POST", base+"/cancel", `"2"`, nil); r.code != 200 || r.body["state"] != "cancelled" {
		t.Fatalf("cancel: %d %s", r.code, r.raw)
	}
	if r = h.call("PUT", base+"/content", `"2"`, edit); r.code != 409 {
		t.Fatalf("edit after cancel: %d", r.code)
	}
	got := strings.Join(auditActions(t, pool, id), ",")
	if got != "change.create,change.edit,change.validate,change.cancel" {
		t.Fatalf("audit records: %s (refused requests must not be audited as mutations)", got)
	}
}

// Drift is reported, never silently "fixed": a legacy publish moves the active
// version (the base drifts and the validation goes stale), a switch mapping
// change makes the validation stale, and an edit rebases onto the active
// version.
func TestChangeAPI_FromDraftAndDrift(t *testing.T) {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	defer h.stop()
	var sw map[string]any
	h.do("POST", "/api/v1/switches", map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"segment_vlans": map[string]int{"corp": 10, "quarantine": 20}, "access_ports": []string{"Ethernet1"}}, 201, &sw, true)
	h.do("POST", "/api/v1/policies", corpRule, 201, nil, true)

	r := h.call("POST", "/api/v1/change-requests", "", map[string]any{"title": "from draft", "from_draft": true})
	if r.code != 201 {
		t.Fatalf("create from draft: %d %s", r.code, r.raw)
	}
	id, baseVersion := r.body["id"].(string), r.body["base_version_number"]
	rules := r.body["content"].(map[string]any)["rules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["name"] != "bound-corp" {
		t.Fatalf("content is not the draft: %s", r.raw)
	}
	base := "/api/v1/change-requests/" + id
	if r = h.call("POST", base+"/validations", `"1"`, nil); r.code != 201 {
		t.Fatalf("validate: %d %s", r.code, r.raw)
	}

	// The switch mappings change: the validation describes an old environment.
	h.do("PUT", "/api/v1/switches/"+sw["id"].(string), map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1",
		"segment_vlans": map[string]int{"corp": 11, "quarantine": 20}, "access_ports": []string{"Ethernet1"}}, 200, nil, true)
	r = h.call("GET", base, "", nil)
	v := r.body["validations"].([]any)[0].(map[string]any)["drift"].(map[string]any)
	if v["state"] != "stale" || !strings.Contains(strings.Join(toStrings(v["reasons"]), ";"), "segment mappings changed") {
		t.Fatalf("validation after a mapping change: %v", v)
	}

	// A legacy publish moves the active version.
	var pv map[string]any
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &pv, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": pv["draft_digest"]}, 201, nil, true)
	r = h.call("GET", base, "", nil)
	bd := r.body["base_drift"].(map[string]any)
	if bd["state"] != "stale" || r.body["base_version_number"] != baseVersion {
		t.Fatalf("base after a legacy publish: %v (base %v)", bd, r.body["base_version_number"])
	}
	// An edit rebases onto the active version and returns to draft.
	r = h.call("PUT", base+"/content", `"1"`, r.body["content"])
	if r.code != 200 || r.body["state"] != "draft" || r.body["base_version_number"] == baseVersion ||
		r.body["base_drift"].(map[string]any)["state"] != "current" {
		t.Fatalf("edit after drift: %d %s", r.code, r.raw)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// No change-request route changes the active policy, the draft, a session,
// a CoA or a switch.
func TestChangeAPI_NoSideEffects(t *testing.T) {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	defer h.stop()
	pool := testPool(t)
	veos := newFakeVEOS(t)
	h.do("POST", "/api/v1/switches", map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"coa_port": veos.port(), "segment_vlans": map[string]int{"corp": 10, "quarantine": 20}, "access_ports": []string{"Ethernet1"}}, 201, nil, true)
	var ep map[string]any
	h.do("POST", "/api/v1/endpoints", map[string]any{"mac": endpointMAC, "hostname": "h1"}, 201, &ep, true)
	h.do("POST", "/api/v1/identity-bindings", map[string]any{"endpoint_id": ep["id"], "issuer": caIssuer, "serial": certSerial}, 201, nil, true)
	h.do("POST", "/api/v1/policies", corpRule, 201, nil, true)
	var pv map[string]any
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &pv, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": pv["draft_digest"]}, 201, nil, true)
	a := authAttrs()
	h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", a, 200)

	snapshot := func() string {
		var b strings.Builder
		for _, q := range []string{
			`SELECT coalesce(json_agg(t ORDER BY t.id), '[]') FROM policy_activations t`,
			`SELECT coalesce(json_agg(t ORDER BY t.number), '[]') FROM policy_versions t`,
			`SELECT coalesce(json_agg(t ORDER BY t.id), '[]') FROM policies t`,
			`SELECT coalesce(json_agg(t), '[]') FROM policy_draft_settings t`,
			`SELECT coalesce(json_agg(t ORDER BY t.id), '[]') FROM radius_sessions t`,
			`SELECT coalesce(json_agg(t ORDER BY t.id), '[]') FROM coa_requests t`,
			`SELECT coalesce(json_agg(t ORDER BY t.id), '[]') FROM switches t`,
			`SELECT coalesce(json_agg(t ORDER BY t.id), '[]') FROM auth_decisions t`,
		} {
			var s string
			if err := pool.QueryRow(context.Background(), q).Scan(&s); err != nil {
				t.Fatal(err)
			}
			b.WriteString(s + "\n")
		}
		return b.String()
	}
	before := snapshot()

	quarantineAll := map[string]any{"rules": []any{map[string]any{"name": "everyone", "priority": 1, "enabled": true, "action": "quarantine",
		"segment": "quarantine", "conditions": []any{}}}, "default_action": "quarantine"}
	r := h.call("POST", "/api/v1/change-requests", "", map[string]any{"title": "quarantine everyone", "content": quarantineAll})
	id := r.body["id"].(string)
	base := "/api/v1/change-requests/" + id
	h.call("POST", "/api/v1/change-requests", "", map[string]any{"title": "copy", "from_draft": true})
	h.call("GET", "/api/v1/change-requests", "", nil)
	h.call("GET", base, "", nil)
	h.call("GET", base+"/revisions", "", nil)
	h.call("GET", base+"/revisions/1", "", nil)
	h.call("POST", base+"/validations", `"1"`, nil)
	h.call("PUT", base+"/content", `"1"`, quarantineAll)
	h.call("PUT", base+"/content", `"1"`, quarantineAll) // refused (stale)
	h.call("POST", base+"/validations", `"2"`, nil)
	h.call("POST", base+"/cancel", `"2"`, nil)

	if after := snapshot(); after != before {
		t.Fatalf("a change-request route changed policy, sessions, CoAs, switches or decisions:\nbefore %s\nafter  %s", before, after)
	}
	veos.mu.Lock()
	sent := len(veos.got)
	veos.mu.Unlock()
	if sent != 0 {
		t.Fatalf("%d CoA packet(s) sent by change-request routes", sent)
	}
	// The next authentication still gets the ACTIVE policy (corp VLAN 10).
	if v := frVal(h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", a, 200), "reply:Tunnel-Private-Group-Id"); v != "10" {
		t.Fatalf("authorization after change-request activity: VLAN %q", v)
	}
}
