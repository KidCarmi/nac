//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kidcarmi/nac/internal/app"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/radius"
)

const (
	adminEmail  = "admin@nac.local"
	adminPass   = "ChangeMe123!-pilot"
	svcToken    = "integration-service-token-0123456789"
	swSecret    = "lab-radius-secret-0123456789"
	caIssuer    = "/CN=NAC Lab Disposable CA"
	certSerial  = "2E1B47DC86A00B7F7250500B4A21F454B2EFD819"
	endpointMAC = "00:50:00:00:02:00"
)

// resetDB drops and recreates the public schema: a clean installation.
func resetDB(t *testing.T, dsn string) {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(context.Background(), `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
}

type harness struct {
	t      *testing.T
	app    *app.App
	srv    *httptest.Server
	token  string
	cancel context.CancelFunc
}

func start(t *testing.T, dir string) *harness {
	t.Helper()
	t.Setenv("NAC_DB_DSN", dsn(t))
	t.Setenv("NAC_BOOTSTRAP_ADMIN_EMAIL", adminEmail)
	t.Setenv("NAC_BOOTSTRAP_ADMIN_PASSWORD", adminPass)
	t.Setenv("NAC_SERVICE_TOKEN", svcToken)
	t.Setenv("NAC_SECRETS_KEY", "integration-secrets-key-0123456789abcdef")
	t.Setenv("NAC_FREERADIUS_CLIENTS_FILE", filepath.Join(dir, "clients.conf"))
	t.Setenv("NAC_FREERADIUS_OUTAGE_LOG", filepath.Join(dir, "nac-outage.log"))
	t.Setenv("NAC_COA_TIMEOUT", "300ms")
	t.Setenv("NAC_COA_RETRIES", "1")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		cancel()
		t.Fatalf("app: %v", err)
	}
	a.StartBackground(ctx)
	h := &harness{t: t, app: a, srv: httptest.NewServer(a.Handler()), cancel: cancel}
	var login struct{ Token string }
	h.do("POST", "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": adminPass}, 200, &login, false)
	h.token = login.Token
	return h
}

func (h *harness) stop() {
	h.app.Pilot().Wait()
	h.srv.Close()
	h.cancel()
	h.app.Close()
}

func (h *harness) do(method, path string, body any, want int, out any, operator bool) []byte {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	if operator {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		h.t.Fatalf("%s %s: status %d want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	if out != nil {
		// Decode into a fresh map: Unmarshal merges into an existing one,
		// which would keep keys that a later response omits.
		if m, ok := out.(*map[string]any); ok {
			*m = nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			h.t.Fatalf("decode %s: %v (%s)", path, err, raw)
		}
	}
	return raw
}

// radiusCall posts a FreeRADIUS rest-format body with Basic service-token auth.
func (h *harness) radiusCall(path, client string, attrs map[string]string, want int) map[string]any {
	h.t.Helper()
	body := map[string]any{}
	for k, v := range attrs {
		body[k] = map[string]any{"type": "string", "value": []string{v}}
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", h.srv.URL+path+"?client="+client, bytes.NewReader(b))
	req.SetBasicAuth("freeradius", svcToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		h.t.Fatalf("%s: status %d want %d: %s", path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func frVal(m map[string]any, k string) string {
	v, ok := m[k].(map[string]any)
	if !ok {
		return ""
	}
	vals, _ := v["value"].([]any)
	if len(vals) == 0 {
		return ""
	}
	return fmt.Sprint(vals[0])
}

// fakeVEOS answers CoA-Requests like vEOS 4.29.2F in ASTRA's lab: NAK 402
// unless NAS-IP-Address and NAS-Port-Id are present, else ACK 200; after an
// ACK it triggers the switch-initiated re-authentication.
type fakeVEOS struct {
	conn   net.PacketConn
	mu     sync.Mutex
	got    []*radius.Packet
	onACK  func(p *radius.Packet)
	silent bool
}

func newFakeVEOS(t *testing.T) *fakeVEOS {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeVEOS{conn: pc}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			raw := append([]byte{}, buf[:n]...)
			req, err := radius.Decode(raw)
			if err != nil || !radius.VerifyRequest(raw, []byte(swSecret)) {
				continue
			}
			f.mu.Lock()
			f.got = append(f.got, req)
			silent, onACK := f.silent, f.onACK
			f.mu.Unlock()
			if silent {
				continue
			}
			resp := &radius.Packet{Code: radius.CodeCoAACK}
			_, hasIP := req.Get(radius.AttrNASIPAddress)
			if !hasIP || req.String(radius.AttrNASPortID) == "" {
				resp.Code = radius.CodeCoANAK
				resp.AddUint32(radius.AttrErrorCause, 402)
			} else {
				resp.AddUint32(radius.AttrErrorCause, 200)
			}
			out, _ := resp.EncodeResponse([]byte(swSecret), req)
			_, _ = pc.WriteTo(out, addr)
			if resp.Code == radius.CodeCoAACK && onACK != nil {
				go onACK(req)
			}
		}
	}()
	t.Cleanup(func() { _ = pc.Close() })
	return f
}

func (f *fakeVEOS) port() int { return f.conn.LocalAddr().(*net.UDPAddr).Port }

func authAttrs() map[string]string {
	return map[string]string{
		"User-Name":                   "wired-endpoint-01",
		"Calling-Station-Id":          "00-50-00-00-02-00",
		"NAS-IP-Address":              "127.0.0.1",
		"NAS-Port-Id":                 "Ethernet1",
		"TLS-Client-Cert-Serial":      strings.ToLower(certSerial),
		"TLS-Client-Cert-Issuer":      caIssuer,
		"TLS-Client-Cert-Common-Name": "wired-endpoint-01",
		"EAP-Message":                 "0x02", // must never be stored
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPilotWorkflow_EndToEnd(t *testing.T) {
	resetDB(t, dsn(t))
	dir := t.TempDir()
	h := start(t, dir)
	veos := newFakeVEOS(t)

	// ---- Clean install: version 1 exists and is active. ----
	var versions struct{ Versions []map[string]any }
	h.do("GET", "/api/v1/policy-versions", nil, 200, &versions, true)
	if len(versions.Versions) != 1 || versions.Versions[0]["active"] != true {
		t.Fatalf("clean install should have one active version: %+v", versions)
	}

	// ---- Onboard the switch with configurable VLANs. ----
	var sw map[string]any
	h.do("POST", "/api/v1/switches", map[string]any{
		"name": "nac-veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret, "coa_port": veos.port(),
		"segment_vlans": map[string]int{"corp": 10, "quarantine": 20}, "access_ports": []string{"Ethernet1"},
		"radius_server_ip": "10.70.99.2",
	}, 201, &sw, true)
	swID := sw["id"].(string)
	status := sw["status"].(map[string]any)
	if status["auth_request_seen"].(map[string]any)["state"] != "no" || status["verified"].(map[string]any)["state"] != "no" {
		t.Fatalf("new switch status: %+v", status)
	}
	if strings.Contains(string(h.do("GET", "/api/v1/switches", nil, 200, nil, true)), swSecret) {
		t.Fatalf("switch secret leaked in API output")
	}
	cfgText := string(h.do("GET", "/api/v1/switches/"+swID+"/config", nil, 200, nil, true))
	for _, want := range []string{"dot1x dynamic-authorization", "radius-server host 10.70.99.2 key <RADIUS_SHARED_SECRET>",
		"vlan 20\n   name quarantine", "interface Ethernet1\n   switchport access vlan 10\n   dot1x pae authenticator\n   dot1x port-control auto"} {
		if !strings.Contains(cfgText, want) {
			t.Fatalf("EOS config missing %q:\n%s", want, cfgText)
		}
	}
	clients, err := os.ReadFile(filepath.Join(dir, "clients.conf"))
	if err != nil || !strings.Contains(string(clients), "ipaddr = 127.0.0.1") || !strings.Contains(string(clients), swSecret) {
		t.Fatalf("FreeRADIUS clients file not rendered: %v\n%s", err, clients)
	}

	// ---- Policy: bound identities to corp; publish via preview digest. ----
	h.do("POST", "/api/v1/policies", map[string]any{"name": "corp-bound-devices", "priority": 10, "enabled": true,
		"action": "allow", "segment": "corp", "conditions": []map[string]any{
			{"field": "identity.bound", "operator": "eq", "values": []string{"true"}},
			{"field": "mac.matches_device", "operator": "eq", "values": []string{"true"}}}}, 201, nil, true)
	var preview map[string]any
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &preview, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": preview["draft_digest"], "note": "pilot baseline"}, 201, nil, true)

	// ---- Unknown switch source: rejected with an actionable reason. ----
	r := h.radiusCall("/api/v1/radius/authorize", "192.0.2.99", authAttrs(), 200)
	if frVal(r, "control:Tmp-String-1") != "nac-reject" {
		t.Fatalf("unregistered switch must be rejected: %v", r)
	}

	// ---- Valid certificate, not yet bound: default deny, explained. ----
	r = h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", authAttrs(), 200)
	if frVal(r, "control:Tmp-String-1") != "nac-reject" {
		t.Fatalf("unbound identity should be denied: %v", r)
	}
	var sessions struct{ Sessions []map[string]any }
	h.do("GET", "/api/v1/sessions", nil, 200, &sessions, true)
	if len(sessions.Sessions) != 1 || sessions.Sessions[0]["state"] != "rejected" {
		t.Fatalf("expected one rejected session: %+v", sessions)
	}
	sessID := sessions.Sessions[0]["id"].(string)

	// ---- Operator creates the device and binds the observed identity. ----
	var ep map[string]any
	h.do("POST", "/api/v1/endpoints", map[string]any{"mac": endpointMAC, "hostname": "wired-endpoint-01", "device_type": "laptop"}, 201, &ep, true)
	h.do("POST", "/api/v1/sessions/"+sessID+"/bind", map[string]any{"endpoint_id": ep["id"]}, 201, nil, true)

	r = h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", authAttrs(), 200)
	if frVal(r, "control:Tmp-String-1") != "nac-accept" || frVal(r, "reply:Tunnel-Private-Group-Id") != "10" ||
		!strings.HasPrefix(frVal(r, "reply:Class"), "nac:") {
		t.Fatalf("bound identity should get corp VLAN 10: %v", r)
	}
	if v, _ := r["reply:Tunnel-Private-Group-Id"].(map[string]any); v["do_xlat"] != false {
		t.Fatalf("reply values must disable xlat expansion: %v", v)
	}

	var detail map[string]any
	h.do("GET", "/api/v1/sessions/"+sessID, nil, 200, &detail, true)
	if detail["state"] != "active" || detail["requested"].(map[string]any)["vlan"].(float64) != 10 {
		t.Fatalf("session should be active on VLAN 10: %+v", detail)
	}
	if detail["observed"].(map[string]any)["freshness"] != "unavailable" {
		t.Fatalf("without eAPI, observation must be explicitly unavailable: %+v", detail["observed"])
	}
	last := detail["last_decision"].(map[string]any)
	inputs := last["inputs"].(map[string]any)
	if inputs["identity.serial"].(map[string]any)["source"] != "eap-tls" ||
		inputs["posture"].(map[string]any)["status"] != "missing" ||
		last["matched_rule"] != "corp-bound-devices" || last["policy_version"].(float64) != 2 {
		t.Fatalf("decision record incomplete: %+v", last)
	}
	if strings.Contains(string(h.do("GET", "/api/v1/sessions/"+sessID, nil, 200, nil, true)), "EAP-Message") {
		t.Fatalf("EAP payload must not be stored")
	}

	// ---- Verify the switch (decision-only, no eAPI). ----
	var verified map[string]any
	h.do("POST", "/api/v1/switches/"+swID+"/verify", nil, 200, &verified, true)
	if verified["status"].(map[string]any)["verified"].(map[string]any)["state"] != "yes" {
		t.Fatalf("verify failed: %+v", verified["status"])
	}

	// ---- Rejected certificate: FreeRADIUS reject path is diagnosable. ----
	bad := map[string]string{"User-Name": "rogue", "Calling-Station-Id": "00-50-00-00-09-09", "NAS-Port-Id": "Ethernet1",
		"Module-Failure-Message": "eap_tls: (TLS) TLS - Alert read:fatal:unknown CA"}
	h.radiusCall("/api/v1/radius/reject", "127.0.0.1", bad, 204)
	var evs struct{ Events []map[string]any }
	h.do("GET", "/api/v1/auth-events?stage=eap-tls", nil, 200, &evs, true)
	if len(evs.Events) != 1 || !strings.Contains(evs.Events[0]["reason"].(string), "not issued by the CA FreeRADIUS trusts") {
		t.Fatalf("certificate rejection not explained: %+v", evs)
	}

	// ---- Draft quarantine-everything change: preview predicts it. ----
	var draft map[string]any
	h.do("GET", "/api/v1/policy-draft", nil, 200, &draft, true)
	ruleID := draft["rules"].([]any)[0].(map[string]any)["id"].(string)
	h.do("PUT", "/api/v1/policies/"+ruleID, map[string]any{"name": "corp-bound-devices", "priority": 10, "enabled": true,
		"action": "quarantine", "segment": "quarantine", "conditions": []map[string]any{
			{"field": "identity.bound", "operator": "eq", "values": []string{"true"}}}}, 200, nil, true)
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &preview, true)
	if preview["changed"].(float64) != 1 {
		t.Fatalf("preview should predict one changed decision: %+v", preview)
	}
	if !strings.HasPrefix(preview["label"].(string), "Prediction based on recorded") {
		t.Fatalf("preview must be labelled a prediction")
	}
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": preview["draft_digest"]}, 201, nil, true)
	// Publication applies at the next authentication: the session is untouched.
	h.do("GET", "/api/v1/sessions/"+sessID, nil, 200, &detail, true)
	if detail["requested"].(map[string]any)["vlan"].(float64) != 10 {
		t.Fatalf("publish must not change existing sessions by itself")
	}
	// Restore v2 (corp) again.
	h.do("POST", "/api/v1/policy-versions/2/restore", map[string]any{"note": "back to baseline"}, 200, nil, true)

	// ---- Quarantine the active session through CoA. ----
	veos.mu.Lock()
	veos.onACK = func(req *radius.Packet) {
		// vEOS re-authenticates the session on its own after the ACK.
		h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", authAttrs(), 200)
	}
	veos.mu.Unlock()
	var coa struct{ Coa map[string]any }
	h.do("POST", "/api/v1/sessions/"+sessID+"/quarantine", nil, 202, &coa, true)
	coaID := coa.Coa["id"].(string)
	waitFor(t, "CoA observed", func() bool {
		h.do("GET", "/api/v1/sessions/"+sessID, nil, 200, &detail, true)
		lc, _ := detail["last_coa"].(map[string]any)
		return lc != nil && lc["id"] == coaID && lc["state"] == "observed"
	})
	if detail["requested"].(map[string]any)["vlan"].(float64) != 20 || detail["override"] != "quarantine" {
		t.Fatalf("quarantine not applied: %+v", detail["requested"])
	}
	veos.mu.Lock()
	sent := veos.got[len(veos.got)-1]
	veos.mu.Unlock()
	if sent.Code != radius.CodeCoARequest || sent.String(radius.AttrNASPortID) != "Ethernet1" ||
		sent.String(radius.AttrCallingStationID) != "00-50-00-00-02-00" || sent.String(radius.AttrTunnelPrivateGroupID) != "20" {
		t.Fatalf("CoA attributes differ from the lab-validated request: %+v", sent.Attributes)
	}
	h.app.Pilot().Wait()

	// ---- A switch that never answers: timeout, never re-targeted. ----
	veos.mu.Lock()
	veos.silent = true
	before := len(veos.got)
	veos.mu.Unlock()
	h.do("POST", "/api/v1/sessions/"+sessID+"/reauthenticate", nil, 202, &coa, true)
	h.app.Pilot().Wait()
	h.do("GET", "/api/v1/sessions/"+sessID, nil, 200, &detail, true)
	if lc := detail["last_coa"].(map[string]any); lc["state"] != "timeout" || lc["attempts"].(float64) != 2 {
		t.Fatalf("expected timeout after 2 attempts: %+v", lc)
	}
	veos.mu.Lock()
	retries := veos.got[before:]
	veos.silent = false
	veos.mu.Unlock()
	if len(retries) != 2 || retries[0].Identifier != retries[1].Identifier || retries[0].Authenticator != retries[1].Authenticator {
		t.Fatalf("retry must resend the identical request")
	}

	// ---- Restore the session; access returns to corp. ----
	h.do("POST", "/api/v1/sessions/"+sessID+"/restore", nil, 202, &coa, true)
	waitFor(t, "restore observed", func() bool {
		h.do("GET", "/api/v1/sessions/"+sessID, nil, 200, &detail, true)
		lc, _ := detail["last_coa"].(map[string]any)
		return lc != nil && lc["kind"] == "restore" && lc["state"] == "observed"
	})
	if ov, _ := detail["override"].(string); detail["requested"].(map[string]any)["vlan"].(float64) != 10 || ov != "" {
		t.Fatalf("restore did not return corp: requested=%+v override=%q", detail["requested"], ov)
	}
	h.app.Pilot().Wait()

	// ---- Outage rejects written by FreeRADIUS are imported once. ----
	line := `{"event":"outage-reject","unix_time":1800000000,"attrs":{"control:Tmp-String-3":"127.0.0.1","NAS-Port-Id":"Ethernet1","Calling-Station-Id":"00-50-00-00-02-00","control:Tmp-String-2":"connection refused"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "nac-outage.log"), []byte(line+line), 0o600); err != nil {
		t.Fatal(err)
	}
	h.stop()

	// ---- Restart on the same database: everything survives. ----
	h2 := start(t, dir)
	defer h2.stop()
	waitFor(t, "outage import", func() bool {
		h2.do("GET", "/api/v1/auth-events?stage=outage", nil, 200, &evs, true)
		return len(evs.Events) == 1
	})
	if evs.Events[0]["source"] != "freeradius-outage-log" {
		t.Fatalf("outage provenance wrong: %+v", evs.Events[0])
	}
	h2.do("GET", "/api/v1/sessions/"+sessID, nil, 200, &detail, true)
	if detail["state"] != "active" || len(detail["timeline"].([]any)) < 8 || len(detail["coa_requests"].([]any)) != 3 {
		t.Fatalf("session history did not survive restart: state=%v timeline=%d coas=%d",
			detail["state"], len(detail["timeline"].([]any)), len(detail["coa_requests"].([]any)))
	}
	var acts struct{ Activations []map[string]any }
	h2.do("GET", "/api/v1/policy-activations", nil, 200, &acts, true)
	if len(acts.Activations) != 4 || acts.Activations[0]["kind"] != "restore" {
		t.Fatalf("activation history wrong after restart: %+v", acts.Activations)
	}
	r = h2.radiusCall("/api/v1/radius/authorize", "127.0.0.1", authAttrs(), 200)
	if frVal(r, "reply:Tunnel-Private-Group-Id") != "10" {
		t.Fatalf("after restart the endpoint should still get corp: %v", r)
	}
}

// Regressions for review findings on PR #1.
func TestPilotRegressions_ReviewFindings(t *testing.T) {
	resetDB(t, dsn(t))
	dir := t.TempDir()
	h := start(t, dir)
	defer h.stop()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	var sw map[string]any
	h.do("POST", "/api/v1/switches", map[string]any{"name": "sw", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"coa_port": 9, "segment_vlans": map[string]int{"corp": 10, "quarantine": 20}}, 201, &sw, true)
	var ep map[string]any
	h.do("POST", "/api/v1/endpoints", map[string]any{"mac": endpointMAC, "hostname": "h1", "posture": "compliant"}, 201, &ep, true)
	h.do("POST", "/api/v1/identity-bindings", map[string]any{"endpoint_id": ep["id"], "issuer": caIssuer, "serial": certSerial}, 201, nil, true)
	h.do("POST", "/api/v1/policies", map[string]any{"name": "compliant-corp", "priority": 10, "enabled": true, "action": "allow", "segment": "corp",
		"conditions": []map[string]any{{"field": "identity.bound", "operator": "eq", "values": []string{"true"}},
			{"field": "posture", "operator": "eq", "values": []string{"compliant"}}}}, 201, nil, true)
	var pv map[string]any
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &pv, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": pv["draft_digest"]}, 201, nil, true)

	attrs := authAttrs()
	r := h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", attrs, 200)
	if frVal(r, "reply:Tunnel-Private-Group-Id") != "10" {
		t.Fatalf("fresh compliant posture should give corp: %v", r)
	}
	firstDecision := strings.TrimPrefix(frVal(r, "reply:Class"), "nac:")
	var list struct{ Sessions []map[string]any }
	h.do("GET", "/api/v1/sessions", nil, 200, &list, true)
	sessID := list.Sessions[0]["id"].(string)

	// --- Finding 3: an inventory edit must not renew old posture. ---
	if _, err := conn.Exec(ctx, `UPDATE endpoints SET posture_observed_at = now() - interval '3 days'`); err != nil {
		t.Fatal(err)
	}
	h.do("PUT", "/api/v1/endpoints/"+ep["id"].(string), map[string]any{"hostname": "h1-renamed"}, 200, nil, true)
	r = h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", attrs, 200)
	if frVal(r, "control:Tmp-String-1") != "nac-reject" {
		t.Fatalf("3-day-old posture must be stale after a hostname edit (default deny), got %v", r)
	}
	var detail map[string]any
	h.do("GET", "/api/v1/sessions/"+sessID, nil, 200, &detail, true)
	if st := detail["last_decision"].(map[string]any)["inputs"].(map[string]any)["posture"].(map[string]any)["status"]; st != "stale" {
		t.Fatalf("posture evidence should be stale, got %v", st)
	}
	// Re-reporting posture makes it fresh again.
	h.do("PUT", "/api/v1/endpoints/"+ep["id"].(string), map[string]any{"hostname": "h1-renamed", "posture": "compliant"}, 200, nil, true)
	r = h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", attrs, 200)
	if frVal(r, "reply:Tunnel-Private-Group-Id") != "10" {
		t.Fatalf("re-reported posture should be fresh: %v", r)
	}

	// --- Finding 1: a stale full-row session write cannot clear an override. ---
	h.do("POST", "/api/v1/sessions/"+sessID+"/quarantine", nil, 202, nil, true)
	h.app.Pilot().Wait() // CoA to an unreachable port: times out, override stays
	var override string
	if err := conn.QueryRow(ctx, `SELECT override FROM radius_sessions WHERE id=$1`, sessID).Scan(&override); err != nil || override != "quarantine" {
		t.Fatalf("override = %q, %v", override, err)
	}
	// Simulate the observation poller and the RADIUS path writing back
	// snapshots taken before the quarantine.
	if _, err := conn.Exec(ctx, `UPDATE radius_sessions SET observed_status='SUCCESS', observed_vlan='10', observed_at=now() WHERE id=$1`, sessID); err != nil {
		t.Fatal(err)
	}
	r = h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", attrs, 200)
	if frVal(r, "reply:Tunnel-Private-Group-Id") != "20" {
		t.Fatalf("override must survive authentication writes: %v", r)
	}
	if err := conn.QueryRow(ctx, `SELECT override FROM radius_sessions WHERE id=$1`, sessID).Scan(&override); err != nil || override != "quarantine" {
		t.Fatalf("authorize write cleared the override: %q, %v", override, err)
	}

	// --- Finding 2: stale accounting must not end the current session. ---
	acct := func(status, acctID, class string) {
		a := map[string]string{"Acct-Status-Type": status, "Acct-Session-Id": acctID, "Calling-Station-Id": "00-50-00-00-02-00", "NAS-Port-Id": "Ethernet1"}
		if class != "" {
			a["Class"] = class
		}
		h.radiusCall("/api/v1/radius/accounting", "127.0.0.1", a, 204)
	}
	state := func() string {
		var st string
		if err := conn.QueryRow(ctx, `SELECT state FROM radius_sessions WHERE id=$1`, sessID).Scan(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	acct("Start", "acct-2", "")
	acct("Stop", "acct-1", "")                         // earlier connection's Stop
	acct("Stop", "acct-2", "nac:"+uuid.New().String()) // Class of a decision that is not this session's
	if state() != "active" {
		t.Fatalf("a stale Stop ended the current session")
	}
	if _, err := conn.Exec(ctx, `UPDATE auth_decisions SET session_id = NULL WHERE id=$1`, firstDecision); err != nil {
		t.Fatal(err)
	}
	acct("Stop", "acct-2", "nac:"+firstDecision) // decision now belongs to no session
	if state() != "active" {
		t.Fatalf("a Stop whose Class names another session's decision ended the current session")
	}
	acct("Stop", "acct-2", "")
	if state() != "ended" {
		t.Fatalf("the matching Stop should end the session")
	}

	// --- Re-review finding: same serial from a DIFFERENT issuer is a
	// different certificate and must not inherit the session (override). ---
	h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", attrs, 200) // fresh session for cert A
	h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
	sessA := list.Sessions[0]["id"].(string)
	h.do("POST", "/api/v1/sessions/"+sessA+"/quarantine", nil, 202, nil, true)
	h.app.Pilot().Wait()
	other := authAttrs()
	other["TLS-Client-Cert-Issuer"] = "/CN=Another Trusted CA"
	h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", other, 200)
	var endedA string
	if err := conn.QueryRow(ctx, `SELECT state FROM radius_sessions WHERE id=$1`, sessA).Scan(&endedA); err != nil || endedA != "ended" {
		t.Fatalf("same serial from another issuer must end session A, got %q %v", endedA, err)
	}
	var newOverride, newIssuer string
	if err := conn.QueryRow(ctx, `SELECT override, cert_issuer FROM radius_sessions WHERE state <> 'ended'`).Scan(&newOverride, &newIssuer); err != nil {
		t.Fatal(err)
	}
	if newOverride != "" || newIssuer != "/CN=Another Trusted CA" {
		t.Fatalf("new session inherited state: override=%q issuer=%q", newOverride, newIssuer)
	}

	// --- Third review: a TLS failure without certificate attributes must not
	// erase the session's identity, so a different certificate afterwards is
	// still recognised as a replacement. ---
	h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", attrs, 200) // bound cert A again: an ACTIVE session
	var sessB string
	if err := conn.QueryRow(ctx, `SELECT id FROM radius_sessions WHERE state = 'active'`).Scan(&sessB); err != nil {
		t.Fatal(err)
	}
	h.do("POST", "/api/v1/sessions/"+sessB+"/quarantine", nil, 202, nil, true) // needs an active session
	h.app.Pilot().Wait()
	h.radiusCall("/api/v1/radius/reject", "127.0.0.1", map[string]string{"User-Name": "x", "Calling-Station-Id": "00-50-00-00-02-00",
		"NAS-Port-Id": "Ethernet1", "Module-Failure-Message": "eap_tls: (TLS) Alert read:fatal:unknown CA"}, 204)
	var keptSerial string
	if err := conn.QueryRow(ctx, `SELECT cert_serial FROM radius_sessions WHERE id=$1`, sessB).Scan(&keptSerial); err != nil || keptSerial == "" {
		t.Fatalf("TLS failure erased the session's certificate identity: %q %v", keptSerial, err)
	}
	third := authAttrs()
	third["TLS-Client-Cert-Serial"] = "ABCDEF0123"
	h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", third, 200)
	var stateB string
	if err := conn.QueryRow(ctx, `SELECT state FROM radius_sessions WHERE id=$1`, sessB).Scan(&stateB); err != nil || stateB != "ended" {
		t.Fatalf("a different certificate after a TLS failure must end the old session, got %q %v", stateB, err)
	}
	if err := conn.QueryRow(ctx, `SELECT override FROM radius_sessions WHERE state <> 'ended'`).Scan(&newOverride); err != nil || newOverride != "" {
		t.Fatalf("the new certificate inherited the old session's override: %q %v", newOverride, err)
	}
}

// Third review: concurrent switch registrations must all end up in the
// FreeRADIUS client file (no stale snapshot may overwrite a newer one).
func TestPilotRegressions_ConcurrentSwitchChangesKeepClientsFileComplete(t *testing.T) {
	resetDB(t, dsn(t))
	dir := t.TempDir()
	h := start(t, dir)
	defer h.stop()
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.do("POST", "/api/v1/switches", map[string]any{"name": fmt.Sprintf("sw-%02d", i),
				"radius_source_ip": fmt.Sprintf("10.20.0.%d", i+1), "radius_secret": swSecret,
				"segment_vlans": map[string]int{"corp": 10, "quarantine": 20}}, 201, nil, true)
		}(i)
	}
	wg.Wait()
	b, err := os.ReadFile(filepath.Join(dir, "clients.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if !strings.Contains(string(b), fmt.Sprintf("ipaddr = 10.20.0.%d\n", i+1)) {
			t.Fatalf("switch sw-%02d missing from the FreeRADIUS client file:\n%s", i, b)
		}
	}
}
