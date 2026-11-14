package pilot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// Store is the persistence the pilot workflows need (implemented by
// postgres.PilotRepo).
type Store interface {
	GetSwitch(ctx context.Context, id uuid.UUID) (*domain.Switch, error)
	ListSwitches(ctx context.Context) ([]domain.Switch, error)
	SaveSwitch(ctx context.Context, s *domain.Switch) error
	DeleteSwitch(ctx context.Context, id uuid.UUID) error
	RecordProbe(ctx context.Context, id uuid.UUID, at time.Time, ok bool, detail string) error
	RecordVerification(ctx context.Context, id uuid.UUID, at time.Time, sessionID uuid.UUID, detail string) error

	BindingByCert(ctx context.Context, issuer, serial string) (*domain.IdentityBinding, error)
	GetBinding(ctx context.Context, id uuid.UUID) (*domain.IdentityBinding, error)
	ListBindings(ctx context.Context, endpointID *uuid.UUID) ([]domain.IdentityBinding, error)
	CreateBinding(ctx context.Context, b *domain.IdentityBinding) error
	DeleteBinding(ctx context.Context, id uuid.UUID) error
	GetEndpoint(ctx context.Context, id uuid.UUID) (*domain.Endpoint, error)

	GetSession(ctx context.Context, id uuid.UUID) (*domain.Session, error)
	// SessionSnapshot and SessionListSnapshot read everything a session
	// view is built from in one read-only REPEATABLE READ transaction.
	SessionSnapshot(ctx context.Context, id uuid.UUID) (*postgres.SessionSnapshot, error)
	SessionListSnapshot(ctx context.Context, f postgres.SessionFilter) ([]postgres.SessionListItem, error)
	SetSessionOverride(ctx context.Context, id uuid.UUID, override, by string, at *time.Time) error
	UpdateSessionObservation(ctx context.Context, id uuid.UUID, status, vlan string, at time.Time, source, detail string) error
	ListSessions(ctx context.Context, f postgres.SessionFilter) ([]domain.Session, error)
	ActiveSessionsOnSwitch(ctx context.Context, switchID uuid.UUID) ([]domain.Session, error)
	GetDecision(ctx context.Context, id uuid.UUID) (*domain.DecisionRecord, error)
	SessionDecisions(ctx context.Context, sessionID uuid.UUID) ([]domain.DecisionRecord, error)
	SessionEvents(ctx context.Context, s *domain.Session) ([]domain.AuthEvent, error)
	ListEvents(ctx context.Context, f postgres.EventFilter) ([]domain.AuthEvent, error)
	LastEvent(ctx context.Context, stage string) (*domain.AuthEvent, error)
	InsertEvent(ctx context.Context, e *domain.AuthEvent) (bool, error)

	InsertCoA(ctx context.Context, c *domain.CoARequest) error
	UpdateCoA(ctx context.Context, c *domain.CoARequest) error
	GetCoA(ctx context.Context, id uuid.UUID) (*domain.CoARequest, error)
	SessionCoAs(ctx context.Context, sessionID uuid.UUID) ([]domain.CoARequest, error)
	FailInterruptedCoAs(ctx context.Context, at time.Time) (int64, error)
}

// Auditor records operator actions (audit.Recorder satisfies it).
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID, outcome string, meta map[string]string)
}

// Config tunes the workflows.
type Config struct {
	ClientsFile        string        // NAC_FREERADIUS_CLIENTS_FILE
	OutageLog          string        // NAC_FREERADIUS_OUTAGE_LOG
	ReloadStatusFile   string        // NAC_FREERADIUS_RELOAD_STATUS (default: freeradius-reload.status next to ClientsFile)
	SkipProcessCheck   bool          // NAC_FREERADIUS_PROCESS_CHECK=off: FreeRADIUS runs on another host
	ObservationMaxAge  time.Duration // observations older than this are stale
	ObservationEvery   time.Duration // eAPI polling interval
	CoATimeout         time.Duration // per attempt
	CoAAttempts        int           // total transmissions per CoA
	CoAObserveWindow   time.Duration // how long to wait for the re-authentication
	VerifyWindow       time.Duration // a verification needs an accept this recent
	EAPIInsecureTLS    bool          // NAC_EAPI_INSECURE_SKIP_VERIFY
	EAPIAllowRSAKex    bool          // NAC_EAPI_TLS_ALLOW_RSA_KEX
	OutageImportPeriod time.Duration
}

// Service implements the pilot workflows.
type Service struct {
	store   Store
	rules   *policyver.Service
	eval    *access.Service
	auditor Auditor
	logger  *slog.Logger
	cfg     Config
	now     func() time.Time
	sendCoA coaSender
	eapi    eapiClient
	// frRunning reports whether FreeRADIUS is running (procHasFreeRADIUS).
	frRunning func() (bool, error)

	mu sync.Mutex
	// clientsMu serialises reading the switch list and replacing the
	// FreeRADIUS client file, so an older snapshot can never overwrite a
	// newer one when switch changes run concurrently.
	clientsMu         sync.Mutex
	clientsFileStatus StatusItem
	// desiredSHA/desiredSince: the switch list nacd last wrote and when that
	// CONTENT first appeared (not the last identical rewrite), so the reload
	// grace period is not reset by the backstop's re-triggers. Under mu.
	desiredSHA   string
	desiredSince time.Time
	// lastRetrigger/retriggerWarned: reload backstop state, under clientsMu.
	lastRetrigger   time.Time
	retriggerWarned string
	outageStatus    StatusItem
	wg              sync.WaitGroup
}

// New constructs the service with defaults filled in.
func New(store Store, rules *policyver.Service, eval *access.Service, auditor Auditor, logger *slog.Logger, cfg Config) *Service {
	if cfg.ObservationMaxAge <= 0 {
		cfg.ObservationMaxAge = 5 * time.Minute
	}
	if cfg.ObservationEvery <= 0 {
		cfg.ObservationEvery = 30 * time.Second
	}
	if cfg.CoATimeout <= 0 {
		cfg.CoATimeout = 3 * time.Second
	}
	if cfg.CoAAttempts <= 0 {
		cfg.CoAAttempts = 3
	}
	if cfg.CoAObserveWindow <= 0 {
		cfg.CoAObserveWindow = 60 * time.Second
	}
	if cfg.VerifyWindow <= 0 {
		cfg.VerifyWindow = 15 * time.Minute
	}
	if cfg.OutageImportPeriod <= 0 {
		cfg.OutageImportPeriod = 30 * time.Second
	}
	s := &Service{store: store, rules: rules, eval: eval, auditor: auditor, logger: logger, cfg: cfg, now: time.Now}
	s.sendCoA = s.exchangeCoA
	s.frRunning = procHasFreeRADIUS
	s.eapi = newEAPIClient(EAPITLS{InsecureSkipVerify: cfg.EAPIInsecureTLS, AllowRSAKeyExchange: cfg.EAPIAllowRSAKex})
	return s
}

func (s *Service) audit(ctx context.Context, action, rtype, id, outcome string, meta map[string]string) {
	if s.auditor != nil {
		s.auditor.Record(ctx, action, rtype, id, outcome, meta)
	}
}

func (s *Service) event(ctx context.Context, e *domain.AuthEvent) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = s.now().UTC()
	}
	if _, err := s.store.InsertEvent(ctx, e); err != nil {
		s.logger.Error("timeline event not recorded", "error", err)
	}
}

// Start recovers interrupted state and launches background workers.
func (s *Service) Start(ctx context.Context) {
	if n, err := s.store.FailInterruptedCoAs(ctx, s.now().UTC()); err != nil {
		s.logger.Error("could not recover interrupted CoA requests", "error", err)
	} else if n > 0 {
		s.logger.Warn("marked CoA requests interrupted by restart as failed", "count", n)
	}
	s.NormalizeStoredIPs(ctx)
	s.WriteClientsFile(ctx)
	if s.cfg.ClientsFile != "" {
		go s.loop(ctx, reloadBackstopEvery, s.ReloadBackstop)
	}
	go s.loop(ctx, s.cfg.ObservationEvery, func(ctx context.Context) { s.ObserveAll(ctx) })
	go s.loop(ctx, s.cfg.OutageImportPeriod, func(ctx context.Context) { s.ImportOutageLog(ctx) })
}

func (s *Service) loop(ctx context.Context, every time.Duration, f func(context.Context)) {
	f(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f(ctx)
		}
	}
}

// Wait blocks until in-flight CoA exchanges finish (tests, shutdown).
func (s *Service) Wait() { s.wg.Wait() }
