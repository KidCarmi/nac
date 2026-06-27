package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/domain"
)

// OperatorRepo is the PostgreSQL implementation of domain.OperatorRepository.
type OperatorRepo struct {
	pool *pgxpool.Pool
}

// NewOperatorRepo constructs an OperatorRepo.
func NewOperatorRepo(pool *pgxpool.Pool) *OperatorRepo { return &OperatorRepo{pool: pool} }

const operatorColumns = `id, email, password_hash, role, disabled, created_at, updated_at`

func (r *OperatorRepo) Create(ctx context.Context, o *domain.Operator) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO operators (`+operatorColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		o.ID, o.Email, o.PasswordHash, o.Role, o.Disabled, o.CreatedAt, o.UpdatedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: operator with email %q already exists", domain.ErrConflict, o.Email)
	}
	return err
}

func (r *OperatorRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Operator, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+operatorColumns+` FROM operators WHERE id = $1`, id)
	return scanOperatorOrNotFound(row)
}

func (r *OperatorRepo) GetByEmail(ctx context.Context, email string) (*domain.Operator, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+operatorColumns+` FROM operators WHERE lower(email) = lower($1)`, email)
	return scanOperatorOrNotFound(row)
}

func (r *OperatorRepo) List(ctx context.Context, p domain.Page) ([]domain.Operator, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+operatorColumns+` FROM operators
		ORDER BY created_at ASC
		LIMIT $1 OFFSET $2`, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Operator
	for rows.Next() {
		o, err := scanOperator(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func scanOperatorOrNotFound(row rowScanner) (*domain.Operator, error) {
	o, err := scanOperator(row)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	return o, err
}

func scanOperator(row rowScanner) (*domain.Operator, error) {
	var o domain.Operator
	if err := row.Scan(&o.ID, &o.Email, &o.PasswordHash, &o.Role, &o.Disabled, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return nil, err
	}
	return &o, nil
}
