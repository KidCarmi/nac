package radiusauth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// sessStore records the writes session() makes. Methods not used by
// session() are inherited from the nil Store and would panic if called.
type sessStore struct {
	Store
	prev       *domain.Session
	prevErr    error
	created    []domain.Session
	overrides  int
	currentErr error
	// winner, when set, is what CurrentSession returns after the first
	// CreateSession conflicts (a concurrent Reject inserted it first).
	winner    *domain.Session
	conflicts int
}

func (f *sessStore) CurrentSession(context.Context, uuid.UUID, string, string) (*domain.Session, error) {
	if f.winner != nil && f.conflicts > 0 {
		c := *f.winner
		return &c, nil
	}
	return nil, f.currentErr
}
func (f *sessStore) PreviousSession(context.Context, uuid.UUID, uuid.UUID, string, string, string, string) (*domain.Session, error) {
	if f.prevErr != nil {
		return nil, f.prevErr
	}
	if f.prev == nil {
		return nil, domain.ErrNotFound
	}
	return f.prev, nil
}
func (f *sessStore) CreateSession(_ context.Context, s *domain.Session) error {
	if f.winner != nil && f.conflicts == 0 {
		f.conflicts++
		return domain.ErrConflict
	}
	f.created = append(f.created, *s)
	return nil
}
func (f *sessStore) SetSessionOverride(context.Context, uuid.UUID, string, string, *time.Time) error {
	f.overrides++
	return nil
}
func (f *sessStore) InsertEvent(context.Context, *domain.AuthEvent) (bool, error) { return true, nil }

func newSessService(st *sessStore) *Service {
	return New(st, nil, nil, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var testReq = Request{NASPortID: "Ethernet1", CertIssuer: "/CN=CA", CertSerial: "ab"}

// Codex (pilot-rc4): the replacement session must be created WITH its
// inherited override in one write; a separate later write left a window in
// which the session existed with an identity but without the override.
func TestNewSessionIsCreatedWithInheritedOverride(t *testing.T) {
	at := time.Now()
	st := &sessStore{currentErr: domain.ErrNotFound,
		prev: &domain.Session{ID: uuid.New(), Override: domain.CoAQuarantine, OverrideBy: "op@x", OverrideAt: &at}}
	sess, prev, isNew, err := newSessService(st).session(context.Background(), &domain.Switch{ID: uuid.New()}, testReq, "00:50:00:00:02:00", "AB", at)
	if err != nil || !isNew || prev == nil {
		t.Fatalf("session: %v new=%v prev=%v", err, isNew, prev)
	}
	if len(st.created) != 1 || st.created[0].Override != domain.CoAQuarantine || st.created[0].OverrideBy != "op@x" {
		t.Fatalf("CreateSession did not carry the inherited override: %+v", st.created)
	}
	if st.overrides != 0 {
		t.Fatalf("override written separately after creation (%d extra writes)", st.overrides)
	}
	if sess.Override != domain.CoAQuarantine {
		t.Fatal("returned session lacks the override")
	}
}

func TestPredecessorLookupFailurePersistsNothing(t *testing.T) {
	st := &sessStore{currentErr: domain.ErrNotFound, prevErr: errors.New("db down")}
	if _, _, _, err := newSessService(st).session(context.Background(), &domain.Switch{ID: uuid.New()}, testReq, "00:50:00:00:02:00", "AB", time.Now()); err == nil {
		t.Fatal("lookup failure must fail the authentication (closed)")
	}
	if len(st.created) != 0 {
		t.Fatalf("a session with an identity but no inherited override was persisted: %+v", st.created)
	}
}

// Codex (pilot-rc5): a concurrent Reject inserts an identity-less session
// first; the valid exchange's insert conflicts. The winner must go through
// full resolution (identity + override inheritance), not be used as-is.
func TestInsertConflictResolvesWinnerWithInheritance(t *testing.T) {
	at := time.Now()
	st := &sessStore{currentErr: domain.ErrNotFound,
		winner: &domain.Session{ID: uuid.New(), State: domain.SessionRejected}, // identity-less, no override
		prev:   &domain.Session{ID: uuid.New(), Override: domain.CoAQuarantine, OverrideBy: "op@x", OverrideAt: &at}}
	sess, _, _, err := newSessService(st).session(context.Background(), &domain.Switch{ID: uuid.New()}, testReq, "00:50:00:00:02:00", "AB", at)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != st.winner.ID {
		t.Fatalf("expected the winning session, got %v", sess.ID)
	}
	if sess.Override != domain.CoAQuarantine || st.overrides != 1 {
		t.Fatalf("winner used without inheriting the quarantine: override=%q writes=%d", sess.Override, st.overrides)
	}
	if sess.CertSerial != "AB" || sess.CertIssuer != "/CN=CA" {
		t.Fatalf("winner did not acquire the validated identity: %q %q", sess.CertIssuer, sess.CertSerial)
	}
}
