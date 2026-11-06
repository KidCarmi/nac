package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Page describes pagination parameters for list operations. Limit is clamped
// by the services to a safe maximum to prevent unbounded result sets.
type Page struct {
	Limit  int
	Offset int
}

// EndpointFilter narrows endpoint list queries. Zero-value fields are ignored.
type EndpointFilter struct {
	AccessStatus AccessStatus
	Search       string // matches MAC or hostname prefix
}

// EndpointRepository persists endpoints. Implementations must translate
// storage-level uniqueness violations on MAC into ErrConflict.
type EndpointRepository interface {
	Create(ctx context.Context, e *Endpoint) error
	GetByID(ctx context.Context, id uuid.UUID) (*Endpoint, error)
	GetByMAC(ctx context.Context, mac string) (*Endpoint, error)
	Update(ctx context.Context, e *Endpoint) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f EndpointFilter, p Page) ([]Endpoint, error)
}

// PolicyRepository persists policies.
type PolicyRepository interface {
	Create(ctx context.Context, p *Policy) error
	GetByID(ctx context.Context, id uuid.UUID) (*Policy, error)
	Update(ctx context.Context, p *Policy) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, p Page) ([]Policy, error)
	// ListEnabled returns all enabled policies ordered by ascending priority.
	// The access engine relies on this ordering for first-match semantics.
	ListEnabled(ctx context.Context) ([]Policy, error)
}

// OperatorRepository persists administrative users.
type OperatorRepository interface {
	Create(ctx context.Context, o *Operator) error
	GetByID(ctx context.Context, id uuid.UUID) (*Operator, error)
	GetByEmail(ctx context.Context, email string) (*Operator, error)
	List(ctx context.Context, p Page) ([]Operator, error)
	// SetPassword stores a new password hash and increments the operator's
	// token version (revoking existing sessions); it returns the new version.
	SetPassword(ctx context.Context, id uuid.UUID, hash string, at time.Time) (int, error)
}

// AuditRepository appends and queries audit events. Writes are append-only;
// there is intentionally no update or delete method.
type AuditRepository interface {
	Append(ctx context.Context, e *AuditEvent) error
	List(ctx context.Context, p Page) ([]AuditEvent, error)
}

// PolicyCache caches the enabled policy set to keep the hot access-decision
// path off the database. A nil/no-op implementation is acceptable.
type PolicyCache interface {
	GetEnabled(ctx context.Context) ([]Policy, bool)
	SetEnabled(ctx context.Context, policies []Policy)
	Invalidate(ctx context.Context)
}
