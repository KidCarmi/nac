package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/domain"
)

// PolicyRepo is the PostgreSQL implementation of domain.PolicyRepository.
type PolicyRepo struct {
	pool *pgxpool.Pool
}

// NewPolicyRepo constructs a PolicyRepo.
func NewPolicyRepo(pool *pgxpool.Pool) *PolicyRepo { return &PolicyRepo{pool: pool} }

const policyColumns = `id, name, description, priority, enabled, conditions, action, segment, created_at, updated_at`

func (r *PolicyRepo) Create(ctx context.Context, p *domain.Policy) error {
	conds, err := marshalConditions(p.Conditions)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO policies (`+policyColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		p.ID, p.Name, p.Description, p.Priority, p.Enabled, conds, p.Action, p.Segment, p.CreatedAt, p.UpdatedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: policy named %q already exists", domain.ErrConflict, p.Name)
	}
	return err
}

func (r *PolicyRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Policy, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+policyColumns+` FROM policies WHERE id = $1`, id)
	p, err := scanPolicy(row)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	return p, err
}

func (r *PolicyRepo) Update(ctx context.Context, p *domain.Policy) error {
	conds, err := marshalConditions(p.Conditions)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE policies SET
			name=$2, description=$3, priority=$4, enabled=$5, conditions=$6,
			action=$7, segment=$8, updated_at=$9
		WHERE id=$1`,
		p.ID, p.Name, p.Description, p.Priority, p.Enabled, conds, p.Action, p.Segment, p.UpdatedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: policy named %q already exists", domain.ErrConflict, p.Name)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PolicyRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM policies WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PolicyRepo) List(ctx context.Context, p domain.Page) ([]domain.Policy, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+policyColumns+` FROM policies
		ORDER BY priority ASC, name ASC
		LIMIT $1 OFFSET $2`, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectPolicies(rows)
}

func (r *PolicyRepo) ListEnabled(ctx context.Context) ([]domain.Policy, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+policyColumns+` FROM policies
		WHERE enabled
		ORDER BY priority ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectPolicies(rows)
}

func collectPolicies(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]domain.Policy, error) {
	var out []domain.Policy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func scanPolicy(row rowScanner) (*domain.Policy, error) {
	var (
		p     domain.Policy
		conds []byte
	)
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Priority, &p.Enabled, &conds, &p.Action, &p.Segment, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if len(conds) > 0 {
		if err := json.Unmarshal(conds, &p.Conditions); err != nil {
			return nil, fmt.Errorf("unmarshal conditions: %w", err)
		}
	}
	return &p, nil
}

func marshalConditions(conds []domain.Condition) ([]byte, error) {
	if conds == nil {
		conds = []domain.Condition{}
	}
	b, err := json.Marshal(conds)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}
	return b, nil
}
