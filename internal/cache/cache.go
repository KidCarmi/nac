// Package cache provides the PolicyCache port implementations: a Redis-backed
// cache for multi-instance deployments and a no-op cache for development or
// when Redis is intentionally disabled.
//
// Caching the enabled-policy set is a deliberate scalability decision: the
// access-evaluation endpoint is the hottest path in the system (every device
// authentication hits it) and the policy set changes rarely. A short TTL plus
// explicit invalidation on policy mutation bounds staleness while removing the
// database from the critical path.
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/observability"
)

const enabledPoliciesKey = "nac:policies:enabled"

// NoopCache satisfies domain.PolicyCache but never caches. It is the safe
// default: correctness is preserved, only the optimization is absent.
type NoopCache struct{}

func (NoopCache) GetEnabled(context.Context) ([]domain.Policy, bool) { return nil, false }
func (NoopCache) SetEnabled(context.Context, []domain.Policy)        {}
func (NoopCache) Invalidate(context.Context)                         {}

// RedisCache is a Redis-backed PolicyCache.
type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

// Client is the subset of *redis.Client used here, exposed for connection
// management by the composition root.
type Client = redis.Client

// Connect opens and pings a Redis client.
func Connect(ctx context.Context, addr, password string, db int) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// NewRedisCache wraps a client with the configured TTL.
func NewRedisCache(client *redis.Client, ttl time.Duration) *RedisCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &RedisCache{client: client, ttl: ttl}
}

// GetEnabled returns the cached enabled policy set. A cache or decode failure
// is treated as a miss (fail open to the database), never as an error to the
// caller, so a degraded cache cannot take down access decisions.
func (c *RedisCache) GetEnabled(ctx context.Context) ([]domain.Policy, bool) {
	raw, err := c.client.Get(ctx, enabledPoliciesKey).Bytes()
	if err != nil {
		return nil, false
	}
	var policies []domain.Policy
	if err := json.Unmarshal(raw, &policies); err != nil {
		observability.LoggerFrom(ctx).Warn("policy cache decode failed; treating as miss", "error", err)
		return nil, false
	}
	return policies, true
}

// SetEnabled stores the enabled policy set with the configured TTL. Failures
// are logged but not propagated.
func (c *RedisCache) SetEnabled(ctx context.Context, policies []domain.Policy) {
	raw, err := json.Marshal(policies)
	if err != nil {
		return
	}
	if err := c.client.Set(ctx, enabledPoliciesKey, raw, c.ttl).Err(); err != nil {
		observability.LoggerFrom(ctx).Warn("policy cache write failed", "error", err)
	}
}

// Invalidate deletes the cached policy set.
func (c *RedisCache) Invalidate(ctx context.Context) {
	if err := c.client.Del(ctx, enabledPoliciesKey).Err(); err != nil {
		observability.LoggerFrom(ctx).Warn("policy cache invalidate failed", "error", err)
	}
}
