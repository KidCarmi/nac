package pilot

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// The fixture is ASTRA's capture of `show dot1x hosts | json` on vEOS-lab
// 4.29.2F (lab/switch-session-observed.json, commit f22c9dd).
func TestParseDot1xHosts_VEOSCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/veos-4.29.2F-show-dot1x-hosts.json")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := ParseDot1xHosts(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("want 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	if h.Interface != "Ethernet1" || h.MAC != "00:50:00:00:02:00" || h.AuthStage != "SUCCESS" || h.VLAN != "10" || h.Method != "EAPOL" {
		t.Fatalf("parsed %+v", h)
	}
	if _, err := ParseDot1xHosts([]byte(`{"something":"else"}`)); err == nil {
		t.Fatalf("unexpected JSON shape must be an error, not an empty result")
	}
}

func TestEOSConfig_MatchesLabValidatedConfig(t *testing.T) {
	sw := &domain.Switch{ID: uuid.New(), Name: "nac-veos", RadiusSecret: "s3cret-s3cret-s3cret", RadiusServerIP: "10.70.99.2",
		SegmentVLANs: map[string]int{"corp": 10, "quarantine": 20}, AccessPorts: []string{"Ethernet1"}}
	cfg := EOSConfig(sw, false)
	// Every mandatory line of lab/configs.sanitized.txt (vEOS section).
	for _, line := range []string{
		"vlan 10\n   name corp", "vlan 20\n   name quarantine",
		"radius-server host 10.70.99.2 key <RADIUS_SHARED_SECRET>",
		"aaa authentication dot1x default group radius", "dot1x system-auth-control", "dot1x dynamic-authorization",
		"interface Ethernet1\n   switchport access vlan 10\n   dot1x pae authenticator\n   dot1x port-control auto",
	} {
		if !strings.Contains(cfg, line) {
			t.Fatalf("missing %q", line)
		}
	}
	if strings.Contains(cfg, sw.RadiusSecret) {
		t.Fatalf("secret must be a placeholder unless requested")
	}
	// Unvalidated lines must be comments only.
	for _, l := range strings.Split(cfg, "\n") {
		if strings.Contains(l, "accounting") || strings.Contains(l, "reauth") || strings.Contains(l, "http-commands") {
			if !strings.HasPrefix(l, "!") {
				t.Fatalf("unvalidated line emitted as configuration: %q", l)
			}
		}
	}
	if !strings.Contains(EOSConfig(sw, true), "key s3cret-s3cret-s3cret") {
		t.Fatalf("include_secret must render the key")
	}
}

func TestRenderClients(t *testing.T) {
	out := RenderClients([]domain.Switch{{Name: "nac-veos", RadiusSourceIP: "10.70.99.1", RadiusSecret: "abc-def-ghi-jkl-mno"}})
	if !strings.Contains(out, "client nac-nac-veos {\n\tipaddr = 10.70.99.1\n\tsecret = \"abc-def-ghi-jkl-mno\"") {
		t.Fatalf("clients:\n%s", out)
	}
}

func TestValidateSecretRejectsConfigInjection(t *testing.T) {
	for _, bad := range []string{"short", "has space in it 1234", `quote"quote-quote-q`, "back\\slash-back-slash", "new\nline-new-line-n", "0123456789${confdir}"} {
		if validateSecret(bad) == nil {
			t.Fatalf("secret %q should be rejected", bad)
		}
	}
	if validateSecret("Gq7-x9!kP2#vR5@wL8") != nil {
		t.Fatalf("a strong printable secret should be accepted")
	}
}

// A line exactly as FreeRADIUS 3.2.5 wrote it in scripts/e2e-freeradius.sh.
func TestNormalizeOutageAttrs_RealFreeRADIUSLine(t *testing.T) {
	line := `{"event":"outage-reject","unix_time":1793956987,"attrs":{":User-Name":"wired-endpoint-01",":Calling-Station-Id":"00-50-00-00-02-00",":NAS-Port-Id":"Ethernet1",":TLS-Client-Cert-Issuer":"\\\/CN=NAC Test CA",":Tmp-String-2":"nacd unreachable",":Tmp-String-3":"127.0.0.1"}}`
	var ol outageLine
	if err := json.Unmarshal([]byte(line), &ol); err != nil {
		t.Fatal(err)
	}
	a := normalizeAttrs(ol.Attrs)
	if attr(a, "Calling-Station-Id") != "00-50-00-00-02-00" || attr(a, "Tmp-String-3") != "127.0.0.1" ||
		attr(a, "TLS-Client-Cert-Issuer") != "/CN=NAC Test CA" || attr(a, "NAS-Port-Id") != "Ethernet1" {
		t.Fatalf("normalised attrs: %v", a)
	}
}
