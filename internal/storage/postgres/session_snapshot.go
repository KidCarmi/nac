package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kidcarmi/nac/internal/domain"
)

// Session reads for the API are taken from ONE database snapshot.
//
// Under READ COMMITTED (the PostgreSQL default, and what separate pool
// queries get) every statement sees the data committed when that statement
// started, so a session row read first and its CoA requests read a moment
// later can describe two different moments: the session before an operator
// action and the CoA after it, or last_coa from one read and coa_requests
// from another. A plain READ COMMITTED transaction does not fix that; a
// REPEATABLE READ transaction does: all its statements see the snapshot taken
// at its first statement.
//
// The transactions here are read-only, contain database reads only (no
// network or file I/O, no waiting on other work), and end before the caller
// builds its response. They take no row locks, so they never block writers
// and writers never block them.
//
// A snapshot is coherent, not fresh: it is what NAC had recorded at that
// instant. It says nothing new about the switch or the endpoint; freshness
// and unknown states are still reported by the caller from the recorded
// source and time.

// SessionSnapshot is one session and everything NAC recorded about it, read
// from a single snapshot.
type SessionSnapshot struct {
	Session   domain.Session
	Switch    *domain.Switch          // nil if the switch row is gone
	Endpoint  *domain.Endpoint        // nil if unbound or deleted
	Binding   *domain.IdentityBinding // nil if unbound or deleted
	Decisions []domain.DecisionRecord // oldest first
	CoAs      []domain.CoARequest     // oldest first (targeted or observed by this session)
	Events    []domain.AuthEvent      // newest SessionEventLimit, oldest first
}

// SessionListItem is one row of a session listing with the records the list
// view derives from, all from the same snapshot as the listing itself.
type SessionListItem struct {
	Session domain.Session
	Switch  *domain.Switch
	CoAs    []domain.CoARequest
	Events  []domain.AuthEvent
}

type snapshotHookKey struct{}

// WithSnapshotHook returns a context whose snapshot reads call fn(stage)
// inside the transaction, right after the statement that fixes the snapshot
// ("session" for SessionSnapshot, "sessions" for SessionListSnapshot).
//
// It exists for deterministic concurrency tests: a test commits a write from
// fn and then checks that the rest of the read does not see it. Nothing in
// nacd sets it; a request cannot, because only Go code holding this function
// can put the key on a context.
func WithSnapshotHook(ctx context.Context, fn func(stage string)) context.Context {
	return context.WithValue(ctx, snapshotHookKey{}, fn)
}

func snapshotHook(ctx context.Context, stage string) {
	if fn, ok := ctx.Value(snapshotHookKey{}).(func(string)); ok && fn != nil {
		fn(stage)
	}
}

// readSnapshot runs fn in a read-only REPEATABLE READ transaction.
func (r *PilotRepo) readSnapshot(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SessionSnapshot reads a session with its switch, endpoint, binding,
// decisions, CoA requests and timeline events from one snapshot.
func (r *PilotRepo) SessionSnapshot(ctx context.Context, id uuid.UUID) (*SessionSnapshot, error) {
	var out SessionSnapshot
	err := r.readSnapshot(ctx, func(tx pgx.Tx) error {
		sess, err := getSession(ctx, tx, id) // first statement: fixes the snapshot
		if err != nil {
			return err
		}
		out.Session = *sess
		snapshotHook(ctx, "session")
		if out.Switch, err = r.optional(r.switchIn(ctx, tx, sess.SwitchID)); err != nil {
			return err
		}
		if sess.EndpointID != nil {
			e, err := scanEndpoint(tx.QueryRow(ctx, `SELECT `+endpointColumns+` FROM endpoints WHERE id = $1`, *sess.EndpointID))
			if err != nil && !noRows(err) {
				return err
			}
			out.Endpoint = e
		}
		if sess.BindingID != nil {
			b, err := scanBinding(tx.QueryRow(ctx, `SELECT `+bindingColumns+` FROM identity_bindings WHERE id=$1`, *sess.BindingID))
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			out.Binding = b
		}
		if out.Decisions, err = sessionDecisions(ctx, tx, sess.ID); err != nil {
			return err
		}
		if out.CoAs, err = sessionCoAs(ctx, tx, sess.ID); err != nil {
			return err
		}
		out.Events, err = sessionEvents(ctx, tx, sess)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SessionListSnapshot lists sessions (as ListSessions) and reads, for each,
// its switch, CoA requests and events from the same snapshot.
func (r *PilotRepo) SessionListSnapshot(ctx context.Context, f SessionFilter) ([]SessionListItem, error) {
	var out []SessionListItem
	err := r.readSnapshot(ctx, func(tx pgx.Tx) error {
		sessions, err := listSessions(ctx, tx, f) // first statement: fixes the snapshot
		if err != nil {
			return err
		}
		snapshotHook(ctx, "sessions")
		switches := map[uuid.UUID]*domain.Switch{}
		out = make([]SessionListItem, 0, len(sessions))
		for i := range sessions {
			sess := &sessions[i]
			sw, seen := switches[sess.SwitchID]
			if !seen {
				if sw, err = r.optional(r.switchIn(ctx, tx, sess.SwitchID)); err != nil {
					return err
				}
				switches[sess.SwitchID] = sw
			}
			item := SessionListItem{Session: *sess, Switch: sw}
			if item.CoAs, err = sessionCoAs(ctx, tx, sess.ID); err != nil {
				return err
			}
			if item.Events, err = sessionEvents(ctx, tx, sess); err != nil {
				return err
			}
			out = append(out, item)
		}
		return nil
	})
	return out, err
}

func (r *PilotRepo) switchIn(ctx context.Context, q querier, id uuid.UUID) (*domain.Switch, error) {
	s, err := r.scanSwitch(q.QueryRow(ctx, `SELECT `+switchColumns+` FROM switches WHERE id = $1`, id))
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	return s, err
}

// optional turns "not found" into nil without an error.
func (r *PilotRepo) optional(sw *domain.Switch, err error) (*domain.Switch, error) {
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	return sw, err
}
