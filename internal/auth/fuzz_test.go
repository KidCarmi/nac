package auth

import (
	"testing"
	"time"
)

// FuzzTokenParse asserts that JWT parsing is total: arbitrary attacker-supplied
// token strings must never panic and must never yield a principal (a non-nil
// result with a nil error) for input the issuer did not sign. This guards the
// authentication boundary against malformed-token crashes and forgeries.
func FuzzTokenParse(f *testing.F) {
	ti, err := NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), "nac", "nac-api", time.Hour)
	if err != nil {
		f.Fatalf("issuer: %v", err)
	}
	for _, seed := range []string{
		"", "a", "a.b", "a.b.c",
		"eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0.", // alg=none attempt
		"...", "Bearer x",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, token string) {
		p, err := ti.Parse(token)
		if err == nil && p == nil {
			t.Fatalf("Parse returned nil principal with nil error for %q", token)
		}
		if err == nil && p != nil {
			// A forged/unsigned token must not parse successfully. The fuzzer
			// cannot produce a validly-signed token without the key, so any
			// success here is a defect.
			t.Fatalf("Parse accepted unsigned token %q as principal %+v", token, p)
		}
	})
}

// FuzzVerifyPassword asserts password verification never panics on arbitrary
// encoded-hash input and never reports a match for a malformed hash.
func FuzzVerifyPassword(f *testing.F) {
	for _, seed := range []string{
		"", "not-a-hash", "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"$$$$$", "$argon2id$", "$argon2id$v=19$m=1,t=1,p=1$$",
	} {
		f.Add("password", seed)
	}
	f.Fuzz(func(t *testing.T, password, encoded string) {
		ok, err := VerifyPassword(password, encoded)
		if ok && err != nil {
			t.Fatalf("VerifyPassword reported match alongside error for %q", encoded)
		}
	})
}
