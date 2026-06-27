package auth

import (
	"errors"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !ok {
		t.Fatalf("expected verify success, ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong password", hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected verify failure for wrong password")
	}
}

func TestHashPassword_RejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatalf("expected error for empty password")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	for _, bad := range []string{
		"",
		"not-a-hash",
		"$argon2i$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",  // wrong variant
		"$argon2id$v=18$m=1,t=1,p=1$c2FsdA$aGFzaA", // wrong version
	} {
		if _, err := VerifyPassword("x", bad); !errors.Is(err, ErrInvalidHash) {
			t.Fatalf("expected ErrInvalidHash for %q, got %v", bad, err)
		}
	}
}

func TestHashPassword_ProducesDistinctSalts(t *testing.T) {
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Fatalf("expected distinct hashes due to random salt")
	}
}
