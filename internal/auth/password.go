// Package auth provides password hashing, JWT session tokens, RBAC, and the
// request principal carried through context. It is the platform's identity and
// access-management core and is written to fail closed.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2 parameters. These follow OWASP Password Storage Cheat Sheet guidance
// for Argon2id (memory-hard) and are encoded into each hash so they can be
// tuned upward over time without invalidating existing credentials.
type argon2Params struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
}

var defaultParams = argon2Params{
	memoryKiB:   64 * 1024, // 64 MiB
	iterations:  3,
	parallelism: 2,
	saltLen:     16,
	keyLen:      32,
}

// ErrInvalidHash indicates a stored hash is malformed or uses an unsupported
// algorithm.
var ErrInvalidHash = errors.New("invalid password hash")

// HashPassword returns an encoded Argon2id hash in the standard PHC string
// format: $argon2id$v=19$m=...,t=...,p=...$salt$hash.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	p := defaultParams
	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.iterations, p.memoryKiB, p.parallelism, p.keyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		p.memoryKiB, p.iterations, p.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded Argon2id hash.
// The comparison is constant-time to avoid timing oracles. A malformed hash
// returns (false, ErrInvalidHash).
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}
	var p argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memoryKiB, &p.iterations, &p.parallelism); err != nil {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}
	// The derived key length must match the stored hash length. Guard the
	// conversion so a maliciously oversized hash cannot overflow uint32.
	if len(want) == 0 || len(want) > 1024 {
		return false, ErrInvalidHash
	}
	keyLen := uint32(len(want)) //nolint:gosec // len(want) is bounded to [1,1024] by the guard above
	got := argon2.IDKey([]byte(password), salt, p.iterations, p.memoryKiB, p.parallelism, keyLen)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
