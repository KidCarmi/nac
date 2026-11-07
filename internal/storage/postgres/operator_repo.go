package postgres

import (
	"context"
	"fmt"
	"time"

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

// operatorReadColumns adds the session-revocation fields (set by migration 0005).
const operatorReadColumns = operatorColumns + `, token_version, password_changed_at`

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
	row := r.pool.QueryRow(ctx, `SELECT `+operatorReadColumns+` FROM operators WHERE id = $1`, id)
	return scanOperatorOrNotFound(row)
}

func (r *OperatorRepo) GetByEmail(ctx context.Context, email string) (*domain.Operator, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+operatorReadColumns+` FROM operators WHERE lower(email) = lower($1)`, email)
	return scanOperatorOrNotFound(row)
}

func (r *OperatorRepo) List(ctx context.Context, p domain.Page) ([]domain.Operator, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+operatorReadColumns+` FROM operators
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

// SetPassword replaces the password hash and revokes every existing session
// by incrementing token_version.
func (r *OperatorRepo) SetPassword(ctx context.Context, id uuid.UUID, hash string, at time.Time) (int, error) {
	var v int
	err := r.pool.QueryRow(ctx, `
		UPDATE operators SET password_hash = $2, token_version = token_version + 1,
			password_changed_at = $3, updated_at = $3
		WHERE id = $1 RETURNING token_version`, id, hash, at).Scan(&v)
	if noRows(err) {
		return 0, domain.ErrNotFound
	}
	return v, err
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
	if err := row.Scan(&o.ID, &o.Email, &o.PasswordHash, &o.Role, &o.Disabled, &o.CreatedAt, &o.UpdatedAt, &o.TokenVersion, &o.PasswordChangedAt); err != nil {
		return nil, err
	}
	return &o, nil
}
