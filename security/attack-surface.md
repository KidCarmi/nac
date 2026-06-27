# Attack Surface

A precise enumeration of everything an attacker can reach, and the controls on
each. Keep this in sync with `internal/server/router.go`.

## Network exposure

| Surface | Exposure | Notes |
|---------|----------|-------|
| `nacd` HTTP `:8080` | Behind ingress/LB (TLS) | Never expose plaintext to untrusted networks |
| PostgreSQL `:5432` | App network only | TLS + least-privilege role |
| Redis `:6379` | App network only | AUTH + TLS; no secrets stored |
| `/metrics` | Internal scrape | No secrets in labels/values |

## HTTP entry points

| Route | Method | Auth | Input | Key controls |
|-------|--------|------|-------|--------------|
| `/healthz` | GET | none | — | No dependency checks (liveness) |
| `/readyz` | GET | none | — | DB ping; 503 if not ready |
| `/version` | GET | none | — | Build metadata only |
| `/metrics` | GET | none | — | Prometheus exposition |
| `/openapi.yaml` | GET | none | — | Static contract |
| `/api/v1/auth/login` | POST | none | email, password | Argon2id, generic 401, constant-work on unknown user, rate limit |
| `/api/v1/endpoints` | GET/POST | JWT+RBAC | filters / endpoint body | Validation, MAC normalization, body cap, unknown-field rejection |
| `/api/v1/endpoints/{id}` | GET/PUT/DELETE | JWT+RBAC | UUID path, body | UUID parse, immutable MAC, validation |
| `/api/v1/policies` | GET/POST | JWT+RBAC | policy body | Operator/value-count validation, fail-closed engine |
| `/api/v1/policies/{id}` | GET/PUT/DELETE | JWT+RBAC | UUID path, body | As above; cache invalidation |
| `/api/v1/access/evaluate` | POST | service token | MAC, posture, attrs | Constant-time token, MAC/posture validation |
| `/api/v1/audit` | GET | JWT+RBAC | pagination | Read-only; bounded page size |

## Global request controls (middleware)

- Panic recovery (no crash, no stack leak to client).
- W3C trace context + correlation id on every request/log.
- Security headers: `X-Content-Type-Options`, `X-Frame-Options: DENY`, strict
  CSP, `Referrer-Policy: no-referrer`, `Cache-Control: no-store`.
- Per-client fixed-window rate limiting.
- Request body size cap (`MaxBytesReader`).
- Server timeouts: read/read-header/write/idle.

## Inputs requiring validation (and where)

| Input | Validator | Location |
|-------|-----------|----------|
| MAC address | normalize + 48-bit hex check | `pkg/validation`, services, access handler |
| IP address | `net.ParseIP` | endpoint service |
| Enums (status/posture/action/operator) | typed `Valid()` + DB CHECK | domain + migrations |
| Hostnames/attributes | length + count bounds | endpoint service |
| Policy conditions | operator/value-arity rules | policy service |
| JSON bodies | strict decode, no unknown fields, single object | `internal/server/render.go` |
| UUIDs | `uuid.Parse` | handlers |
| Pagination | clamp to max limit | services |

## Trust assumptions

See [Trust Boundaries](../docs/security/trust-boundaries.md). TLS termination,
network segmentation, host hardening, and the secrets manager are provided by
the deployment environment and are out of `nacd`'s code scope.
