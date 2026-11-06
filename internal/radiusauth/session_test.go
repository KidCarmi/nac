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
}

func (f *sessStore) CurrentSession(context.Context, uuid.UUID, string, string) (*domain.Session, error) {
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
