// Package problem implements RFC 7807 "Problem Details for HTTP APIs".
//
// A consistent, machine-readable error envelope is a security control as much
// as an ergonomic one: it ensures handlers never leak internal error strings,
// stack traces, or SQL to clients, while still giving operators a correlation
// handle (the request ID) to find the full detail in the server logs.
package problem

import (
	"encoding/json"
	"net/http"
)

// ContentType is the media type mandated by RFC 7807.
const ContentType = "application/problem+json"

// Details is the RFC 7807 problem object. Only Title, Status and Type are
// required by the RFC; the remaining fields are optional extensions.
type Details struct {
	// Type is a URI reference identifying the problem type. We use a stable
	// relative URN under /problems/ so clients can switch on it.
	Type string `json:"type"`
	// Title is a short, human-readable, stable summary of the problem type.
	Title string `json:"title"`
	// Status is the HTTP status code.
	Status int `json:"status"`
	// Detail is a human-readable explanation specific to this occurrence.
	// It MUST NOT contain sensitive internal information.
	Detail string `json:"detail,omitempty"`
	// Instance optionally identifies the specific occurrence (request ID).
	Instance string `json:"instance,omitempty"`
	// Errors carries per-field validation messages for 422 responses.
	Errors map[string]string `json:"errors,omitempty"`
}

// New builds a Details with a conventional type URI derived from the status.
func New(status int, title, detail string) *Details {
	return &Details{
		Type:   typeURI(status),
		Title:  title,
		Status: status,
		Detail: detail,
	}
}

// WithInstance attaches a correlation identifier (typically the request ID).
func (d *Details) WithInstance(instance string) *Details {
	d.Instance = instance
	return d
}

// WithErrors attaches field-level validation errors.
func (d *Details) WithErrors(errs map[string]string) *Details {
	d.Errors = errs
	return d
}

// Write serializes the problem to the response writer with the correct
// content type and status code. Serialization errors are intentionally
// swallowed: at this point the status line is already committed and there is
// nothing useful to do but stop writing.
func (d *Details) Write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(d.Status)
	_ = json.NewEncoder(w).Encode(d)
}

func typeURI(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "/problems/bad-request"
	case http.StatusUnauthorized:
		return "/problems/unauthorized"
	case http.StatusForbidden:
		return "/problems/forbidden"
	case http.StatusNotFound:
		return "/problems/not-found"
	case http.StatusConflict:
		return "/problems/conflict"
	case http.StatusUnprocessableEntity:
		return "/problems/validation"
	case http.StatusTooManyRequests:
		return "/problems/rate-limited"
	default:
		return "/problems/internal"
	}
}
