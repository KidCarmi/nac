package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// ResetOperatorPassword is the host-local recovery path for a forgotten
// operator password (`nacd -reset-password`). It needs the service's database
// credentials, so only someone with root on the control node can run it. It
// applies the same password policy as the API, revokes every session of the
// operator and writes an audit event attributed to the host command.
func ResetOperatorPassword(ctx context.Context, cfg *config.Config, email, password string) error {
	pool, err := postgres.Connect(ctx, cfg.DB)
	if err != nil {
		return fmt.Errorf("database: %w (run with the service environment, e.g. via nac-reset-password)", err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	ops := postgres.NewOperatorRepo(pool)
	op, err := ops.GetByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return fmt.Errorf("operator %q: %w", email, err)
	}
	if reason := auth.ValidateNewPassword(password, op.Email); reason != "" {
		return fmt.Errorf("%w: %s", domain.ErrInvalidInput, reason)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := ops.SetPassword(ctx, op.ID, hash, now); err != nil {
		return err
	}
	return postgres.NewAuditRepo(pool).Append(ctx, &domain.AuditEvent{
		ID: uuid.New(), OccurredAt: now, ActorEmail: "host:nacd -reset-password",
		Action: "operator.password_reset", ResourceType: "operator", ResourceID: op.ID.String(), Outcome: "success",
		Metadata: map[string]string{"email": op.Email, "sessions_revoked": "all sessions of this operator"},
	})
}
