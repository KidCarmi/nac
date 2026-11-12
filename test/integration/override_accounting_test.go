//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/kidcarmi/nac/internal/radius"
)

// ASTRA pilot-rc2 (c121a95, vEOS-lab 4.29.2F, accounting enabled): after a
// Quarantine CoA the switch sent accounting Stop for the old session and then
// re-authenticated. NAC created a new session WITHOUT the operator override,
// so the endpoint got corp VLAN 10 and the CoA stayed "acked". The override
// must follow the same identity on the same port (switch + port + MAC +
// certificate issuer + serial) until an operator restores it, and the CoA
// must be correlated with the re-authentication that replaced the session.
func TestQuarantineSurvivesAccountingStopAndSessionReplacement(t *testing.T) {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	defer h.stop()
	veos := newFakeVEOS(t)

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

	acct := func(status, id string) {
		h.radiusCall("/api/v1/radius/accounting", "127.0.0.1", map[string]string{"Acct-Status-Type": status, "Acct-Session-Id": id,
			"Calling-Station-Id": "00-50-00-00-02-00", "NAS-Port-Id": "Ethernet1", "NAS-IP-Address": "127.0.0.1"}, 204)
	}
	auth := func(acctID string) string {
		a := authAttrs()
		a["Acct-Session-Id"] = acctID
		return frVal(h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", a, 200), "reply:Tunnel-Private-Group-Id")
	}
	if v := auth("acct-1"); v != "10" {
		t.Fatalf("baseline: VLAN %s", v)
	}
	acct("Start", "acct-1")
	var list struct{ Sessions []map[string]any }
	h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
	s1 := list.Sessions[0]["id"].(string)

	// ASTRA (pilot-aruba-rc3): with a stored accounting Start, the session
	// must not claim "no RADIUS accounting"; liveness stays unknown (no fresh
	// observation) and says why a Start does not prove presence.
	var sv map[string]any
	h.do("GET", "/api/v1/sessions/"+s1, nil, 200, &sv, true)
	lv := sv["liveness"].(map[string]any)
	if note := lv["note"].(string); lv["state"] != "unknown" || lv["basis"] != "accounting Start, no Stop" ||
		strings.Contains(note, "no RADIUS accounting") || !strings.Contains(note, "received RADIUS accounting Start for this session") ||
		!strings.Contains(note, "(Acct-Session-Id acct-1)") || !strings.Contains(note, "does not prove the endpoint is connected now") {
		t.Fatalf("liveness with a stored accounting Start: %+v", lv)
	}
	why := strings.Join(func() []string {
		var out []string
		for _, l := range sv["explanation"].([]any) {
			out = append(out, l.(string))
		}
		return out
	}(), "\n")
	if strings.Contains(why, "no RADIUS accounting") || !strings.Contains(why, "accounting Start") {
		t.Fatalf("Why panel contradicts the stored accounting Start:\n%s", why)
	}

	// The switch's behaviour on a CoA re-authentication with accounting on.
	reauthAs := func(oldID, newID string) func(*radius.Packet) {
		return func(*radius.Packet) {
			acct("Stop", oldID)
			auth(newID)
			acct("Start", newID)
		}
	}
	coaState := func(sessionID string) (string, map[string]any) {
		var d map[string]any
		h.do("GET", "/api/v1/sessions/"+sessionID, nil, 200, &d, true)
		lc, _ := d["last_coa"].(map[string]any)
		if lc == nil {
			return "", d
		}
		return lc["state"].(string), d
	}

	veos.mu.Lock()
	veos.onACK = reauthAs("acct-1", "acct-2")
	veos.mu.Unlock()
	h.do("POST", "/api/v1/sessions/"+s1+"/quarantine", nil, 202, nil, true)
	var s2 string
	waitFor(t, "replacement session", func() bool {
		h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
		for _, s := range list.Sessions {
			if s["id"] != s1 {
				s2 = s["id"].(string)
			}
		}
		return s2 != ""
	})
	h.app.Pilot().Wait()
	st, d := coaState(s2)
	req, _ := d["requested"].(map[string]any)
	if d["override"] != "quarantine" || req["vlan"] != float64(20) {
		t.Fatalf("replacement session lost the quarantine: override=%v requested=%v", d["override"], req)
	}
	if st != "observed" {
		t.Fatalf("Quarantine CoA not correlated with the replacement re-authentication: last_coa state %q", st)
	}
	// Arista regression (Aruba role path): a VLAN CoA carries the Tunnel
	// attributes and never Aruba-User-Role.
	veos.mu.Lock()
	last := veos.got[len(veos.got)-1]
	veos.mu.Unlock()
	if v := last.String(radius.AttrTunnelPrivateGroupID); !strings.HasSuffix(v, "20") {
		t.Fatalf("Arista quarantine CoA lost its VLAN: %q", v)
	}
	if _, ok := last.VSAString(radius.VendorAruba, radius.ArubaAttrUserRole); ok {
		t.Fatal("Arista CoA carries Aruba-User-Role")
	}

	// Unplug and replug (Stop, later a fresh authentication): still quarantined.
	acct("Stop", "acct-2")
	if v := auth("acct-3"); v != "20" {
		t.Fatalf("quarantine bypassed by reconnecting: VLAN %s", v)
	}
	acct("Start", "acct-3")

	// Codex (rc3): after the Stop, one deliberately FAILED EAP-TLS attempt
	// creates an identity-less session; the next valid login reuses it and
	// must still inherit the quarantine.
	acct("Stop", "acct-3")
	h.radiusCall("/api/v1/radius/reject", "127.0.0.1", map[string]string{"User-Name": "x", "Calling-Station-Id": "00-50-00-00-02-00",
		"NAS-Port-Id": "Ethernet1", "Module-Failure-Message": "eap_tls: (TLS) Alert write:fatal:unknown CA"}, 204)
	if v := auth("acct-3b"); v != "20" {
		t.Fatalf("quarantine bypassed by a failed attempt before the valid login: VLAN %s", v)
	}
	acct("Start", "acct-3b")
	h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
	s3 := list.Sessions[0]["id"].(string)

	// Restore clears it, also across the same replacement pattern.
	veos.mu.Lock()
	veos.onACK = reauthAs("acct-3b", "acct-4")
	veos.mu.Unlock()
	h.do("POST", "/api/v1/sessions/"+s3+"/restore", nil, 202, nil, true)
	var s4 string
	waitFor(t, "replacement after restore", func() bool {
		h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
		for _, s := range list.Sessions {
			if s["id"] != s3 {
				s4 = s["id"].(string)
			}
		}
		return s4 != ""
	})
	h.app.Pilot().Wait()
	st, d = coaState(s4)
	req, _ = d["requested"].(map[string]any)
	if d["override"] != nil && d["override"] != "" || req["vlan"] != float64(10) || st != "observed" {
		t.Fatalf("restore across replacement: override=%v vlan=%v coa=%q", d["override"], req["vlan"], st)
	}
	acct("Stop", "acct-4")
	if v := auth("acct-5"); v != "10" {
		t.Fatalf("restored identity reconnects into quarantine: VLAN %s", v)
	}
}
