// Package validation provides small, dependency-free input validators.
//
// All external input is treated as hostile. Validators here are used by the
// domain services so that validation is enforced regardless of which adapter
// (HTTP, CLI, future gRPC) calls into the core.
package validation

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Errors accumulates field-level validation failures. It implements error so
// it can be returned through the normal error path, while still exposing the
// per-field map for RFC 7807 rendering.
type Errors struct {
	fields map[string]string
}

// NewErrors returns an empty validation error accumulator.
func NewErrors() *Errors {
	return &Errors{fields: make(map[string]string)}
}

// Add records a failure for the named field. The first message for a field
// wins, which keeps messages stable and avoids unbounded growth.
func (e *Errors) Add(field, msg string) {
	if _, exists := e.fields[field]; !exists {
		e.fields[field] = msg
	}
}

// HasErrors reports whether any field failed validation.
func (e *Errors) HasErrors() bool { return len(e.fields) > 0 }

// Fields returns a copy of the field→message map.
func (e *Errors) Fields() map[string]string {
	out := make(map[string]string, len(e.fields))
	for k, v := range e.fields {
		out[k] = v
	}
	return out
}

func (e *Errors) Error() string {
	parts := make([]string, 0, len(e.fields))
	for k, v := range e.fields {
		parts = append(parts, fmt.Sprintf("%s: %s", k, v))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// ErrValidation is the sentinel that callers can errors.Is against.
var ErrValidation = errors.New("validation error")

func (e *Errors) Is(target error) bool { return target == ErrValidation }

var (
	// macRE matches the canonical lowercase colon-separated MAC form.
	macRE = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)
	// emailRE is a deliberately conservative, non-exhaustive email check.
	// Full RFC 5322 validation is intentionally avoided; we only guard
	// against obviously malformed input and rely on verification flows
	// elsewhere for deliverability.
	emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// NormalizeMAC canonicalizes a MAC address to lowercase colon-separated form.
// It accepts colon-, hyphen-, and dot-separated inputs as well as bare hex.
// It returns an error if the input is not a valid 48-bit MAC.
func NormalizeMAC(in string) (string, error) {
	cleaned := strings.ToLower(strings.TrimSpace(in))
	cleaned = strings.NewReplacer("-", "", ":", "", ".", "").Replace(cleaned)
	if len(cleaned) != 12 {
		return "", fmt.Errorf("%w: MAC must be 48 bits", ErrValidation)
	}
	for _, r := range cleaned {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			return "", fmt.Errorf("%w: MAC contains non-hex characters", ErrValidation)
		}
	}
	var b strings.Builder
	for i := 0; i < len(cleaned); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(cleaned[i : i+2])
	}
	out := b.String()
	if !macRE.MatchString(out) {
		return "", fmt.Errorf("%w: invalid MAC", ErrValidation)
	}
	return out, nil
}

// IsEmail reports whether s looks like a syntactically valid email address.
func IsEmail(s string) bool {
	if utf8.RuneCountInString(s) > 254 {
		return false
	}
	return emailRE.MatchString(s)
}

// IsIP reports whether s parses as a valid IPv4 or IPv6 address.
func IsIP(s string) bool {
	return net.ParseIP(s) != nil
}

// MaxLen reports whether s is within max runes (not bytes), guarding against
// resource exhaustion from oversized fields.
func MaxLen(s string, max int) bool {
	return utf8.RuneCountInString(s) <= max
}

// NonEmpty reports whether s contains non-whitespace content.
func NonEmpty(s string) bool {
	return strings.TrimSpace(s) != ""
}
