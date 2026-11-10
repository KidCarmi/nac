// Package secrets encrypts small operator-supplied secrets (switch RADIUS
// shared secrets, eAPI passwords) at rest with AES-256-GCM, so a database dump
// alone does not disclose them. The key comes from NAC_SECRETS_KEY.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const prefix = "v1:"

// Box seals and opens secrets.
type Box struct{ aead cipher.AEAD }

// ErrNoKey is returned when no key is configured.
var ErrNoKey = errors.New("NAC_SECRETS_KEY is not set")

// New derives an AES-256 key from the configured key material. Material must
// be at least 32 characters (e.g. `openssl rand -base64 32`).
func New(material string) (*Box, error) {
	if material == "" {
		return nil, ErrNoKey
	}
	if len(material) < 32 {
		return nil, errors.New("NAC_SECRETS_KEY must be at least 32 characters")
	}
	key := sha256.Sum256([]byte("nac-secrets-v1:" + material))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. The empty string seals to the empty string.
func (b *Box) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Open decrypts a sealed value.
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealed, prefix) {
		return "", errors.New("unrecognised secret format")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, prefix))
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errors.New("corrupt sealed secret")
	}
	pt, err := b.aead.Open(nil, raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("cannot decrypt secret (was NAC_SECRETS_KEY changed?): %w", err)
	}
	return string(pt), nil
}
