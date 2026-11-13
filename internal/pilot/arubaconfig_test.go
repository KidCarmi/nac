package pilot

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// U3 (ASTRA rc5 run, 2026-11-13): the generated AOS-CX reference configuration
// said the NAC-driven path was "not yet lab-qualified" after the NAC-integrated
// rc4 qualification. The header must state exactly what was qualified, and on
// which platform, without claiming physical-switch or isolation results.
func TestArubaConfig_QualificationWording(t *testing.T) {
	sw := &domain.Switch{ID: uuid.New(), Name: "aos-cx-1", Platform: domain.PlatformArubaAOSCX,
		RadiusSecret: "s3cret-s3cret-s3cret", RadiusServerIP: "10.70.99.5",
		SegmentRoles: map[string]string{"corp": "corp", "quarantine": "quarantine"}, AccessPorts: []string{"1/1/1"}}
	cfg := ArubaConfig(sw, false)
	for _, want := range []string{
		"! Lab-tested with NAC on ArubaOS-CX Virtual.10.07.0004 (isolated virtual lab, pilot-aruba-rc4): role assignment at admission and role CoA.",
		"! Not verified on physical switches.",
		"it is not packet isolation or connection teardown.",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("reference configuration lacks %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "not yet lab-qualified") {
		t.Errorf("stale qualification wording is still emitted:\n%s", cfg)
	}
	for _, line := range strings.Split(cfg, "\n") {
		if strings.Contains(line, "Lab-tested") && !strings.HasPrefix(line, "!") {
			t.Errorf("qualification wording must be a comment line, got %q", line)
		}
	}
}
