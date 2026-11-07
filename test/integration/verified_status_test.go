//go:build integration

package integration

import (
	"strings"
	"testing"
)

// ASTRA pilot-rc7: after a newer session replaced the one used for Verify,
// the switch still showed verified=yes naming the ended session, while
// reachability was "no". Verified is a commissioning record: it must be
// marked historical and say whether its session is still the active one.
func TestVerifiedIsMarkedAsHistoricalRecord(t *testing.T) {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	defer h.stop()

	var sw map[string]any
	h.do("POST", "/api/v1/switches", map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"segment_vlans": map[string]int{"corp": 10, "quarantine": 20}, "access_ports": []string{"Ethernet1"}}, 201, &sw, true)
	swID := sw["id"].(string)
	var ep map[string]any
	h.do("POST", "/api/v1/endpoints", map[string]any{"mac": endpointMAC, "hostname": "h1"}, 201, &ep, true)
	h.do("POST", "/api/v1/identity-bindings", map[string]any{"endpoint_id": ep["id"], "issuer": caIssuer, "serial": certSerial}, 201, nil, true)
	h.do("POST", "/api/v1/policies", map[string]any{"name": "bound-corp", "priority": 10, "enabled": true, "action": "allow", "segment": "corp",
		"conditions": []map[string]any{{"field": "identity.bound", "operator": "eq", "values": []string{"true"}}}}, 201, nil, true)
	var pv map[string]any
	h.do("GET", "/api/v1/policy-draft/preview", nil, 200, &pv, true)
	h.do("POST", "/api/v1/policy-draft/publish", map[string]any{"expected_digest": pv["draft_digest"]}, 201, nil, true)

	auth := func(acct string) {
		a := authAttrs()
		a["Acct-Session-Id"] = acct
		h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", a, 200)
		h.radiusCall("/api/v1/radius/accounting", "127.0.0.1", map[string]string{"Acct-Status-Type": "Start", "Acct-Session-Id": acct,
			"Calling-Station-Id": "00-50-00-00-02-00", "NAS-Port-Id": "Ethernet1"}, 204)
	}
	verified := func() map[string]any {
		var v map[string]any
		h.do("GET", "/api/v1/switches/"+swID, nil, 200, &v, true)
		return v["status"].(map[string]any)["verified"].(map[string]any)
	}

	auth("acct-1")
	// Switch authorization, the switch's view and endpoint reachability stay
	// separate facts: NAC never claims the endpoint is reachable.
	var sessions struct {
		Sessions []map[string]any `json:"sessions"`
	}
	h.do("GET", "/api/v1/sessions?state=active", nil, 200, &sessions, true)
	var detail map[string]any
	h.do("GET", "/api/v1/sessions/"+sessions.Sessions[0]["id"].(string), nil, 200, &detail, true)
	er, _ := detail["endpoint_reachability"].(map[string]any)
	if er["state"] != "not_observed" || !strings.Contains(er["note"].(string), "does not test the endpoint") {
		t.Fatalf("endpoint_reachability: %v", detail["endpoint_reachability"])
	}
	h.do("POST", "/api/v1/switches/"+swID+"/verify", nil, 200, nil, true)
	v := verified()
	if v["state"] != "yes" || v["historical"] != true || !strings.Contains(v["note"].(string), "still active") {
		t.Fatalf("fresh verification: %v", v)
	}

	// The verified session ends; a newer session becomes current.
	h.radiusCall("/api/v1/radius/accounting", "127.0.0.1", map[string]string{"Acct-Status-Type": "Stop", "Acct-Session-Id": "acct-1",
		"Calling-Station-Id": "00-50-00-00-02-00", "NAS-Port-Id": "Ethernet1"}, 204)
	auth("acct-2")
	v = verified()
	note, _ := v["note"].(string)
	if v["historical"] != true || !strings.Contains(note, "Record of a past verification") || !strings.Contains(note, "ended at") ||
		strings.Contains(note, "still active") {
		t.Fatalf("verified still reads as current after its session ended: %v", v)
	}
}
