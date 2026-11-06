// Package app is the composition root shared by cmd/nacd and the end-to-end
// tests: it connects infrastructure, applies migrations, bootstraps the first
// administrator and wires every service into the HTTP server. Keeping it out
// of package main lets tests start a real server against a real database.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/cache"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/endpoint"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/internal/policy"
	"github.com/kidcarmi/nac/internal/policyver"
	"github.com/kidcarmi/nac/internal/server"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// App owns the process-wide resources.
type App struct {
	cfg     *config.Config
	logger  *slog.Logger
	pool    *pgxpool.Pool
	closers []func()
	handler http.Handler
	bg      []func(ctx context.Context)
}

// New connects to the database, migrates, bootstraps and wires the server.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*App, error) {
	pool, err := postgres.Connect(ctx, cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("database: %w (check NAC_DB_DSN and that PostgreSQL is running)", err)
	}
	a := &App{cfg: cfg, logger: logger, pool: pool, closers: []func(){pool.Close}}

	if err := postgres.Migrate(ctx, pool); err != nil {
		a.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	var policyCache domain.PolicyCache = cache.NoopCache{}
	if cfg.Redis.Addr != "" {
		client, err := cache.Connect(ctx, cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
		if err != nil {
			// Redis is an optimisation only; run without it rather than fail.
			logger.Warn("redis unavailable; continuing without policy cache", "error", err)
		} else {
			a.closers = append(a.closers, func() { _ = client.Close() })
			policyCache = cache.NewRedisCache(client, cfg.Redis.TTL)
		}
	}

	endpointRepo := postgres.NewEndpointRepo(pool)
	policyRepo := postgres.NewPolicyRepo(pool)
	operatorRepo := postgres.NewOperatorRepo(pool)
	auditRepo := postgres.NewAuditRepo(pool)

	if err := bootstrapAdmin(ctx, cfg, operatorRepo, logger); err != nil {
		a.Close()
		return nil, err
	}

	tokens, err := auth.NewTokenIssuer(cfg.Auth.JWTSigningKey, cfg.Auth.JWTIssuer, cfg.Auth.JWTAudience, cfg.Auth.JWTTTL)
	if err != nil {
		a.Close()
		return nil, err
	}

	engine := access.NewEngine()
	engine.DefaultAction = cfg.DefaultAction
	policySvc := policy.NewService(policyRepo, policyCache)
	endpointSvc := endpoint.NewService(endpointRepo)
	versionSvc := policyver.New(postgres.NewPolicyVersionRepo(pool), nil, nil, cfg.DefaultAction)
	if err := versionSvc.EnsureInitialVersion(ctx); err != nil {
		a.Close()
		return nil, fmt.Errorf("initial policy version: %w", err)
	}

	srv := server.New(server.Deps{
		Config:    cfg,
		Logger:    logger,
		Metrics:   observability.NewMetrics(),
		Tokens:    tokens,
		Endpoints: endpointSvc,
		Policies:  policySvc,
		// Decisions always evaluate the ACTIVE published version; the
		// /policies CRUD edits the draft.
		Access:    access.NewService(engine, versionSvc, endpointRepo),
		Versions:  versionSvc,
		Operators: operatorRepo,
		AuditRepo: auditRepo,
		Audit:     audit.NewRecorder(auditRepo),
		Ready:     func(ctx context.Context) error { return pool.Ping(ctx) },
	})
	a.handler = srv.Handler()
	return a, nil
}

// Handler is the fully wired HTTP handler.
func (a *App) Handler() http.Handler { return a.handler }

// StartBackground launches background workers bound to ctx.
func (a *App) StartBackground(ctx context.Context) {
	for _, f := range a.bg {
		go f(ctx)
	}
}

// Close releases resources in reverse order of acquisition.
func (a *App) Close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
	a.closers = nil
}

// bootstrapAdmin creates the first administrator when configured. It is
// idempotent: an existing account with that email is left untouched.
func bootstrapAdmin(ctx context.Context, cfg *config.Config, repo domain.OperatorRepository, logger *slog.Logger) error {
	email := strings.TrimSpace(strings.ToLower(cfg.Bootstrap.AdminEmail))
	if email == "" {
		return nil
	}
	if _, err := repo.GetByEmail(ctx, email); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("bootstrap lookup: %w", err)
	}
	if len(cfg.Bootstrap.AdminPassword) < 12 {
		return errors.New("NAC_BOOTSTRAP_ADMIN_PASSWORD must be at least 12 characters")
	}
	hash, err := auth.HashPassword(cfg.Bootstrap.AdminPassword)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	op := &domain.Operator{ID: uuid.New(), Email: email, PasswordHash: hash, Role: domain.RoleAdmin, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(ctx, op); err != nil && !errors.Is(err, domain.ErrConflict) {
		return fmt.Errorf("bootstrap create: %w", err)
	}
	logger.Info("bootstrapped administrator", "email", email)
	return nil
}
