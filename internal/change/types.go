// Package change implements review-only change requests (ADR-0008, slice A
// of docs/architecture/implementation-plan-change-request-core.md).
//
// A change request is a persisted, revisioned PROPOSAL for the policy rule
// set, with recorded validation and (later) a reproducible historical
// replay. It is review-only: nothing in this package changes the active
// policy, a session, a CoA or a switch, and the legacy publish and restore
// paths are unchanged, so a change request is not governance of policy
// changes. Approval, activation, dispatch and delegated principals are out of
// scope.
package change

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
)

// WorkflowReviewOnly labels every change request in this slice.
const WorkflowReviewOnly = "review_only"

// State of a change request. Later workflow states (approval, execution) do
// not exist in this slice; the database CHECK constraint excludes them.
type State string

// States.
const (
	StateDraft     State = "draft"
	StateValidated State = "validated"
	StateSimulated State = "simulated"
	StateCancelled State = "cancelled"
)

// Terminal reports whether no further change is allowed.
func (s State) Terminal() bool { return s == StateCancelled }

// Content is a proposed rule set, in the same shape as the policy draft.
type Content struct {
	Rules         []domain.Policy     `json:"rules"`
	DefaultAction domain.PolicyAction `json:"default_action"`
}

// Digest is the canonical content hash (policyver.Digest), so a change
// request's digest is comparable with draft and version digests.
func (c Content) Digest() string { return policyver.Digest(c.Rules, c.DefaultAction) }

// ChangeRequest is the current view of a change request: its header plus the
// content digest and base version of its current revision.
type ChangeRequest struct {
	ID                uuid.UUID `json:"id"`
	Title             string    `json:"title"`
	Intent            string    `json:"intent"`
	Workflow          string    `json:"workflow"`
	State             State     `json:"state"`
	Revision          int       `json:"revision"`
	ContentDigest     string    `json:"content_digest"`
	BaseVersionID     uuid.UUID `json:"base_version_id"`
	BaseVersionNumber int       `json:"base_version_number"`
	BaseVersionDigest string    `json:"base_version_digest"`
	RequestedBy       string    `json:"requested_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Revision is one immutable revision of a change request's content.
type Revision struct {
	ChangeRequestID   uuid.UUID `json:"change_request_id"`
	Revision          int       `json:"revision"`
	Content           Content   `json:"content"`
	ContentDigest     string    `json:"content_digest"`
	BaseVersionID     uuid.UUID `json:"base_version_id"`
	BaseVersionNumber int       `json:"base_version_number"`
	BaseVersionDigest string    `json:"base_version_digest"`
	Author            string    `json:"author"`
	CreatedAt         time.Time `json:"created_at"`
}

// Validation is an immutable record of validating one revision against the
// environment (switch segment mappings) as it was then.
type Validation struct {
	ID                uuid.UUID            `json:"id"`
	ChangeRequestID   uuid.UUID            `json:"change_request_id"`
	Revision          int                  `json:"revision"`
	ContentDigest     string               `json:"content_digest"`
	EnvironmentDigest string               `json:"environment_digest"`
	Valid             bool                 `json:"valid"`
	Result            policyver.Validation `json:"result"`
	CreatedBy         string               `json:"created_by"`
	CreatedAt         time.Time            `json:"created_at"`
}

// RevisionConflict is returned when an operation names a revision that is not
// the current one (or the change request moved state). It wraps
// domain.ErrConflict (HTTP 409) and names the current revision.
type RevisionConflict struct {
	Current int
	State   State
}

func (e *RevisionConflict) Error() string {
	return fmt.Sprintf("%s: the change request is at revision %d (%s)", domain.ErrConflict, e.Current, e.State)
}

// Unwrap makes errors.Is(err, domain.ErrConflict) true.
func (e *RevisionConflict) Unwrap() error { return domain.ErrConflict }

// ErrTerminal is returned for any change to a cancelled change request.
var ErrTerminal = fmt.Errorf("%w: the change request is cancelled and can no longer change", domain.ErrConflict)

// ListFilter narrows a listing.
type ListFilter struct {
	State State
	Limit int
}

// Repository persists change requests. Every mutation takes the audit event
// that describes it and writes both in ONE transaction: if the audit record
// cannot be written, the mutation is not committed.
type Repository interface {
	Create(ctx context.Context, cr *ChangeRequest, rev *Revision, a *domain.AuditEvent) error
	Get(ctx context.Context, id uuid.UUID) (*ChangeRequest, error)
	List(ctx context.Context, f ListFilter) ([]ChangeRequest, error)
	Revision(ctx context.Context, id uuid.UUID, revision int) (*Revision, error)
	Revisions(ctx context.Context, id uuid.UUID) ([]Revision, error)
	// Edit appends rev as revision expected+1 if expected is current, and
	// returns the change request to draft.
	Edit(ctx context.Context, id uuid.UUID, expected int, rev *Revision, a *domain.AuditEvent) (*ChangeRequest, error)
	// Cancel moves the change request to cancelled if expected is current.
	Cancel(ctx context.Context, id uuid.UUID, expected int, a *domain.AuditEvent) (*ChangeRequest, error)
	// RecordValidation stores v if v.Revision is current; a valid result
	// moves a draft to validated, an invalid one moves it back to draft.
	RecordValidation(ctx context.Context, v *Validation, a *domain.AuditEvent) (*ChangeRequest, error)
	Validations(ctx context.Context, id uuid.UUID) ([]Validation, error)
	// RecordSimulation stores the input set and the simulation of the
	// current revision (state validated or simulated) and moves the change
	// request to simulated, with its audit record, in one transaction.
	RecordSimulation(ctx context.Context, set *InputSet, sim *Simulation, a *domain.AuditEvent) (*ChangeRequest, error)
	// Simulations lists simulations without their rows.
	Simulations(ctx context.Context, id uuid.UUID) ([]Simulation, error)
	Simulation(ctx context.Context, id, simID uuid.UUID) (*Simulation, error)
	InputSet(ctx context.Context, id, setID uuid.UUID) (*InputSet, error)
}

// ErrNotValidated is returned when simulating a revision that has no valid
// validation since its last edit.
var ErrNotValidated = fmt.Errorf("%w: validate the current revision first (state must be validated or simulated)", domain.ErrConflict)
