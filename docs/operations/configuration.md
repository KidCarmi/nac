# Configuration

The NAC daemon (`cmd/nacd`) is configured **entirely through environment
variables**. There is no configuration file format and no command-line flags for
runtime tuning — the twelve-factor model keeps the single binary identical
across development, staging, and production, with only the environment differing.

All configuration is loaded and **validated once at startup** by
`internal/config/config.go`. Validation errors are **aggregated**: the process
reports every invalid or missing setting in a single failure rather than
stopping at the first problem, so a misconfigured deployment surfaces all of its
issues in one boot attempt.

If validation fails, the process exits before binding the HTTP listener. A
configuration that passes validation is guaranteed to have a usable database
DSN, a valid default access action, a positive rate limit, and — in non-development
environments — cryptographically adequate secrets.

## Environment variable reference

### Runtime environment

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_ENV` | enum (`development`, `staging`, `production`) | `development` | No | Selects the runtime profile. Non-development profiles enforce stricter secret requirements (see validation rules below). |

### HTTP server

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_HTTP_ADDR` | address | `:8080` | No | Listen address for the HTTP server. |
| `NAC_HTTP_READ_TIMEOUT` | duration | `10s` | No | Maximum duration for reading the entire request, including the body. |
| `NAC_HTTP_WRITE_TIMEOUT` | duration | `15s` | No | Maximum duration before timing out writes of the response. |
| `NAC_HTTP_IDLE_TIMEOUT` | duration | `60s` | No | Maximum time to wait for the next request on a keep-alive connection. |
| `NAC_HTTP_READ_HEADER_TIMEOUT` | duration | `5s` | No | Maximum time allowed to read request headers. |
| `NAC_HTTP_SHUTDOWN_TIMEOUT` | duration | `20s` | No | Grace period for in-flight requests to complete during shutdown. |
| `NAC_HTTP_MAX_BODY_BYTES` | bytes | `1048576` (1 MiB) | No | Maximum accepted request body size, enforced by the `limitBody` middleware. |
| `NAC_HTTP_RATE_LIMIT_PER_MIN` | integer | `600` | No | Per-client fixed-window request budget per minute. Must be `> 0`. |

### Database (PostgreSQL via pgx v5)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_DB_DSN` | string | — | **Yes** | PostgreSQL connection string. The process will not start without it. |
| `NAC_DB_MAX_CONNS` | integer | `10` | No | Maximum size of the connection pool. |
| `NAC_DB_MIN_CONNS` | integer | `2` | No | Minimum number of idle connections kept warm in the pool. |
| `NAC_DB_MAX_CONN_LIFETIME` | duration | `1h` | No | Maximum lifetime of a pooled connection before it is recycled. |

### Redis cache (go-redis v9)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_REDIS_ADDR` | address | empty | No | Redis address. **When empty, caching is disabled** and a no-op cache is used. |
| `NAC_REDIS_PASSWORD` | string | empty | No | Redis password, if the server requires authentication. |
| `NAC_REDIS_DB` | integer | `0` | No | Redis logical database index. |
| `NAC_REDIS_TTL` | duration | `30s` | No | Time-to-live for the cached enabled policy set. |

### Authentication and authorization

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_JWT_SIGNING_KEY` | string (>= 32 bytes) | insecure dev fallback | **Yes** (non-dev) | HMAC signing key for operator JWTs. In development a built-in insecure fallback is used; in staging/production a key of at least 32 bytes is required. |
| `NAC_JWT_ISSUER` | string | `nac` | No | `iss` claim asserted on issued tokens. |
| `NAC_JWT_AUDIENCE` | string | `nac-api` | No | `aud` claim asserted and validated on tokens. |
| `NAC_JWT_TTL` | duration | `1h` | No | Lifetime of issued operator JWTs. |
| `NAC_SERVICE_TOKEN` | string (>= 24 chars) | — | **Yes** (non-dev) | Shared secret presented by service clients. Required and must be at least 24 characters outside development. |

### Policy engine

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_DEFAULT_ACTION` | enum (`deny`, `quarantine`, `allow`) | `deny` | No | Fallback access decision when no policy matches. Must be one of the three valid actions. |

### Logging

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_LOG_LEVEL` | enum (`debug`, `info`, `warn`, `error`) | `info` | No | Minimum log level emitted. |
| `NAC_LOG_FORMAT` | enum (`json`, `text`) | `json` | No | Log encoding. Must be `json` or `text`. |

### Bootstrap administrator (optional, first-boot)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `NAC_BOOTSTRAP_ADMIN_EMAIL` | string | empty | No | Email for an administrator created on first boot. |
| `NAC_BOOTSTRAP_ADMIN_PASSWORD` | string | empty | No | Password for the bootstrap administrator. Hashed with argon2 before storage. |

The bootstrap administrator provisioning is **idempotent**: it creates the
account only if it does not already exist, so leaving these variables set across
restarts is safe.

## Validation behavior

At startup the loader applies the following rules and aggregates all failures
into a single error before exiting:

| Rule | Scope | Behavior on failure |
|------|-------|---------------------|
| `NAC_DB_DSN` must be present | All environments | Boot rejected. |
| `NAC_DEFAULT_ACTION` must be `deny`, `quarantine`, or `allow` | All environments | Boot rejected. |
| `NAC_HTTP_RATE_LIMIT_PER_MIN` must be `> 0` | All environments | Boot rejected. |
| `NAC_LOG_FORMAT` must be `json` or `text` | All environments | Boot rejected. |
| `NAC_JWT_SIGNING_KEY` must be `>= 32` bytes | Non-development | Boot rejected. |
| `NAC_SERVICE_TOKEN` must be `>= 24` characters | Non-development | Boot rejected. |

Because errors are aggregated, a single boot attempt reports the complete set of
problems rather than one at a time. This is intentional: it minimizes the number
of deploy-fix-redeploy cycles needed to bring up a misconfigured environment.

## Secure-by-default behavior

The defaults are chosen so that a deployment is safe even when an operator
forgets to set a value:

- **`NAC_DEFAULT_ACTION` defaults to `deny`.** When no policy matches a request,
  access is denied rather than granted. Fail-closed is the default posture of the
  policy engine.
- **Secrets are mandatory outside development.** In staging and production the
  process refuses to start without an adequately sized JWT signing key and
  service token. The insecure development fallback is never silently used in a
  non-development environment.
- **Request bodies are bounded.** `NAC_HTTP_MAX_BODY_BYTES` caps body size to
  defend against memory-exhaustion abuse, enforced via `MaxBytesReader`.
- **Requests are rate limited.** A positive per-client per-minute budget is
  required, preventing an accidental `0` from disabling the protection.
- **Redis is optional and fails open.** If Redis is not configured, a no-op cache
  is used and the system reads policy directly from PostgreSQL. Caching never
  becomes a hard dependency, and any cache error or decode failure falls back to
  the database, preserving correctness.
- **Secrets are never logged.** The logging layer redacts sensitive attribute
  keys regardless of log level (see [logging.md](logging.md)).

## Example: development

Development relies on permissive defaults. Only the database DSN is strictly
required; the JWT signing key uses an insecure built-in fallback and the service
token is not enforced.

```bash
# Minimal local development environment
export NAC_ENV=development
export NAC_DB_DSN="postgres://nac:nac@localhost:5432/nac?sslmode=disable"

# Optional: enable a local Redis cache
export NAC_REDIS_ADDR="localhost:6379"

# Optional: provision a first-boot admin
export NAC_BOOTSTRAP_ADMIN_EMAIL="admin@example.test"
export NAC_BOOTSTRAP_ADMIN_PASSWORD="change-me-locally"

# Optional: more verbose, human-readable logs while iterating
export NAC_LOG_LEVEL=debug
export NAC_LOG_FORMAT=text
```

## Example: production

Production sets `NAC_ENV=production`, which enforces the secret length rules.
All secrets are injected from a secrets manager rather than written inline (see
[secrets-management.md](secrets-management.md)).

```bash
export NAC_ENV=production
export NAC_HTTP_ADDR=":8080"

# Database — DSN and credentials sourced from the secrets manager
export NAC_DB_DSN="postgres://nac:${DB_PASSWORD}@postgres:5432/nac?sslmode=require"
export NAC_DB_MAX_CONNS=20
export NAC_DB_MIN_CONNS=2

# Redis cache
export NAC_REDIS_ADDR="redis:6379"
export NAC_REDIS_PASSWORD="${REDIS_PASSWORD}"
export NAC_REDIS_TTL=30s

# Auth secrets — generated and stored in the secrets manager
export NAC_JWT_SIGNING_KEY="${JWT_SIGNING_KEY}"   # >= 32 bytes
export NAC_SERVICE_TOKEN="${SERVICE_TOKEN}"       # >= 24 chars
export NAC_JWT_TTL=1h

# Policy posture
export NAC_DEFAULT_ACTION=deny

# Observability
export NAC_LOG_LEVEL=info
export NAC_LOG_FORMAT=json
```

In Kubernetes, the non-secret values above are supplied through the `nacd-config`
ConfigMap and the secrets through the `nacd-secrets` Secret. See
[`deployments/kubernetes/config.yaml`](../../deployments/kubernetes/config.yaml).

## Related documentation

- [Secrets management](secrets-management.md) — how the JWT key, service token,
  database credentials, and Redis password are generated and injected.
- [Observability](observability.md) — logging, metrics, and tracing overview.
- [Logging](logging.md) — log format, levels, and redaction.
- [Metrics](metrics.md) — Prometheus collectors and scrape configuration.
- [Tracing](tracing.md) — W3C trace context propagation.
- `.env.example` at the repository root — a copy-ready environment template.


## Pilot settings (RADIUS, switches, CoA)

| Variable | Type | Default | Required | Description |
|---|---|---|---|---|
| `NAC_HTTPS_ADDR` | address | — | No | Adds an HTTPS listener for operators (installer: `:8443`). Requires the two TLS files below. |
| `NAC_TLS_CERT_FILE`, `NAC_TLS_KEY_FILE` | path | — | with HTTPS | Console certificate and key (PEM). |
| `NAC_SECRETS_KEY` | secret (32+ chars) | dev key in development | **Yes** outside development | Encrypts switch RADIUS secrets and eAPI passwords at rest (AES-256-GCM). Losing it makes stored switch secrets unreadable. |
| `NAC_POSTURE_MAX_AGE` | duration | `24h` | No | Posture older than this is stale evidence (treated as missing). |
| `NAC_OBSERVATION_MAX_AGE` | duration | `5m` | No | Switch observations older than this are shown as stale. |
| `NAC_COA_TIMEOUT` | duration | `3s` | No | Wait per CoA transmission. |
| `NAC_COA_RETRIES` | int 0–10 | `2` | No | Retransmissions after the first CoA attempt (byte-identical). |
| `NAC_FREERADIUS_CLIENTS_FILE` | path | — | No | Where nacd renders FreeRADIUS client definitions (installer: `/var/lib/nac/freeradius-clients.conf`). |
| `NAC_FREERADIUS_OUTAGE_LOG` | path | — | No | FreeRADIUS outage log imported into the timeline (installer: `/var/log/freeradius/nac-outage.log`). |
| `NAC_EAPI_INSECURE_SKIP_VERIFY` | bool | `false` | No | Accept self-signed switch eAPI certificates. |
| `NAC_BOOTSTRAP_ADMIN_EMAIL`, `NAC_BOOTSTRAP_ADMIN_PASSWORD` | string | — | No | Creates the first administrator if missing (password 12+ characters). |
