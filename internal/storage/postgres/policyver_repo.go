package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
)

// PolicyVersionRepo implements policyver.Store.
type PolicyVersionRepo struct{ pool *pgxpool.Pool }

// NewPolicyVersionRepo constructs the repo.
func NewPolicyVersionRepo(pool *pgxpool.Pool) *PolicyVersionRepo {
	return &PolicyVersionRepo{pool: pool}
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readWorkspace(ctx context.Context, q querier) ([]domain.Policy, domain.PolicyAction, error) {
	rows, err := q.Query(ctx, `SELECT `+policyColumns+` FROM policies ORDER BY priority ASC, name ASC`)
	if err != nil {
		return nil, "", err
	}
	ws, err := collectPolicies(rows)
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	var def *string
	if err := q.QueryRow(ctx, `SELECT default_action FROM policy_draft_settings WHERE singleton`).Scan(&def); err != nil && !noRows(err) {
		return nil, "", err
	}
	var a domain.PolicyAction
	if def != nil {
		a = domain.PolicyAction(*def)
	}
	if ws == nil {
		ws = []domain.Policy{}
	}
	return ws, a, nil
}

// Workspace returns the draft rules and draft default action ("" = config default).
func (r *PolicyVersionRepo) Workspace(ctx context.Context) ([]domain.Policy, domain.PolicyAction, error) {
	return readWorkspace(ctx, r.pool)
}

// SetDraftDefaultAction stores the draft default action.
func (r *PolicyVersionRepo) SetDraftDefaultAction(ctx context.Context, a domain.PolicyAction) error {
	_, err := r.pool.Exec(ctx, `UPDATE policy_draft_settings SET default_action = $1 WHERE singleton`, string(a))
	return err
}

const versionColumns = `v.id, v.number, v.rules, v.default_action, v.digest, v.note, v.created_by, v.created_at`

func scanVersion(row rowScanner) (*domain.PolicyVersion, error) {
	var (
		v     domain.PolicyVersion
		rules []byte
	)
	if err := row.Scan(&v.ID, &v.Number, &rules, &v.DefaultAction, &v.Digest, &v.Note, &v.CreatedBy, &v.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rules, &v.Rules); err != nil {
		return nil, fmt.Errorf("unmarshal version rules: %w", err)
	}
	return &v, nil
}

const activeVersionSQL = `
	SELECT ` + versionColumns + ` FROM policy_activations a
	JOIN policy_versions v ON v.id = a.version_id
	ORDER BY a.id DESC LIMIT 1`

func activeVersion(ctx context.Context, q querier) (*domain.PolicyVersion, error) {
	v, err := scanVersion(q.QueryRow(ctx, activeVersionSQL))
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Active = true
	return v, nil
}

// ActiveVersion returns the currently active version.
func (r *PolicyVersionRepo) ActiveVersion(ctx context.Context) (*domain.PolicyVersion, error) {
	return activeVersion(ctx, r.pool)
}

// GetVersion returns a version by number.
func (r *PolicyVersionRepo) GetVersion(ctx context.Context, number int) (*domain.PolicyVersion, error) {
	v, err := scanVersion(r.pool.QueryRow(ctx, `SELECT `+versionColumns+` FROM policy_versions v WHERE v.number = $1`, number))
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a, err := activeVersion(ctx, r.pool); err == nil && a.ID == v.ID {
		v.Active = true
	}
	return v, nil
}

// ListVersions lists versions newest first.
func (r *PolicyVersionRepo) ListVersions(ctx context.Context) ([]domain.PolicyVersion, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+versionColumns+` FROM policy_versions v ORDER BY v.number DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PolicyVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if a, err := activeVersion(ctx, r.pool); err == nil {
		for i := range out {
			out[i].Active = out[i].ID == a.ID
		}
	}
	return out, nil
}

// ListActivations lists activations newest first.
func (r *PolicyVersionRepo) ListActivations(ctx context.Context, limit int) ([]domain.PolicyActivation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.version_id, v.number, a.kind, a.previous_version_id, pv.number, a.activated_by, a.activated_at, a.note
		FROM policy_activations a
		JOIN policy_versions v ON v.id = a.version_id
		LEFT JOIN policy_versions pv ON pv.id = a.previous_version_id
		ORDER BY a.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PolicyActivation{}
	for rows.Next() {
		var a domain.PolicyActivation
		if err := rows.Scan(&a.ID, &a.VersionID, &a.VersionNumber, &a.Kind, &a.PreviousVersionID, &a.PreviousVersionNumber, &a.ActivatedBy, &a.ActivatedAt, &a.Note); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// lockForActivation serialises publish/restore and blocks draft edits for the
// duration of the transaction.
func lockForActivation(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `LOCK TABLE policies, policy_draft_settings, policy_activations IN SHARE ROW EXCLUSIVE MODE`)
	return err
}

func insertActivation(ctx context.Context, tx pgx.Tx, a *domain.PolicyActivation, versionID uuid.UUID) error {
	at := a.ActivatedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return tx.QueryRow(ctx, `
		INSERT INTO policy_activations (version_id, kind, previous_version_id, activated_by, activated_at, note)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		versionID, string(a.Kind), a.PreviousVersionID, a.ActivatedBy, at, a.Note).Scan(&a.ID)
}

// PublishWorkspace snapshots the workspace into a new active version.
// txView loads, on the activation transaction itself, everything the
// prepare callback may need, so the callback never takes a second pooled
// connection while this transaction holds one and the activation lock.
func txView(ctx context.Context, tx pgx.Tx) (policyver.TxView, error) {
	var v policyver.TxView
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM policy_versions`).Scan(&v.NextNumber); err != nil {
		return v, err
	}
	v.Segments, v.SegmentsErr = segmentMappings(ctx, tx)
	return v, nil
}

func (r *PolicyVersionRepo) PublishWorkspace(ctx context.Context, prepare func(ws []domain.Policy, def domain.PolicyAction, active *domain.PolicyVersion, view policyver.TxView) (*domain.PolicyVersion, *domain.PolicyActivation, error)) (*domain.PolicyVersion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockForActivation(ctx, tx); err != nil {
		return nil, err
	}
	ws, def, err := readWorkspace(ctx, tx)
	if err != nil {
		return nil, err
	}
	active, err := activeVersion(ctx, tx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	view, err := txView(ctx, tx)
	if err != nil {
		return nil, err
	}
	v, act, err := prepare(ws, def, active, view)
	if err != nil {
		return nil, err
	}
	rules, err := json.Marshal(v.Rules)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO policy_versions (id, number, rules, default_action, digest, note, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		v.ID, v.Number, rules, string(v.DefaultAction), v.Digest, v.Note, v.CreatedBy, v.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: version %d already exists", domain.ErrConflict, v.Number)
		}
		return nil, err
	}
	if err := insertActivation(ctx, tx, act, v.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	v.Active = true
	return v, nil
}

// Activate re-activates an existing version, optionally resetting the draft.
func (r *PolicyVersionRepo) Activate(ctx context.Context, number int, prepare func(target, active *domain.PolicyVersion, view policyver.TxView) (*domain.PolicyActivation, error), resetWorkspace bool) (*domain.PolicyActivation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockForActivation(ctx, tx); err != nil {
		return nil, err
	}
	target, err := scanVersion(tx.QueryRow(ctx, `SELECT `+versionColumns+` FROM policy_versions v WHERE v.number = $1`, number))
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	active, err := activeVersion(ctx, tx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	view, err := txView(ctx, tx)
	if err != nil {
		return nil, err
	}
	act, err := prepare(target, active, view)
	if err != nil {
		return nil, err
	}
	if err := insertActivation(ctx, tx, act, target.ID); err != nil {
		return nil, err
	}
	if resetWorkspace {
		if _, err := tx.Exec(ctx, `DELETE FROM policies`); err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		for _, p := range target.Rules {
			conds, err := marshalConditions(p.Conditions)
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO policies (`+policyColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				p.ID, p.Name, p.Description, p.Priority, p.Enabled, conds, p.Action, p.Segment, now, now); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE policy_draft_settings SET default_action = $1 WHERE singleton`, string(target.DefaultAction)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	act.VersionID, act.VersionNumber = target.ID, target.Number
	if active != nil {
		n := active.Number
		act.PreviousVersionNumber = &n
	}
	return act, nil
}
