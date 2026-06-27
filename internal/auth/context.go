package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// Principal is the authenticated identity making a request.
type Principal struct {
	ID    uuid.UUID
	Email string
	Role  domain.Role
}

type principalKey struct{}

// WithPrincipal returns a child context carrying the authenticated principal.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom extracts the principal from ctx, if present.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok && p != nil
}
