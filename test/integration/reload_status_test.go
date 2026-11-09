//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Known defect D1 (ASTRA chaos run C4 on pilot-rc10): an IPv4-mapped source
// IP was accepted, FreeRADIUS rejected the client file, and nacd still said
// "configured: yes". Configured must now follow what FreeRADIUS loaded, as
// recorded by the reload helper, and the input must be rejected.
func TestReloadStatus_ConfiguredFollowsFreeRADIUSLoad(t *testing.T) {
	resetDB(t, dsn(t))
	dir := t.TempDir()
	h := start(t, dir)
	clients := filepath.Join(dir, "clients.conf")
	statusFile := filepath.Join(dir, "freeradius-reload.status")

	// The C4 inputs are rejected.
	for _, ip := range []string{"::ffff:10.250.99.200", "::ffff:a46:6301"} {
		h.do("POST", "/api/v1/switches", map[string]any{"name": "bad", "radius_source_ip": ip, "radius_secret": swSecret,
			"segment_vlans": map[string]int{"corp": 10, "quarantine": 20}}, 422, nil, true)
	}
	h.do("POST", "/api/v1/switches", map[string]any{"name": "ok-src", "radius_source_ip": "10.250.99.201", "nas_ip": "::ffff:10.250.99.201",
		"radius_secret": swSecret, "segment_vlans": map[string]int{"corp": 10, "quarantine": 20}}, 422, nil, true)

	var sw map[string]any
	h.do("POST", "/api/v1/switches", map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1", "radius_secret": swSecret,
		"segment_vlans": map[string]int{"corp": 10, "quarantine": 20}}, 201, &sw, true)
	id := sw["id"].(string)
	state := func() (string, string) {
		var v map[string]any
		h.do("GET", "/api/v1/switches/"+id, nil, 200, &v, true)
		st := v["status"].(map[string]any)
		c := st["configured"].(map[string]any)
		l := st["freeradius_load"].(map[string]any)
		if c["state"] != l["state"] {
			t.Fatalf("configured %v disagrees with freeradius_load %v", c["state"], l["state"])
		}
		return c["state"].(string), c["detail"].(string)
	}
	sum := func() string {
		b, err := os.ReadFile(clients)
		if err != nil {
			t.Fatal(err)
		}
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	}
	record := func(result, sha, detail string) {
		if err := os.WriteFile(statusFile, []byte("result="+result+"\nclients_sha256="+sha+"\nat=2026-11-09T12:00:00Z\ndetail="+detail+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if st, d := state(); st != "pending" {
		t.Fatalf("written but not loaded must be pending, got %s: %s", st, d)
	}
	record("fail", sum(), `freeradius -C rejected the configuration: Invalid address`)
	if st, d := state(); st != "no" || !strings.Contains(d, "Invalid address") {
		t.Fatalf("rejected list must be no with the reason, got %s: %s", st, d)
	}
	record("ok", sum(), "FreeRADIUS restarted with this client list")
	if st, _ := state(); st != "yes" {
		t.Fatalf("loaded list must be yes, got %s", st)
	}
	var health map[string]any
	h.do("GET", "/api/v1/radius/health", nil, 200, &health, true)
	if l, _ := health["freeradius_load"].(map[string]any); l["state"] != "yes" {
		t.Fatalf("health freeradius_load: %v", health["freeradius_load"])
	}

	// A change FreeRADIUS has not loaded is no longer "yes".
	h.do("PUT", "/api/v1/switches/"+id, map[string]any{"name": "veos", "radius_source_ip": "127.0.0.1",
		"segment_vlans": map[string]int{"corp": 10, "quarantine": 21}, "radius_secret": swSecret + "x"}, 200, nil, true)
	if st, _ := state(); st != "pending" {
		t.Fatalf("secret rotation not yet loaded must be pending, got %s", st)
	}

	// A row stored by rc10 in mapped form is rewritten to dotted IPv4 at
	// startup, so the client file and the RADIUS source lookup work.
	h.stop()
	conn, err := pgx.Connect(context.Background(), dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(context.Background(), `UPDATE switches SET radius_source_ip='::ffff:127.0.0.1', nas_ip='::ffff:127.0.0.1'`); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(context.Background())
	h = start(t, dir)
	defer h.stop()
	h.do("GET", "/api/v1/switches/"+id, nil, 200, &sw, true)
	if sw["radius_source_ip"] != "127.0.0.1" || sw["nas_ip"] != "127.0.0.1" {
		t.Fatalf("legacy mapped address not normalised: %v / %v", sw["radius_source_ip"], sw["nas_ip"])
	}
	b, _ := os.ReadFile(clients)
	if !strings.Contains(string(b), "ipaddr = 127.0.0.1") || strings.Contains(string(b), "::ffff") {
		t.Fatalf("client file after normalisation:\n%s", b)
	}
	h.radiusCall("/api/v1/radius/authorize", "127.0.0.1", authAttrs(), 200)
	var v map[string]any
	h.do("GET", "/api/v1/switches/"+id, nil, 200, &v, true)
	if seen := v["status"].(map[string]any)["auth_request_seen"].(map[string]any); seen["state"] != "yes" {
		t.Fatalf("RADIUS from 127.0.0.1 not attributed to the normalised switch: %v", seen)
	}
}
