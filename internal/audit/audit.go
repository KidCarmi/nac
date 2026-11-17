// Package audit records security-relevant actions to an append-only store and
// mirrors them to the structured log. Audit writes are best-effort with
// respect to the caller's request: a failure to persist an audit event is
// logged at error level but does not fail the underlying operation, because
// the alternative — denying a legitimate action because the audit sink is
// briefly unavailable — is usually worse. This trade-off is documented in
// docs/security/audit-logging.md and can be tightened per-deployment.
package audit

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/observability"
)

// Outcome constants for audit events.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

// Recorder writes audit events.
type Recorder struct {
	repo domain.AuditRepository
	now  func() time.Time
}

// NewRecorder constructs a Recorder backed by repo.
func NewRecorder(repo domain.AuditRepository) *Recorder {
	return &Recorder{repo: repo, now: time.Now}
}

// Event builds an audit event from the request context (actor, request ID,
// remote address) WITHOUT persisting it. Callers that must not commit a
// mutation without its audit record pass the event to a repository that
// writes both in one transaction (e.g. change requests); Record is the
// best-effort path.
func (r *Recorder) Event(ctx context.Context, action, resourceType, resourceID, outcome string, meta map[string]string) *domain.AuditEvent {
	return NewEvent(ctx, r.now().UTC(), action, resourceType, resourceID, outcome, meta)
}

// NewEvent builds an audit event at time at from the request context.
func NewEvent(ctx context.Context, at time.Time, action, resourceType, resourceID, outcome string, meta map[string]string) *domain.AuditEvent {
	ev := &domain.AuditEvent{
		ID:           uuid.New(),
		OccurredAt:   at,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Outcome:      outcome,
		Metadata:     meta,
	}
	if p, ok := auth.PrincipalFrom(ctx); ok {
		id := p.ID
		ev.ActorID = &id
		ev.ActorEmail = p.Email
	}
	if tc, ok := observability.TraceFrom(ctx); ok {
		ev.RequestID = tc.SpanID
	}
	if ra, ok := remoteAddrFrom(ctx); ok {
		ev.RemoteAddr = ra
	}
	return ev
}

// Record persists an audit event derived from the request context. The actor,
// request ID and remote address are pulled from context so call sites only
// specify the action-specific fields. It is best-effort (see the package
// comment).
func (r *Recorder) Record(ctx context.Context, action, resourceType, resourceID, outcome string, meta map[string]string) {
	ev := r.Event(ctx, action, resourceType, resourceID, outcome, meta)

	log := observability.LoggerFrom(ctx)
	if err := r.repo.Append(ctx, ev); err != nil {
		log.Error("failed to persist audit event",
			"action", action, "resource_type", resourceType, "error", err)
	}
	log.Info("audit",
		"action", action,
		"resource_type", resourceType,
		"resource_id", resourceID,
		"outcome", outcome,
		"actor", ev.ActorEmail,
	)
}

type remoteAddrKey struct{}

// WithRemoteAddr stores the client's network address for audit attribution.
func WithRemoteAddr(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, remoteAddrKey{}, addr)
}

func remoteAddrFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(remoteAddrKey{}).(string)
	return v, ok && v != ""
}
