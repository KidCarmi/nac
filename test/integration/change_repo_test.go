//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/change"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// Change-request repository (slice A, milestone 1): revisioned, review-only
// change requests whose every mutation commits together with its audit
// record.

type crRig struct {
	pool      *pgxpool.Pool
	repo      *postgres.ChangeRepo
	versionID uuid.UUID
	digest    string
}

func newCRRig(t *testing.T) *crRig {
	t.Helper()
	resetDB(t, dsn(t))
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, config.DBConfig{DSN: dsn(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	r := &crRig{pool: pool, repo: postgres.NewChangeRepo(pool), versionID: uuid.New(),
		digest: policyver.Digest(nil, domain.ActionDeny)}
	if _, err := pool.Exec(ctx, `INSERT INTO policy_versions (id, number, rules, default_action, digest) VALUES ($1, 1, '[]', 'deny', $2)`,
		r.versionID, r.digest); err != nil {
		t.Fatal(err)
	}
	return r
}

func crContent(segment string) change.Content {
	return change.Content{DefaultAction: domain.ActionDeny, Rules: []domain.Policy{{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(segment)), Name: "corp", Priority: 10,
		Enabled: true, Action: domain.ActionAllow, Segment: segment,
		Conditions: []domain.Condition{{Field: "identity.bound", Operator: domain.OpEquals, Values: []string{"true"}}}}}}
}

func (r *crRig) revision(id uuid.UUID, c change.Content) *change.Revision {
	return &change.Revision{ChangeRequestID: id, Revision: 1, Content: c, ContentDigest: c.Digest(), BaseVersionID: r.versionID,
		BaseVersionDigest: r.digest, Author: adminEmail, CreatedAt: time.Now().UTC()}
}

func auditEvent(action string, id uuid.UUID) *domain.AuditEvent {
	return &domain.AuditEvent{ID: uuid.New(), OccurredAt: time.Now().UTC(), ActorEmail: adminEmail, Action: action,
		ResourceType: "change_request", ResourceID: id.String(), Outcome: "success"}
}

func (r *crRig) create(t *testing.T) *change.ChangeRequest {
	t.Helper()
	now := time.Now().UTC()
	cr := &change.ChangeRequest{ID: uuid.New(), Title: "move bound devices to corp", Workflow: change.WorkflowReviewOnly,
		State: change.StateDraft, Revision: 1, RequestedBy: adminEmail, CreatedAt: now, UpdatedAt: now}
	if err := r.repo.Create(context.Background(), cr, r.revision(cr.ID, crContent("corp")), auditEvent("change.create", cr.ID)); err != nil {
		t.Fatal(err)
	}
	return cr
}

func (r *crRig) auditCount(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := r.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE resource_type='change_request' AND resource_id=$1`,
		id.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestChangeRepo_CreateGetRevisions(t *testing.T) {
	r := newCRRig(t)
	ctx := context.Background()
	cr := r.create(t)
	got, err := r.repo.Get(ctx, cr.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := crContent("corp").Digest()
	if got.State != change.StateDraft || got.Revision != 1 || got.Workflow != change.WorkflowReviewOnly || got.ContentDigest != want ||
		got.BaseVersionID != r.versionID || got.BaseVersionNumber != 1 || got.BaseVersionDigest != r.digest {
		t.Fatalf("change request: %+v", got)
	}
	rev, err := r.repo.Revision(ctx, cr.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Content.Digest() != want || rev.ContentDigest != want || len(rev.Content.Rules) != 1 || rev.Content.Rules[0].Segment != "corp" {
		t.Fatalf("revision content did not round-trip: %+v", rev)
	}
	if n := r.auditCount(t, cr.ID); n != 1 {
		t.Fatalf("%d audit records for a create, want 1", n)
	}
	if list, err := r.repo.List(ctx, change.ListFilter{State: change.StateDraft}); err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
}

func TestChangeRepo_RevisionConflict(t *testing.T) {
	r := newCRRig(t)
	ctx := context.Background()
	cr := r.create(t)
	c2 := crContent("guest")
	got, err := r.repo.Edit(ctx, cr.ID, 1, r.revision(cr.ID, c2), auditEvent("change.edit", cr.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.ContentDigest != c2.Digest() || got.State != change.StateDraft {
		t.Fatalf("after edit: %+v", got)
	}
	_, err = r.repo.Edit(ctx, cr.ID, 1, r.revision(cr.ID, crContent("other")), auditEvent("change.edit", cr.ID))
	var rc *change.RevisionConflict
	if !errors.As(err, &rc) || rc.Current != 2 || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	if revs, _ := r.repo.Revisions(ctx, cr.ID); len(revs) != 2 {
		t.Fatalf("%d revisions after a refused edit", len(revs))
	}
	if n := r.auditCount(t, cr.ID); n != 2 {
		t.Fatalf("%d audit records, want 2 (create, edit): a refused edit must not be recorded as a mutation", n)
	}
}

func TestChangeRepo_ConcurrentMutationsOneWinner(t *testing.T) {
	r := newCRRig(t)
	ctx := context.Background()
	cr := r.create(t)
	const n = 10
	var (
		wg         sync.WaitGroup
		gate       = make(chan struct{})
		wins, lost atomic.Int32
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			var err error
			switch i % 3 {
			case 0:
				_, err = r.repo.Cancel(ctx, cr.ID, 1, auditEvent("change.cancel", cr.ID))
			case 1:
				_, err = r.repo.Edit(ctx, cr.ID, 1, r.revision(cr.ID, crContent("guest")), auditEvent("change.edit", cr.ID))
			default:
				c := crContent("corp")
				_, err = r.repo.RecordValidation(ctx, &change.Validation{ID: uuid.New(), ChangeRequestID: cr.ID, Revision: 1,
					ContentDigest: c.Digest(), EnvironmentDigest: "env", Valid: true, CreatedBy: adminEmail, CreatedAt: time.Now().UTC()},
					auditEvent("change.validate", cr.ID))
			}
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, domain.ErrConflict):
				lost.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	close(gate)
	wg.Wait()
	// Validation does not change the revision, so a validation can win
	// together with one cancel or one edit that follows it; edits and
	// cancels exclude each other.
	got, _ := r.repo.Get(ctx, cr.ID)
	revs, _ := r.repo.Revisions(ctx, cr.ID)
	vals, _ := r.repo.Validations(ctx, cr.ID)
	edits := len(revs) - 1
	cancels := 0
	if got.State == change.StateCancelled {
		cancels = 1
	}
	if edits+cancels > 1 || int(wins.Load()) != edits+cancels+len(vals) || int(wins.Load()+lost.Load()) != n {
		t.Fatalf("wins %d (edits %d, cancels %d, validations %d), conflicts %d", wins.Load(), edits, cancels, len(vals), lost.Load())
	}
	if a := r.auditCount(t, cr.ID); a != 1+int(wins.Load()) {
		t.Fatalf("%d audit records for 1 create + %d committed mutations", a, wins.Load())
	}
}

func TestChangeRepo_CancelledIsTerminal(t *testing.T) {
	r := newCRRig(t)
	ctx := context.Background()
	cr := r.create(t)
	if _, err := r.repo.Cancel(ctx, cr.ID, 1, auditEvent("change.cancel", cr.ID)); err != nil {
		t.Fatal(err)
	}
	c := crContent("corp")
	for name, err := range map[string]error{
		"edit": func() error {
			_, err := r.repo.Edit(ctx, cr.ID, 1, r.revision(cr.ID, crContent("guest")), auditEvent("change.edit", cr.ID))
			return err
		}(),
		"cancel": func() error { _, err := r.repo.Cancel(ctx, cr.ID, 1, auditEvent("change.cancel", cr.ID)); return err }(),
		"validate": func() error {
			_, err := r.repo.RecordValidation(ctx, &change.Validation{ID: uuid.New(), ChangeRequestID: cr.ID, Revision: 1,
				ContentDigest: c.Digest(), EnvironmentDigest: "env", Valid: true, CreatedBy: adminEmail, CreatedAt: time.Now().UTC()},
				auditEvent("change.validate", cr.ID))
			return err
		}(),
	} {
		if !errors.Is(err, change.ErrTerminal) {
			t.Fatalf("%s on a cancelled change request: %v", name, err)
		}
	}
}

func TestChangeRepo_ValidationTransitions(t *testing.T) {
	r := newCRRig(t)
	ctx := context.Background()
	cr := r.create(t)
	c := crContent("corp")
	val := func(rev int, digest string, valid bool) (*change.ChangeRequest, error) {
		return r.repo.RecordValidation(ctx, &change.Validation{ID: uuid.New(), ChangeRequestID: cr.ID, Revision: rev, ContentDigest: digest,
			EnvironmentDigest: "env-1", Valid: valid, Result: policyver.Validation{Valid: valid, Errors: []string{}, Warnings: []string{}},
			CreatedBy: adminEmail, CreatedAt: time.Now().UTC()}, auditEvent("change.validate", cr.ID))
	}
	got, err := val(1, c.Digest(), true)
	if err != nil || got.State != change.StateValidated {
		t.Fatalf("valid validation of a draft: %v %+v", err, got)
	}
	if _, err := val(1, "not-the-content", true); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("validation for other content: %v", err)
	}
	if _, err := val(2, c.Digest(), true); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("validation of a revision that is not current: %v", err)
	}
	if got, err = val(1, c.Digest(), false); err != nil || got.State != change.StateDraft {
		t.Fatalf("invalid validation: %v %+v", err, got)
	}
	if vals, _ := r.repo.Validations(ctx, cr.ID); len(vals) != 2 || vals[0].EnvironmentDigest != "env-1" || !vals[0].Result.Valid {
		t.Fatalf("validations: %+v", vals)
	}
}

// A mutation whose audit record cannot be written is not committed. The
// failure is a real database error (duplicate audit id), not a mock.
func TestChangeRepo_AuditFailureRollsBackMutation(t *testing.T) {
	r := newCRRig(t)
	ctx := context.Background()
	taken := auditEvent("unrelated", uuid.New())
	if _, err := r.pool.Exec(ctx, `INSERT INTO audit_events (id, action, resource_type, outcome) VALUES ($1, 'x', 'x', 'success')`, taken.ID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	cr := &change.ChangeRequest{ID: uuid.New(), Title: "t", Workflow: change.WorkflowReviewOnly, State: change.StateDraft, Revision: 1,
		RequestedBy: adminEmail, CreatedAt: now, UpdatedAt: now}
	if err := r.repo.Create(ctx, cr, r.revision(cr.ID, crContent("corp")), taken); err == nil {
		t.Fatal("create committed without its audit record")
	}
	if _, err := r.repo.Get(ctx, cr.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("change request exists after a failed audit write: %v", err)
	}
	if err := r.repo.Create(ctx, cr, r.revision(cr.ID, crContent("corp")), nil); err == nil {
		t.Fatal("create committed with no audit record")
	}

	cr = r.create(t)
	if _, err := r.repo.Edit(ctx, cr.ID, 1, r.revision(cr.ID, crContent("guest")), taken); err == nil {
		t.Fatal("edit committed without its audit record")
	}
	if _, err := r.repo.Cancel(ctx, cr.ID, 1, taken); err == nil {
		t.Fatal("cancel committed without its audit record")
	}
	got, _ := r.repo.Get(ctx, cr.ID)
	revs, _ := r.repo.Revisions(ctx, cr.ID)
	if got.Revision != 1 || got.State != change.StateDraft || len(revs) != 1 {
		t.Fatalf("a mutation survived its failed audit write: %+v, %d revisions", got, len(revs))
	}
}

// History cannot be rewritten even by SQL that bypasses the repository.
func TestChangeRepo_HistoryIsImmutable(t *testing.T) {
	r := newCRRig(t)
	ctx := context.Background()
	cr := r.create(t)
	c := crContent("corp")
	if _, err := r.repo.RecordValidation(ctx, &change.Validation{ID: uuid.New(), ChangeRequestID: cr.ID, Revision: 1, ContentDigest: c.Digest(),
		EnvironmentDigest: "env", Valid: true, CreatedBy: adminEmail, CreatedAt: time.Now().UTC()}, auditEvent("change.validate", cr.ID)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE change_request_revisions SET content_digest = 'x' WHERE change_request_id = $1`,
		`DELETE FROM change_request_revisions WHERE change_request_id = $1`,
		`UPDATE change_request_validations SET valid = false WHERE change_request_id = $1`,
		`DELETE FROM change_request_validations WHERE change_request_id = $1`,
		`DELETE FROM change_requests WHERE id = $1`,
		`UPDATE change_requests SET state = 'approved' WHERE id = $1`,
		`UPDATE change_requests SET workflow = 'mandatory' WHERE id = $1`,
	} {
		if _, err := r.pool.Exec(ctx, q, cr.ID); err == nil {
			t.Fatalf("allowed: %s", q)
		}
	}
}
