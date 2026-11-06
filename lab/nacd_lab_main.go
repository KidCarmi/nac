// Lab-only composition root: the upstream commit documents cmd/nacd but does
// not include it. This wires the unmodified NAC services for the EVE test.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/cache"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/endpoint"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/internal/policy"
	"github.com/kidcarmi/nac/internal/server"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, cfg.DB)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		log.Fatal(err)
	}

	operators := postgres.NewOperatorRepo(pool)
	if email, password := os.Getenv("NAC_BOOTSTRAP_ADMIN_EMAIL"), os.Getenv("NAC_BOOTSTRAP_ADMIN_PASSWORD"); email != "" && password != "" {
		if _, err := operators.GetByEmail(ctx, email); errors.Is(err, domain.ErrNotFound) {
			hash, err := auth.HashPassword(password)
			if err != nil {
				log.Fatal(err)
			}
			now := time.Now().UTC()
			if err := operators.Create(ctx, &domain.Operator{ID: uuid.New(), Email: email, PasswordHash: hash, Role: domain.RoleAdmin, CreatedAt: now, UpdatedAt: now}); err != nil {
				log.Fatal(err)
			}
		} else if err != nil {
			log.Fatal(err)
		}
	}

	tokens, err := auth.NewTokenIssuer(cfg.Auth.JWTSigningKey, cfg.Auth.JWTIssuer, cfg.Auth.JWTAudience, cfg.Auth.JWTTTL)
	if err != nil {
		log.Fatal(err)
	}
	endpointRepo := postgres.NewEndpointRepo(pool)
	policyRepo := postgres.NewPolicyRepo(pool)
	policySvc := policy.NewService(policyRepo, cache.NoopCache{})
	engine := access.NewEngine()
	engine.DefaultAction = cfg.DefaultAction
	auditRepo := postgres.NewAuditRepo(pool)
	app := server.New(server.Deps{
		Config:    cfg,
		Logger:    observability.NewLogger(cfg.Log.Level, cfg.Log.Format),
		Metrics:   observability.NewMetrics(),
		Tokens:    tokens,
		Endpoints: endpoint.NewService(endpointRepo),
		Policies:  policySvc,
		Access:    access.NewService(engine, policySvc, endpointRepo),
		Operators: operators,
		AuditRepo: auditRepo,
		Audit:     audit.NewRecorder(auditRepo),
		Ready:     pool.Ping,
	})
	server := &http.Server{Addr: cfg.HTTP.Addr, Handler: app.Handler(), ReadTimeout: cfg.HTTP.ReadTimeout, WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout, ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout}
	log.Fatal(server.ListenAndServe())
}
