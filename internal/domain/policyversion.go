package domain

import (
	"time"

	"github.com/google/uuid"
)

// PolicyVersion is an immutable, complete snapshot of the rule set. Exactly
// one version is active at a time; decisions record the version they used so
// historical decisions remain explainable after later changes.
type PolicyVersion struct {
	ID            uuid.UUID    `json:"id"`
	Number        int          `json:"number"`
	Rules         []Policy     `json:"rules"`
	DefaultAction PolicyAction `json:"default_action"`
	// Digest is a SHA-256 over the canonical rules + default action. Publish
	// requires the digest the operator previewed, so what was previewed is
	// exactly what becomes active.
	Digest    string    `json:"digest"`
	Note      string    `json:"note,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Active    bool      `json:"active"`
}

// RuleSet is what the decision path evaluates.
type RuleSet struct {
	VersionID     uuid.UUID
	VersionNumber int
	Rules         []Policy
	DefaultAction PolicyAction
}

// ActivationKind records why a version became active.
type ActivationKind string

const (
	ActivationInitial ActivationKind = "initial"
	ActivationPublish ActivationKind = "publish"
	ActivationRestore ActivationKind = "restore"
)

// PolicyActivation is an append-only record of a version becoming active.
type PolicyActivation struct {
	ID                    int64          `json:"id"`
	VersionID             uuid.UUID      `json:"version_id"`
	VersionNumber         int            `json:"version_number"`
	Kind                  ActivationKind `json:"kind"`
	PreviousVersionID     *uuid.UUID     `json:"previous_version_id,omitempty"`
	PreviousVersionNumber *int           `json:"previous_version_number,omitempty"`
	ActivatedBy           string         `json:"activated_by"`
	ActivatedAt           time.Time      `json:"activated_at"`
	Note                  string         `json:"note,omitempty"`
}
