package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// ErrInvalidToken is returned when a token is missing, malformed, expired, or
// signed with an unexpected algorithm or key.
var ErrInvalidToken = errors.New("invalid token")

// Claims is the JWT payload for operator sessions.
type Claims struct {
	Email string      `json:"email"`
	Role  domain.Role `json:"role"`
	jwt.RegisteredClaims
}

// TokenIssuer mints and verifies operator session tokens using HMAC-SHA256.
// The signing key must be at least 32 bytes; shorter keys are rejected at
// construction time to prevent weak deployments.
type TokenIssuer struct {
	key      []byte
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

// NewTokenIssuer validates the key length and returns a configured issuer.
func NewTokenIssuer(key []byte, issuer, audience string, ttl time.Duration) (*TokenIssuer, error) {
	if len(key) < 32 {
		return nil, errors.New("jwt signing key must be at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("jwt ttl must be positive")
	}
	return &TokenIssuer{
		key:      key,
		issuer:   issuer,
		audience: audience,
		ttl:      ttl,
		now:      time.Now,
	}, nil
}

// Issue mints a signed token for the given operator.
func (t *TokenIssuer) Issue(op *domain.Operator) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	claims := Claims{
		Email: op.Email,
		Role:  op.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   op.ID.String(),
			Issuer:    t.issuer,
			Audience:  jwt.ClaimStrings{t.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(t.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Parse verifies a token's signature, algorithm, issuer, audience and expiry,
// returning the principal it represents.
func (t *TokenIssuer) Parse(tokenStr string) (*Principal, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(tok *jwt.Token) (any, error) {
		// Pin the algorithm: reject anything other than HS256 to defeat
		// "alg=none" and algorithm-confusion attacks.
		if tok.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidToken, tok.Header["alg"])
		}
		return t.key, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(t.issuer),
		jwt.WithAudience(t.audience),
		jwt.WithTimeFunc(t.now),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("%w: bad subject", ErrInvalidToken)
	}
	if !claims.Role.Valid() {
		return nil, fmt.Errorf("%w: bad role", ErrInvalidToken)
	}
	return &Principal{ID: id, Email: claims.Email, Role: claims.Role}, nil
}

// withClock overrides the time source for tests.
func (t *TokenIssuer) withClock(now func() time.Time) *TokenIssuer {
	t.now = now
	return t
}
