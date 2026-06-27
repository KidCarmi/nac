package validation

import (
	"regexp"
	"testing"
)

// canonicalMAC mirrors the canonical output form NormalizeMAC must produce.
var canonicalMAC = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)

// FuzzNormalizeMAC asserts the MAC normalizer is total (never panics on hostile
// input) and that, when it accepts an input, the output is in canonical form
// and is idempotent (normalizing it again yields the same value). MAC parsing
// sits on the access-evaluation hot path, so robustness here is security
// relevant.
func FuzzNormalizeMAC(f *testing.F) {
	for _, seed := range []string{
		"aa:bb:cc:dd:ee:ff", "AA-BB-CC-DD-EE-FF", "aabb.ccdd.eeff",
		"aabbccddeeff", "", "zz", "   ", "::::::", "00:00:00:00:00:00",
		"\x00\xff", "g1:g2:g3:g4:g5:g6",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, err := NormalizeMAC(in)
		if err != nil {
			return // rejection is always acceptable
		}
		if !canonicalMAC.MatchString(out) {
			t.Fatalf("accepted %q but produced non-canonical %q", in, out)
		}
		again, err2 := NormalizeMAC(out)
		if err2 != nil || again != out {
			t.Fatalf("normalization not idempotent: %q -> %q -> (%q,%v)", in, out, again, err2)
		}
	})
}

// FuzzIsIP asserts the IP validator never panics on arbitrary input.
func FuzzIsIP(f *testing.F) {
	for _, seed := range []string{"192.168.0.1", "::1", "", "999.999.999.999", "not-an-ip"} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, in string) {
		_ = IsIP(in)
	})
}
