//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/kidcarmi/nac/internal/radius"
)

const arubaPort = "1/1/1"

// The bounded Aruba AOS-CX role path: the decision selects a configured local
// role, returned as Aruba-User-Role; Quarantine and Restore are session-
// targeted role CoAs that carry no Tunnel-* attributes, so the VLAN and the
// endpoint's IP never change. The identity-scoped override must persist
// exactly as on the VLAN path, and an ACK must never be reported as traffic
// verification.
func TestArubaRolePath_QuarantineRestoreAndPersistence(t *testing.T) {
	resetDB(t, dsn(t))
	dir := t.TempDir()
	h := start(t, dir)
	sw := newFakeVEOS(t) // same RFC 5176 behaviour: ACK when NAS-IP-Address and NAS-Port-Id are present

	var swRec map[string]any
	h.do("POST", "/api/v1/switches", map[string]any{"name": "aos-cx", "platform": "aruba-aos-cx", "radius_source_ip": "127.0.0.1",
		"radius_secret": swSecret, "coa_port": sw.port(), "access_vlan": 10,
		"segment_roles": map[string]string{"corp": "nac-corp", "quarantine": "nac-quarantine"},
		"access_ports":  []string{arubaPort}}, 201, &swRec, true)
	if swRec["enforcement"] != "role" {
		t.Fatalf("Aruba switch must default to role enforcement: %v", swRec["enforcement"])
	}
	cfg := string(h.do("GET", "/api/v1/switches/"+swRec["id"].(string)+"/config", nil, 200, nil, true))
	for _, want := range []string{"port-access role nac-corp", "port-access role nac-quarantine", "radius dyn-authorization enable", "dot1x authenticator"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("AOS-CX config lacks %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "vlan 20") || strings.Contains(cfg, "Tunnel") {
		t.Fatalf("AOS-CX role config must not move VLANs:\n%s", cfg)
	}

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
			"Calling-Station-Id": "00-50-00-00-02-00", "NAS-Port-Id": arubaPort, "NAS-IP-Address": "127.0.0.1"}, 204)
	}
	auth := func(acctID string) map[string]any {
		a := authAttrs()
		a["NAS-Port-Id"] = arubaPort
		a["Acct-Session-Id"] = acctID
		return h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", a, 200)
	}
	role := func(r map[string]any) string {
		if v := frVal(r, "reply:Tunnel-Private-Group-Id"); v != "" {
			t.Fatalf("role switch reply carries a VLAN (%s); the VLAN must not change", v)
		}
		return frVal(r, "reply:Aruba-User-Role")
	}
	session := func() (string, map[string]any) {
		var list struct{ Sessions []map[string]any }
		h.do("GET", "/api/v1/sessions?state=active", nil, 200, &list, true)
		if len(list.Sessions) != 1 {
			t.Fatalf("want one active session, got %d", len(list.Sessions))
		}
		id := list.Sessions[0]["id"].(string)
		var d map[string]any
		h.do("GET", "/api/v1/sessions/"+id, nil, 200, &d, true)
		return id, d
	}
	lastCoAPacket := func() *radius.Packet {
		sw.mu.Lock()
		defer sw.mu.Unlock()
		if len(sw.got) == 0 {
			t.Fatal("no CoA reached the switch")
		}
		return sw.got[len(sw.got)-1]
	}

	// 1. corp
	if r := role(auth("acct-1")); r != "nac-corp" {
		t.Fatalf("baseline role %q", r)
	}
	acct("Start", "acct-1")
	s1, d := session()
	enf, _ := d["enforcement"].(map[string]any)
	obs, _ := d["observed"].(map[string]any)
	if enf["strategy"] != "role" || obs["freshness"] != "unavailable" || len(enf["limitations"].([]any)) == 0 {
		t.Fatalf("session view must state the role contract and that the switch is not observed: enforcement=%v observed=%v", enf, obs)
	}

	// 2. operator Quarantine: role CoA, targeted at this session, no re-auth.
	h.do("POST", "/api/v1/sessions/"+s1+"/quarantine", nil, 202, nil, true)
	h.app.Pilot().Wait()
	p := lastCoAPacket()
	if r, ok := p.VSAString(radius.VendorAruba, radius.ArubaAttrUserRole); !ok || r != "nac-quarantine" {
		t.Fatalf("CoA lacks Aruba-User-Role=nac-quarantine: %q %v", r, ok)
	}
	if _, has := p.Get(radius.AttrTunnelPrivateGroupID); has {
		t.Fatal("role CoA carries Tunnel-Private-Group-Id")
	}
	if p.String(radius.AttrNASPortID) != arubaPort || p.String(radius.AttrCallingStationID) != "00-50-00-00-02-00" || p.String(radius.AttrUserName) == "" {
		t.Fatal("role CoA is not session-targeted (User-Name, NAS-Port-Id, Calling-Station-Id)")
	}
	_, d = session()
	lc, _ := d["last_coa"].(map[string]any)
	req, _ := d["requested"].(map[string]any)
	if lc["state"] != "acked" || !strings.Contains(lc["detail"].(string), "not verification") {
		t.Fatalf("role CoA-ACK must be acked and say it is not verification: %v", lc)
	}
	if d["override"] != "quarantine" || lc["requested_role"] != "nac-quarantine" {
		t.Fatalf("override/requested role: %v %v (requested %v)", d["override"], lc["requested_role"], req)
	}

	// 3. Persistence: re-authentication, accounting Stop/Start and session
	// replacement keep the identity-scoped quarantine.
	if r := role(auth("acct-1")); r != "nac-quarantine" {
		t.Fatalf("re-authentication dropped the quarantine: %q", r)
	}
	acct("Stop", "acct-1")
	if r := role(auth("acct-2")); r != "nac-quarantine" {
		t.Fatalf("session replacement dropped the quarantine: %q", r)
	}
	acct("Start", "acct-2")

	// 4. nacd restart: still quarantined.
	h.stop()
	h = start(t, dir)
	if r := role(auth("acct-2")); r != "nac-quarantine" {
		t.Fatalf("restart dropped the quarantine: %q", r)
	}
	s2, _ := session()

	// 5. CoA timeout: recorded as timeout, never as applied; the override
	// stays and still applies at the next authentication.
	sw.mu.Lock()
	sw.silent = true
	sw.mu.Unlock()
	h.do("POST", "/api/v1/sessions/"+s2+"/reauthenticate", nil, 202, nil, true)
	h.app.Pilot().Wait()
	_, d = session()
	lc, _ = d["last_coa"].(map[string]any)
	if lc["state"] != "timeout" {
		t.Fatalf("silent switch: CoA state %v", lc["state"])
	}
	sw.mu.Lock()
	sw.silent = false
	sw.mu.Unlock()

	// 6. Restore: role CoA back to corp.
	h.do("POST", "/api/v1/sessions/"+s2+"/restore", nil, 202, nil, true)
	h.app.Pilot().Wait()
	if r, _ := lastCoAPacket().VSAString(radius.VendorAruba, radius.ArubaAttrUserRole); r != "nac-corp" {
		t.Fatalf("restore CoA role %q", r)
	}
	if r := role(auth("acct-2")); r != "nac-corp" {
		t.Fatalf("restored identity still gets %q", r)
	}

	// 7. A role switch cannot send a reject by CoA: when policy would deny,
	// the request is refused and the override is untouched.
	var pol struct{ Items []map[string]any }
	h.do("GET", "/api/v1/policies", nil, 200, &pol, true)
	h.do("DELETE", "/api/v1/policies/"+pol.Items[0]["id"].(string), nil, 204, nil, true)
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &pv, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": pv["draft_digest"]}, 201, nil, true)
	h.do("POST", "/api/v1/sessions/"+s2+"/reauthenticate", nil, 422, nil, true)
	// Fail-safe: quarantine, then a Restore the policy would turn into a
	// deny is refused and the session STAYS quarantined.
	h.do("POST", "/api/v1/sessions/"+s2+"/quarantine", nil, 202, nil, true)
	h.app.Pilot().Wait()
	h.do("POST", "/api/v1/sessions/"+s2+"/restore", nil, 422, nil, true)
	if _, d = session(); d["override"] != "quarantine" {
		t.Fatalf("refused restore changed the override: %v", d["override"])
	}

	// 8. Stale session: no CoA to an ended session.
	acct("Stop", "acct-2")
	h.do("POST", "/api/v1/sessions/"+s2+"/quarantine", nil, 422, nil, true)
	h.stop()
}

func TestArubaRolePath_Validation(t *testing.T) {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	defer h.stop()
	base := func() map[string]any {
		return map[string]any{"name": "sw", "platform": "aruba-aos-cx", "radius_source_ip": "10.0.0.9", "radius_secret": swSecret,
			"segment_roles": map[string]string{"corp": "nac-corp", "quarantine": "nac-quarantine"}, "access_ports": []string{arubaPort}}
	}
	cases := map[string]func(m map[string]any){
		"aruba with VLAN mappings":    func(m map[string]any) { m["segment_vlans"] = map[string]int{"corp": 10} },
		"aruba with vlan enforcement": func(m map[string]any) { m["enforcement"] = "vlan" },
		"no quarantine role":          func(m map[string]any) { m["segment_roles"] = map[string]string{"corp": "nac-corp"} },
		"same role twice": func(m map[string]any) {
			m["segment_roles"] = map[string]string{"corp": "r", "quarantine": "r"}
		},
		"bad role name":    func(m map[string]any) { m["segment_roles"] = map[string]string{"corp": "a b", "quarantine": "q"} },
		"aruba with eAPI":  func(m map[string]any) { m["eapi_url"] = "https://10.0.0.9" },
		"unknown platform": func(m map[string]any) { m["platform"] = "cisco-ios" },
		"arista with roles": func(m map[string]any) {
			m["platform"] = "arista-eos"
			m["segment_vlans"] = map[string]int{"corp": 10, "quarantine": 20}
		},
	}
	for name, mut := range cases {
		m := base()
		mut(m)
		h.do("POST", "/api/v1/switches", m, 422, nil, true)
		_ = name
	}
	h.do("POST", "/api/v1/switches", base(), 201, nil, true)
}
