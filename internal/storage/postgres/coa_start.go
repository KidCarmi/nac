package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kidcarmi/nac/internal/domain"
)

// ErrSessionChanged means the session was changed (state, override or
// current decision) between the moment an operator action was prepared and
// the moment it was committed. Nothing was written; the caller re-reads and
// prepares the action again. It wraps domain.ErrConflict.
var ErrSessionChanged = fmt.Errorf("%w: the session changed while the action was being prepared; nothing was recorded", domain.ErrConflict)

// CoAStart is an operator CoA command: the CoA request, the session override
// it implies and the operator's timeline event, persisted together.
type CoAStart struct {
	CoA *domain.CoARequest

	// The session as the action was prepared from. The locked row must
	// still match, or StartCoA writes nothing and returns ErrSessionChanged.
	ExpectedState      string
	ExpectedOverride   string
	ExpectedDecisionID *uuid.UUID

	// SetOverride writes Override/OverrideBy/OverrideAt (operator columns
	// only).
	SetOverride bool
	Override    string
	OverrideBy  string
	OverrideAt  *time.Time

	Event *domain.AuthEvent // operator timeline event (optional)

	// Audit is the operator's security audit record. It is written in the
	// same transaction: if it cannot be persisted, nothing commits and
	// nothing is sent. (Delivery to any external sink stays outside.)
	Audit *domain.AuditEvent
}

type coaStartHookKey struct{}

// WithCoAStartHook returns a context whose StartCoA calls fn(stage) in this
// order: "prepared" (before the transaction begins), then inside it
// "locked", "coa_inserted", "override_set", "event_inserted" and
// "audit_inserted" (the last is immediately before COMMIT), then
// "committed" (after a successful COMMIT). A non-nil error from fn aborts
// StartCoA exactly like a database error at that point; at "committed" it
// simulates a commit whose acknowledgement was lost (the data is committed
// but StartCoA returns an error). CoAStartCommitted calls it at
// "reconcile_lookup".
//
// It exists for failure-injection and concurrency tests. Nothing in nacd sets
// it; a request cannot, because only Go code holding this function can put
// the key on a context.
func WithCoAStartHook(ctx context.Context, fn func(stage string) error) context.Context {
	return context.WithValue(ctx, coaStartHookKey{}, fn)
}

func coaStartHook(ctx context.Context, stage string) error {
	if fn, ok := ctx.Value(coaStartHookKey{}).(func(string) error); ok && fn != nil {
		return fn(stage)
	}
	return nil
}

// StartCoA records an operator CoA command atomically. In ONE transaction it
//
//  1. locks the session row (SELECT ... FOR UPDATE) and checks that its
//     state, override and current decision are still what the action was
//     prepared from (else ErrSessionChanged);
//  2. refuses a second CoA while one is requested or sent (ErrConflict);
//  3. inserts the CoA request (state "requested");
//  4. writes the operator override, if the action changes it;
//  5. inserts the operator timeline event.
//
// Either all of it commits or none of it does: a failure at any step can no
// longer leave a "requested" CoA without its override, which used to block
// every later CoA on the session until nacd restarted. StartCoA performs no
// network I/O; the caller sends the CoA only after it returns nil (after
// COMMIT).
func (r *PilotRepo) StartCoA(ctx context.Context, st *CoAStart) error {
	c := st.CoA
	if err := coaStartHook(ctx, "prepared"); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		state, override string
		decision        *uuid.UUID
	)
	err = tx.QueryRow(ctx, `SELECT state, override, last_decision_id FROM radius_sessions WHERE id=$1 FOR UPDATE`, c.SessionID).
		Scan(&state, &override, &decision)
	if noRows(err) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := coaStartHook(ctx, "locked"); err != nil {
		return err
	}
	if state != st.ExpectedState || override != st.ExpectedOverride || !sameID(decision, st.ExpectedDecisionID) {
		return ErrSessionChanged
	}
	var inFlight int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM coa_requests WHERE session_id=$1 AND state IN ('requested','sent')`, c.SessionID).Scan(&inFlight); err != nil {
		return err
	}
	if inFlight > 0 {
		return fmt.Errorf("%w: a CoA for this session is already in progress", domain.ErrConflict)
	}
	target, _ := json.Marshal(c.Target)
	if _, err := tx.Exec(ctx, `INSERT INTO coa_requests (`+coaColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		c.ID, c.SessionID, c.SwitchID, c.Kind, target, c.RequestedSegment, c.RequestedVLAN, c.State, c.Attempts, c.RadiusID,
		c.ErrorCause, c.Detail, c.CreatedBy, c.CreatedAt, c.SentAt, c.RespondedAt, c.ObservedAt, c.ObservedDecisionID, c.UpdatedAt,
		c.RequestedRole); err != nil {
		return err
	}
	if err := coaStartHook(ctx, "coa_inserted"); err != nil {
		return err
	}
	if st.SetOverride {
		if _, err := tx.Exec(ctx, `UPDATE radius_sessions SET override=$2, override_by=$3, override_at=$4 WHERE id=$1`,
			c.SessionID, st.Override, st.OverrideBy, st.OverrideAt); err != nil {
			return err
		}
	}
	if err := coaStartHook(ctx, "override_set"); err != nil {
		return err
	}
	if st.Event != nil {
		if _, err := insertEvent(ctx, tx, st.Event); err != nil {
			return err
		}
	}
	if err := coaStartHook(ctx, "event_inserted"); err != nil {
		return err
	}
	if st.Audit != nil {
		if err := insertAudit(ctx, tx, st.Audit); err != nil {
			return fmt.Errorf("operator audit record not written (nothing committed): %w", err)
		}
	}
	if err := coaStartHook(ctx, "audit_inserted"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return coaStartHook(ctx, "committed")
}

// insertAudit writes an audit event through the start transaction.
func insertAudit(ctx context.Context, tx pgx.Tx, e *domain.AuditEvent) error {
	meta, err := json.Marshal(orEmptyMap(e.Metadata))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (`+auditColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ID, e.OccurredAt, e.ActorID, e.ActorEmail, e.Action, e.ResourceType, e.ResourceID, e.Outcome, e.RequestID, e.RemoteAddr, meta)
	return err
}

// CoAStartCommitted reports whether the start of CoA id committed. It is
// how a caller resolves an ambiguous StartCoA error (a commit whose
// acknowledgement was lost): the start is atomic, so the CoA row exists if
// and only if the CoA, override, event and audit record all committed.
func (r *PilotRepo) CoAStartCommitted(ctx context.Context, id uuid.UUID) (bool, error) {
	if err := coaStartHook(ctx, "reconcile_lookup"); err != nil {
		return false, err
	}
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM coa_requests WHERE id = $1`, id).Scan(&n); err != nil {
		return false, err
	}
	return n == 1, nil
}

// StaleStart is a CoA whose start committed but that no sender claimed.
type StaleStart struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	SwitchID  uuid.UUID
	NASPortID string
}

// FailUnclaimedCoAs marks as failed every CoA still "requested" (committed
// but never claimed by a sender) that was created before olderThan, and
// returns them. The update is conditional on the state, so it cannot race a
// sender: the sender's first transition is a conditional claim requested ->
// sent, and exactly one of the two wins. Nothing is sent and the session
// override is not touched.
func (r *PilotRepo) FailUnclaimedCoAs(ctx context.Context, olderThan, at time.Time, detail string) ([]StaleStart, error) {
	rows, err := r.pool.Query(ctx, `UPDATE coa_requests SET state = 'failed', updated_at = $2, detail = $3
		WHERE state = 'requested' AND created_at < $1
		RETURNING id, session_id, switch_id, target->>'nas_port_id'`, olderThan, at, detail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StaleStart
	for rows.Next() {
		var s StaleStart
		var port *string
		if err := rows.Scan(&s.ID, &s.SessionID, &s.SwitchID, &port); err != nil {
			return nil, err
		}
		if port != nil {
			s.NASPortID = *port
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// IsSessionChanged reports whether err is ErrSessionChanged.
func IsSessionChanged(err error) bool { return errors.Is(err, ErrSessionChanged) }
