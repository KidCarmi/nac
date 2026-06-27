package validation

import "testing"

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"colon":      {"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff", true},
		"hyphen":     {"aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee:ff", true},
		"dot":        {"aabb.ccdd.eeff", "aa:bb:cc:dd:ee:ff", true},
		"bare":       {"aabbccddeeff", "aa:bb:cc:dd:ee:ff", true},
		"whitespace": {"  aa:bb:cc:dd:ee:ff  ", "aa:bb:cc:dd:ee:ff", true},
		"too short":  {"aabbccddee", "", false},
		"non hex":    {"zz:bb:cc:dd:ee:ff", "", false},
		"empty":      {"", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeMAC(tc.in)
			if tc.ok && (err != nil || got != tc.want) {
				t.Fatalf("NormalizeMAC(%q)=%q,%v want %q", tc.in, got, err, tc.want)
			}
			if !tc.ok && err == nil {
				t.Fatalf("NormalizeMAC(%q) expected error", tc.in)
			}
		})
	}
}

func TestIsEmail(t *testing.T) {
	if !IsEmail("a@b.co") {
		t.Errorf("expected valid email")
	}
	for _, bad := range []string{"", "no-at", "a@b", "a b@c.d", "@b.co"} {
		if IsEmail(bad) {
			t.Errorf("expected %q invalid", bad)
		}
	}
}

func TestIsIP(t *testing.T) {
	if !IsIP("192.168.1.1") || !IsIP("2001:db8::1") {
		t.Errorf("expected valid IPs")
	}
	if IsIP("999.0.0.1") || IsIP("notanip") {
		t.Errorf("expected invalid IPs to fail")
	}
}

func TestErrorsAccumulation(t *testing.T) {
	e := NewErrors()
	if e.HasErrors() {
		t.Fatalf("new errors should be empty")
	}
	e.Add("field", "first")
	e.Add("field", "second") // first wins
	if e.Fields()["field"] != "first" {
		t.Fatalf("expected first message to win")
	}
	if !e.HasErrors() {
		t.Fatalf("expected errors")
	}
}
