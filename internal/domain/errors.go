// Package domain defines the core NAC entities, the access-decision contract,
// and the persistence ports. It has no dependency on any adapter (HTTP,
// PostgreSQL, Redis) so the business rules can be tested in isolation and
// reused across delivery mechanisms.
package domain

import "errors"

// Sentinel errors returned by the domain services. Adapters map these to
// transport-specific representations (e.g. HTTP status codes) so that the
// core never imports net/http.
var (
	// ErrNotFound indicates a requested resource does not exist.
	ErrNotFound = errors.New("resource not found")
	// ErrConflict indicates a uniqueness or state conflict (e.g. duplicate MAC).
	ErrConflict = errors.New("resource conflict")
	// ErrUnauthorized indicates missing or invalid authentication.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden indicates the principal lacks permission for the action.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidInput indicates the caller supplied semantically invalid data.
	ErrInvalidInput = errors.New("invalid input")
)
