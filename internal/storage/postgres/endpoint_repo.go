package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kidcarmi/nac/internal/domain"
)

// EndpointRepo is the PostgreSQL implementation of domain.EndpointRepository.
type EndpointRepo struct {
	pool *pgxpool.Pool
}

// NewEndpointRepo constructs an EndpointRepo.
func NewEndpointRepo(pool *pgxpool.Pool) *EndpointRepo { return &EndpointRepo{pool: pool} }

const endpointColumns = `id, mac, ip, hostname, device_type, access_status, posture, attributes, last_seen, created_at, updated_at, posture_source, posture_observed_at`

func (r *EndpointRepo) Create(ctx context.Context, e *domain.Endpoint) error {
	attrs, err := json.Marshal(orEmptyMap(e.Attributes))
	if err != nil {
		return fmt.Errorf("marshal attributes: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO endpoints (`+endpointColumns+`)
		VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.ID, e.MAC, e.IP, e.Hostname, e.DeviceType, e.AccessStatus, e.Posture, attrs, e.LastSeen, e.CreatedAt, e.UpdatedAt,
		e.PostureSource, e.PostureObservedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: endpoint with MAC %s already exists", domain.ErrConflict, e.MAC)
	}
	return err
}

func (r *EndpointRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Endpoint, error) {
	return r.scanOne(ctx, `SELECT `+endpointColumns+` FROM endpoints WHERE id = $1`, id)
}

func (r *EndpointRepo) GetByMAC(ctx context.Context, mac string) (*domain.Endpoint, error) {
	return r.scanOne(ctx, `SELECT `+endpointColumns+` FROM endpoints WHERE mac = $1`, mac)
}

func (r *EndpointRepo) Update(ctx context.Context, e *domain.Endpoint) error {
	attrs, err := json.Marshal(orEmptyMap(e.Attributes))
	if err != nil {
		return fmt.Errorf("marshal attributes: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE endpoints SET
			ip = NULLIF($2,''), hostname = NULLIF($3,''), device_type = NULLIF($4,''),
			access_status = $5, posture = $6, attributes = $7, last_seen = $8, updated_at = $9,
			posture_source = $10, posture_observed_at = $11
		WHERE id = $1`,
		e.ID, e.IP, e.Hostname, e.DeviceType, e.AccessStatus, e.Posture, attrs, e.LastSeen, e.UpdatedAt,
		e.PostureSource, e.PostureObservedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EndpointRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM endpoints WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EndpointRepo) List(ctx context.Context, f domain.EndpointFilter, p domain.Page) ([]domain.Endpoint, error) {
	// Parameterized query; the search term is bound, never interpolated, so
	// it is immune to SQL injection. The trailing % for prefix matching is
	// added to the bound value, not the SQL text.
	rows, err := r.pool.Query(ctx, `
		SELECT `+endpointColumns+` FROM endpoints
		WHERE ($1 = '' OR access_status = $1)
		  AND ($2 = '' OR mac ILIKE $2 || '%' OR hostname ILIKE $2 || '%')
		ORDER BY last_seen DESC
		LIMIT $3 OFFSET $4`,
		string(f.AccessStatus), f.Search, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Endpoint
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *EndpointRepo) scanOne(ctx context.Context, q string, args ...any) (*domain.Endpoint, error) {
	row := r.pool.QueryRow(ctx, q, args...)
	e, err := scanEndpoint(row)
	if noRows(err) {
		return nil, domain.ErrNotFound
	}
	return e, err
}

// rowScanner abstracts pgx.Row and pgx.Rows for shared scanning.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEndpoint(row rowScanner) (*domain.Endpoint, error) {
	var (
		e        domain.Endpoint
		ip       *string
		hostname *string
		devType  *string
		attrs    []byte
	)
	if err := row.Scan(&e.ID, &e.MAC, &ip, &hostname, &devType, &e.AccessStatus, &e.Posture, &attrs, &e.LastSeen, &e.CreatedAt, &e.UpdatedAt, &e.PostureSource, &e.PostureObservedAt); err != nil {
		return nil, err
	}
	e.IP = deref(ip)
	e.Hostname = deref(hostname)
	e.DeviceType = deref(devType)
	if len(attrs) > 0 {
		if err := json.Unmarshal(attrs, &e.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes: %w", err)
		}
	}
	return &e, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
