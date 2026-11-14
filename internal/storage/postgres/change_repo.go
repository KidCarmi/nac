package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/change"
	"github.com/kidcarmi/nac/internal/domain"
)

// ChangeRepo persists review-only change requests (change.Repository).
//
// Every mutation and its audit record are written in ONE transaction: if the
// audit insert fails, nothing is committed. State changes are compare-and-set
// on (state, revision) under a row lock, so concurrent edits, cancels and
// validations of one change request have exactly one winner. Revisions and
// validations are immutable (database triggers reject UPDATE and DELETE).
type ChangeRepo struct {
	pool *pgxpool.Pool
}

// NewChangeRepo constructs a ChangeRepo.
func NewChangeRepo(pool *pgxpool.Pool) *ChangeRepo { return &ChangeRepo{pool: pool} }

var _ change.Repository = (*ChangeRepo)(nil)

// sqlExecer is a pool or a transaction.
type sqlExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// appendAudit writes an audit event through q (a transaction, so that it
// commits or rolls back with the mutation it describes).
func appendAudit(ctx context.Context, q sqlExecer, e *domain.AuditEvent) error {
	if e == nil {
		return errors.New("audit event is required for a change-request mutation")
	}
	meta, err := json.Marshal(orEmptyMap(e.Metadata))
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	_, err = q.Exec(ctx, `INSERT INTO audit_events (`+auditColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ID, e.OccurredAt, e.ActorID, e.ActorEmail, e.Action, e.ResourceType,
		e.ResourceID, e.Outcome, e.RequestID, e.RemoteAddr, meta)
	if err != nil {
		return fmt.Errorf("audit record not written (mutation not committed): %w", err)
	}
	return nil
}

const crSelect = `SELECT c.id, c.title, c.intent, c.workflow, c.state, c.revision, r.content_digest, r.base_version_id, v.number,
	r.base_version_digest, c.requested_by, c.created_at, c.updated_at
	FROM change_requests c
	JOIN change_request_revisions r ON r.change_request_id = c.id AND r.revision = c.revision
	JOIN policy_versions v ON v.id = r.base_version_id`

func scanCR(row rowScanner) (*change.ChangeRequest, error) {
	var c change.ChangeRequest
	err := row.Scan(&c.ID, &c.Title, &c.Intent, &c.Workflow, &c.State, &c.Revision, &c.ContentDigest, &c.BaseVersionID,
		&c.BaseVersionNumber, &c.BaseVersionDigest, &c.RequestedBy, &c.CreatedAt, &c.UpdatedAt)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func insertRevision(ctx context.Context, q sqlExecer, rev *change.Revision) error {
	rules, err := json.Marshal(rev.Content.Rules)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO change_request_revisions (change_request_id, revision, rules, default_action, content_digest,
		base_version_id, base_version_digest, author, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		rev.ChangeRequestID, rev.Revision, rules, rev.Content.DefaultAction, rev.ContentDigest,
		rev.BaseVersionID, rev.BaseVersionDigest, rev.Author, rev.CreatedAt)
	return err
}

// Create inserts a change request with its first revision and audit record.
func (r *ChangeRepo) Create(ctx context.Context, cr *change.ChangeRequest, rev *change.Revision, a *domain.AuditEvent) error {
	if rev.Revision != 1 || cr.Revision != 1 || rev.ChangeRequestID != cr.ID {
		return fmt.Errorf("%w: a new change request starts at revision 1", domain.ErrInvalidInput)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO change_requests (id, title, intent, workflow, state, revision, requested_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		cr.ID, cr.Title, cr.Intent, cr.Workflow, cr.State, cr.Revision, cr.RequestedBy, cr.CreatedAt, cr.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return err
	}
	if err := insertRevision(ctx, tx, rev); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Get returns the current view of a change request.
func (r *ChangeRepo) Get(ctx context.Context, id uuid.UUID) (*change.ChangeRequest, error) {
	return scanCR(r.pool.QueryRow(ctx, crSelect+` WHERE c.id = $1`, id))
}

// List lists change requests, most recently updated first.
func (r *ChangeRepo) List(ctx context.Context, f change.ListFilter) ([]change.ChangeRequest, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	q, args := crSelect, []any{}
	if f.State != "" {
		q += ` WHERE c.state = $1`
		args = append(args, f.State)
	}
	rows, err := r.pool.Query(ctx, q+fmt.Sprintf(` ORDER BY c.updated_at DESC, c.id LIMIT %d`, f.Limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []change.ChangeRequest{}
	for rows.Next() {
		c, err := scanCR(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

const revSelect = `SELECT r.change_request_id, r.revision, r.rules, r.default_action, r.content_digest, r.base_version_id, v.number,
	r.base_version_digest, r.author, r.created_at
	FROM change_request_revisions r JOIN policy_versions v ON v.id = r.base_version_id`

func scanRevision(row rowScanner) (*change.Revision, error) {
	var (
		rev   change.Revision
		rules []byte
	)
	err := row.Scan(&rev.ChangeRequestID, &rev.Revision, &rules, &rev.Content.DefaultAction, &rev.ContentDigest, &rev.BaseVersionID,
		&rev.BaseVersionNumber, &rev.BaseVersionDigest, &rev.Author, &rev.CreatedAt)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rules, &rev.Content.Rules); err != nil {
		return nil, err
	}
	if rev.Content.Rules == nil {
		rev.Content.Rules = []domain.Policy{}
	}
	return &rev, nil
}

// Revision returns one revision.
func (r *ChangeRepo) Revision(ctx context.Context, id uuid.UUID, revision int) (*change.Revision, error) {
	return scanRevision(r.pool.QueryRow(ctx, revSelect+` WHERE r.change_request_id = $1 AND r.revision = $2`, id, revision))
}

// Revisions lists all revisions, oldest first.
func (r *ChangeRepo) Revisions(ctx context.Context, id uuid.UUID) ([]change.Revision, error) {
	rows, err := r.pool.Query(ctx, revSelect+` WHERE r.change_request_id = $1 ORDER BY r.revision`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []change.Revision{}
	for rows.Next() {
		rev, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rev)
	}
	return out, rows.Err()
}

// lockCurrent locks the change request row and checks that expected is the
// current revision and the change request is not terminal.
func lockCurrent(ctx context.Context, tx pgx.Tx, id uuid.UUID, expected int) (change.State, error) {
	var (
		state change.State
		rev   int
	)
	err := tx.QueryRow(ctx, `SELECT state, revision FROM change_requests WHERE id = $1 FOR UPDATE`, id).Scan(&state, &rev)
	if noRows(err) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if state.Terminal() {
		return state, change.ErrTerminal
	}
	if rev != expected {
		return state, &change.RevisionConflict{Current: rev, State: state}
	}
	return state, nil
}

// Edit appends a new revision if expected is current and returns the change
// request to draft (its validation and simulations described the old
// content).
func (r *ChangeRepo) Edit(ctx context.Context, id uuid.UUID, expected int, rev *change.Revision, a *domain.AuditEvent) (*change.ChangeRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockCurrent(ctx, tx, id, expected); err != nil {
		return nil, err
	}
	rev.ChangeRequestID, rev.Revision = id, expected+1
	if err := insertRevision(ctx, tx, rev); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE change_requests SET revision = $2, state = $3, updated_at = $4 WHERE id = $1`,
		id, rev.Revision, change.StateDraft, rev.CreatedAt); err != nil {
		return nil, err
	}
	return r.finish(ctx, tx, id, a)
}

// Cancel moves the change request to cancelled if expected is current.
func (r *ChangeRepo) Cancel(ctx context.Context, id uuid.UUID, expected int, a *domain.AuditEvent) (*change.ChangeRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockCurrent(ctx, tx, id, expected); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE change_requests SET state = $2, updated_at = $3 WHERE id = $1`,
		id, change.StateCancelled, a.OccurredAt); err != nil {
		return nil, err
	}
	return r.finish(ctx, tx, id, a)
}

// RecordValidation stores a validation of the current revision. A valid
// result moves a draft to validated (a validated or simulated change request
// keeps its state); an invalid result moves it back to draft.
func (r *ChangeRepo) RecordValidation(ctx context.Context, v *change.Validation, a *domain.AuditEvent) (*change.ChangeRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := lockCurrent(ctx, tx, v.ChangeRequestID, v.Revision)
	if err != nil {
		return nil, err
	}
	var digest string
	if err := tx.QueryRow(ctx, `SELECT content_digest FROM change_request_revisions WHERE change_request_id = $1 AND revision = $2`,
		v.ChangeRequestID, v.Revision).Scan(&digest); err != nil {
		return nil, err
	}
	if digest != v.ContentDigest {
		return nil, fmt.Errorf("%w: validation is for content %s, revision %d has %s", domain.ErrConflict, v.ContentDigest, v.Revision, digest)
	}
	result, err := json.Marshal(v.Result)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO change_request_validations (id, change_request_id, revision, content_digest, environment_digest,
		valid, result, created_by, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		v.ID, v.ChangeRequestID, v.Revision, v.ContentDigest, v.EnvironmentDigest, v.Valid, result, v.CreatedBy, v.CreatedAt); err != nil {
		return nil, err
	}
	next := state
	switch {
	case !v.Valid:
		next = change.StateDraft
	case state == change.StateDraft:
		next = change.StateValidated
	}
	if _, err := tx.Exec(ctx, `UPDATE change_requests SET state = $2, updated_at = $3 WHERE id = $1`,
		v.ChangeRequestID, next, v.CreatedAt); err != nil {
		return nil, err
	}
	return r.finish(ctx, tx, v.ChangeRequestID, a)
}

// finish writes the audit record, commits, and returns the new current view.
func (r *ChangeRepo) finish(ctx context.Context, tx pgx.Tx, id uuid.UUID, a *domain.AuditEvent) (*change.ChangeRequest, error) {
	if err := appendAudit(ctx, tx, a); err != nil {
		return nil, err
	}
	cr, err := scanCR(tx.QueryRow(ctx, crSelect+` WHERE c.id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return cr, nil
}

// Validations lists a change request's validations, oldest first.
func (r *ChangeRepo) Validations(ctx context.Context, id uuid.UUID) ([]change.Validation, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, change_request_id, revision, content_digest, environment_digest, valid, result, created_by,
		created_at FROM change_request_validations WHERE change_request_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []change.Validation{}
	for rows.Next() {
		var (
			v      change.Validation
			result []byte
		)
		if err := rows.Scan(&v.ID, &v.ChangeRequestID, &v.Revision, &v.ContentDigest, &v.EnvironmentDigest, &v.Valid, &result,
			&v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(result, &v.Result); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
