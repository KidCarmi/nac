package secrets

import (
	"strings"
	"testing"
)

func TestSealOpenRoundTripAndWrongKey(t *testing.T) {
	b, err := New(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal("radius-secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "radius-secret") {
		t.Fatalf("plaintext leaked into sealed value")
	}
	if got, err := b.Open(sealed); err != nil || got != "radius-secret" {
		t.Fatalf("open = %q, %v", got, err)
	}
	other, _ := New(strings.Repeat("x", 32))
	if _, err := other.Open(sealed); err == nil {
		t.Fatalf("expected failure with a different key")
	}
	if _, err := New("short"); err == nil {
		t.Fatalf("short key must be rejected")
	}
}
