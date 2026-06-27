package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/domain"
)

// AuditRepo is the PostgreSQL implementation of domain.AuditRepository. It is
// deliberately append-only: there is no Update or Delete, preserving the
// integrity of the security log.
type AuditRepo struct {
	pool *pgxpool.Pool
}

// NewAuditRepo constructs an AuditRepo.
func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo { return &AuditRepo{pool: pool} }

const auditColumns = `id, occurred_at, actor_id, actor_email, action, resource_type, resource_id, outcome, request_id, remote_addr, metadata`

func (r *AuditRepo) Append(ctx context.Context, e *domain.AuditEvent) error {
	meta, err := json.Marshal(orEmptyMap(e.Metadata))
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO audit_events (`+auditColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ID, e.OccurredAt, e.ActorID, e.ActorEmail, e.Action, e.ResourceType,
		e.ResourceID, e.Outcome, e.RequestID, e.RemoteAddr, meta)
	return err
}

func (r *AuditRepo) List(ctx context.Context, p domain.Page) ([]domain.AuditEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+auditColumns+` FROM audit_events
		ORDER BY occurred_at DESC
		LIMIT $1 OFFSET $2`, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.AuditEvent
	for rows.Next() {
		var (
			e    domain.AuditEvent
			meta []byte
		)
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorID, &e.ActorEmail, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.Outcome, &e.RequestID, &e.RemoteAddr, &meta); err != nil {
			return nil, err
		}
		if len(meta) > 0 {
			if err := json.Unmarshal(meta, &e.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
