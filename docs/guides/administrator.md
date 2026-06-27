# Administrator Guide

This guide is for platform administrators responsible for deploying, configuring,
securing, and operating the **NAC** (Network Access Control) platform (`nacd`).
It covers installation, configuration and secret management, bootstrapping the
first operator, authoring policies, managing endpoints, wiring up network
enforcement points, reading the audit log, monitoring, and day-to-day tasks.

If you are an operator or viewer *using* the API rather than running the
service, see the [User Manual](user-manual.md). If you are developing the
platform, see the [Developer Guide](developer.md).

---

## 1. Overview

`nacd` is a single Go binary (`cmd/nacd`) backed by PostgreSQL (required) and
Redis (optional, for policy caching). It exposes:

- A **control-plane REST API** (`/api/v1/...`) for operators, authenticated with
  JWT bearer tokens.
- An **enforcement decision endpoint** (`POST /api/v1/access/evaluate`) for
  network enforcement points (switches, wireless controllers, RADIUS bridges),
  authenticated with a static **service token**.
- **Unauthenticated operational endpoints**: `GET /healthz`, `/readyz`,
  `/version`, `/metrics`, `/openapi.yaml`.

All errors are returned as RFC 7807 `application/problem+json`. The decision
engine is **default-deny**: if no policy matches, access is denied.

---

## 2. Installation and deployment

### 2.1 Prerequisites

- Docker / Docker Compose (for the local or single-host stack), or a Kubernetes
  cluster (for production).
- PostgreSQL 14+ reachable by the service.
- Optionally, Redis 6+ for policy caching.
- Go 1.24+ only if building from source.

### 2.2 Local / single-host (Docker Compose)

The repository ships a Compose stack that brings up PostgreSQL, Redis, and
`nacd` together:

```bash
make up      # docker compose up --build -d
make down    # docker compose down -v  (DESTROYS volumes)
```

The Compose stack seeds a **development-only** bootstrap admin and a fixed
service token. Do **not** use these values outside development:

| Setting | Dev value |
|---------|-----------|
| Bootstrap admin email | `admin@nac.local` |
| Bootstrap admin password | `ChangeMe123!` |
| Service token | `dev-service-token-change-me-please-1234` |

Once the stack is healthy, exercise the API end-to-end with the smoke script
(requires `curl` and `jq`):

```bash
./scripts/smoke.sh
```

### 2.3 Building and running the binary directly

```bash
make build           # produces ./bin/nacd
make migrate         # applies embedded DB migrations and exits (nacd -migrate)
make run             # go run ./cmd/nacd (expects Postgres/Redis reachable)
```

Migrations are embedded in the binary; running `nacd -migrate` applies any
outstanding migrations and exits. Run this before first start and on every
upgrade.

### 2.4 Kubernetes

Production manifests live in [`deployments/kubernetes/`](../../deployments/kubernetes/):

| File | Purpose |
|------|---------|
| `deployment.yaml` | `nacd` Deployment (distroless non-root image, hardened `securityContext`, probes), plus HPA and PDB |
| `service.yaml` | ClusterIP Service for the API |
| `config.yaml` | Non-secret configuration (`ConfigMap`) |

Recommended rollout:

```bash
# 1. Create the namespace and the secret (see §3.3) out of band.
kubectl create namespace nac

# 2. Apply non-secret config and the workload.
kubectl -n nac apply -f deployments/kubernetes/config.yaml
kubectl -n nac apply -f deployments/kubernetes/service.yaml
kubectl -n nac apply -f deployments/kubernetes/deployment.yaml

# 3. Run migrations as a one-off Job or init step before serving traffic.
kubectl -n nac run nacd-migrate --rm -it --restart=Never \
  --image=nac/nacd:<version> --command -- /nacd -migrate
```

Liveness should target `/healthz`; readiness should target `/readyz` (which also
checks downstream dependencies). See
[operations/observability.md](../operations/observability.md) for probe and
dashboard guidance.

---

## 3. Configuration and secrets

All configuration is read from environment variables at startup (12-factor).
The canonical reference is [operations/configuration.md](../operations/configuration.md)
and the annotated [`.env.example`](../../.env.example). Secret-handling guidance
lives in [operations/secrets-management.md](../operations/secrets-management.md).

### 3.1 Core variables

| Variable | Default | Required | Notes |
|----------|---------|----------|-------|
| `NAC_ENV` | `development` | no | `development` \| `staging` \| `production` |
| `NAC_HTTP_ADDR` | `:8080` | no | Listen address |
| `NAC_DB_DSN` | — | **yes** | PostgreSQL DSN (`postgres://...`) |
| `NAC_REDIS_ADDR` | — | no | Empty disables policy caching |
| `NAC_JWT_SIGNING_KEY` | — | **yes outside dev** | >= 32 bytes; signs operator JWTs |
| `NAC_SERVICE_TOKEN` | — | **yes outside dev** | >= 24 chars; enforcement-point bearer |
| `NAC_DEFAULT_ACTION` | `deny` | no | Engine fallback when no policy matches |
| `NAC_LOG_LEVEL` | `info` | no | slog level |
| `NAC_LOG_FORMAT` | `json` | no | `json` recommended for production |
| `NAC_BOOTSTRAP_ADMIN_EMAIL` | — | no | First-boot admin (idempotent) |
| `NAC_BOOTSTRAP_ADMIN_PASSWORD` | — | no | First-boot admin password |

Outside `development`, `nacd` **refuses to start** without a strong
`NAC_JWT_SIGNING_KEY` (>= 32 bytes) and `NAC_SERVICE_TOKEN` (>= 24 chars). This
is a deliberate fail-closed default.

> **Security note:** Changing `NAC_DEFAULT_ACTION` away from `deny` weakens
> least-privilege. Leave it at `deny` unless you have a specific, reviewed
> reason.

### 3.2 Generating secrets

```bash
# JWT signing key (>= 32 bytes)
openssl rand -base64 48

# Service token (>= 24 chars)
openssl rand -hex 32
```

The helper [`scripts/gen-secrets.sh`](../../scripts/gen-secrets.sh) generates a
consistent set for you.

### 3.3 Injecting secrets in Kubernetes

Keep secrets out of `config.yaml`. Inject them from your secrets manager (e.g.
External Secrets, Vault, cloud KMS) into a `Secret` consumed by the Deployment:

```bash
kubectl -n nac create secret generic nacd-secrets \
  --from-literal=NAC_DB_DSN='postgres://nac:***@db:5432/nac?sslmode=require' \
  --from-literal=NAC_JWT_SIGNING_KEY="$(openssl rand -base64 48)" \
  --from-literal=NAC_SERVICE_TOKEN="$(openssl rand -hex 32)"
```

Secrets are **never logged**. Do not place them in `ConfigMap` or commit a real
`.env`.

---

## 4. First-boot admin bootstrap

There is no public registration endpoint. The first administrator is created at
startup via the **idempotent bootstrap**:

1. Set `NAC_BOOTSTRAP_ADMIN_EMAIL` and `NAC_BOOTSTRAP_ADMIN_PASSWORD`.
2. Start `nacd`. On first boot it creates an `admin` operator with those
   credentials. If the operator already exists, the bootstrap is a no-op (safe
   to leave configured, but rotating the password afterward is recommended).

After bootstrap, use the admin account to create additional operators
(`operator`, `viewer`) through operator-management APIs. Roles:

| Role | Capabilities |
|------|--------------|
| `admin` | Everything, including operator management and audit read |
| `operator` | Read/write endpoints and policies, evaluate, audit read |
| `viewer` | Read-only on endpoints, policies, and audit |
| (unknown) | Nothing (fail-closed) |

---

## 5. Common admin tasks (curl walkthrough)

Base URL below is `http://localhost:8080`. The full flow is **login → create
policy → register endpoint → evaluate**.

### 5.1 Log in and capture the JWT

```bash
BASE=http://localhost:8080

TOKEN=$(curl -sS -X POST "$BASE/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@nac.local","password":"ChangeMe123!"}' \
  | jq -r .token)

echo "$TOKEN"
```

The response is `{ "token": "...", "expires_at": "...", "role": "admin" }`.
A failed login returns a generic `401` (no user enumeration).

### 5.2 Create policies (worked example)

A common pattern is **allow compliant laptops, quarantine everything else**.
Policies are evaluated in priority order (**lower number = higher precedence**),
first match wins, and **all conditions in a rule are ANDed**. A rule with no
conditions is a catch-all.

First, the high-precedence allow rule:

```bash
curl -sS -X POST "$BASE/api/v1/policies" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "compliant-laptops-allow",
    "description": "Authorize laptops that report a compliant posture.",
    "priority": 10,
    "enabled": true,
    "action": "allow",
    "segment": "corp",
    "conditions": [
      { "field": "device_type", "operator": "eq", "values": ["laptop"] },
      { "field": "posture",     "operator": "eq", "values": ["compliant"] }
    ]
  }'
```

Then the catch-all quarantine fallback at a *lower* precedence (higher number):

```bash
curl -sS -X POST "$BASE/api/v1/policies" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "default-quarantine",
    "description": "Quarantine anything not explicitly allowed above.",
    "priority": 1000,
    "enabled": true,
    "action": "quarantine",
    "segment": "remediation",
    "conditions": []
  }'
```

Because the engine is default-deny, the catch-all is optional: without it,
unmatched devices are denied. Use a catch-all when you prefer to *quarantine*
unknown devices into a remediation segment instead of denying them outright.

Conditions reference resolvable fields: `mac`, `ip`, `hostname`, `device_type`,
`posture`, and custom attributes under `attr.<key>`. Operators are `eq`, `ne`,
`in`, and `exists`. **Unknown operators fail closed** (the condition does not
match).

Example using a custom attribute and the `in` operator:

```bash
curl -sS -X POST "$BASE/api/v1/policies" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "guest-printers-allow",
    "priority": 50,
    "enabled": true,
    "action": "allow",
    "segment": "iot",
    "conditions": [
      { "field": "device_type", "operator": "in",     "values": ["printer","camera"] },
      { "field": "attr.owner",  "operator": "exists",  "values": [] }
    ]
  }'
```

### 5.3 Register and manage endpoints

Endpoints are network devices keyed by MAC. `mac` is required; it is normalized
to lowercase colon form and is **immutable** on update.

```bash
curl -sS -X POST "$BASE/api/v1/endpoints" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "mac": "AA:BB:CC:11:22:33",
    "ip": "10.0.12.50",
    "hostname": "laptop-jdoe",
    "device_type": "laptop",
    "posture": "compliant",
    "access_status": "unknown",
    "attributes": { "owner": "jdoe", "department": "eng" }
  }'
```

List, filter, and search:

```bash
# Paginated list (limit default 50, max 200; offset >= 0)
curl -sS "$BASE/api/v1/endpoints?limit=50&offset=0" -H "Authorization: Bearer $TOKEN"

# Filter by access_status and prefix-search MAC/hostname
curl -sS "$BASE/api/v1/endpoints?access_status=quarantined&q=laptop" \
  -H "Authorization: Bearer $TOKEN"
```

Update (note `mac` cannot change) and delete:

```bash
curl -sS -X PUT "$BASE/api/v1/endpoints/{id}" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{ "posture": "at_risk", "hostname": "laptop-jdoe" }'

curl -sS -X DELETE "$BASE/api/v1/endpoints/{id}" -H "Authorization: Bearer $TOKEN"
```

`access_status` is one of `unknown | authorized | quarantined | denied`;
`posture` is one of `unknown | compliant | at_risk | noncompliant`.

### 5.4 Evaluate access (simulating an enforcement point)

The evaluate endpoint is **enforcement-point facing** and authenticates with the
**static service token** as a bearer (not a JWT):

```bash
SERVICE_TOKEN='dev-service-token-change-me-please-1234'

curl -sS -X POST "$BASE/api/v1/access/evaluate" \
  -H "Authorization: Bearer $SERVICE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "mac": "AA:BB:CC:11:22:33",
    "ip": "10.0.12.50",
    "posture": "compliant",
    "attributes": { "owner": "jdoe" }
  }'
```

The response is an `AccessDecision`:

```json
{
  "endpoint": { "mac": "aa:bb:cc:11:22:33", "hostname": "laptop-jdoe", "...": "..." },
  "status": "authorized",
  "action": "allow",
  "segment": "corp",
  "matched_policy_id": "<uuid>",
  "reason": "matched policy compliant-laptops-allow",
  "evaluated_at": "2026-06-27T12:00:00Z"
}
```

---

## 6. Integrating network enforcement points

Enforcement points (switches, WLCs, RADIUS bridges) call
`POST /api/v1/access/evaluate` with the service token to obtain a verdict for a
device joining the network.

- **Authentication:** static service token (`NAC_SERVICE_TOKEN`) as
  `Authorization: Bearer <token>`. This is *not* a JWT and is compared in
  constant time.
- **Request:** `mac` is required; `ip`, `posture`, and `attributes` are
  optional context that policies can match on.
- **Response:** the `action` (`allow` | `deny` | `quarantine`) and `segment`
  tell the enforcement point how to place the device. `reason` and
  `matched_policy_id` make every verdict explainable.

Operational recommendations:

- Issue a distinct service token per enforcement-point fleet where feasible, and
  rotate on a schedule. See
  [operations/secrets-management.md](../operations/secrets-management.md).
- Terminate TLS at the ingress; never send the service token over plaintext.
- Treat a `503`/`readyz` failure as "fail according to local policy" at the
  enforcement point — `nacd` itself is default-deny, but network gear should
  have a defined fallback.

---

## 7. Reading the audit log

Every state-changing action produces an **append-only** audit event. Audit read
is gated by RBAC (`admin` and `operator` can read; `viewer` can read).

```bash
curl -sS "$BASE/api/v1/audit?limit=50&offset=0" -H "Authorization: Bearer $TOKEN"
```

Pagination follows the platform defaults (`limit` default 50, max 200; `offset`
>= 0). Audit events carry the request/correlation id, enabling you to tie an API
action to its log line and trace. For the data model and retention guidance see
[security/audit-logging.md](../security/audit-logging.md).

---

## 8. Health, readiness, and metrics monitoring

| Endpoint | Auth | Use |
|----------|------|-----|
| `GET /healthz` | none | Liveness — process is up |
| `GET /readyz` | none | Readiness — dependencies reachable |
| `GET /version` | none | Build version/commit/date |
| `GET /metrics` | none | Prometheus metrics |
| `GET /openapi.yaml` | none | Embedded API spec |

```bash
curl -sS "$BASE/healthz"
curl -sS "$BASE/readyz"
curl -sS "$BASE/metrics" | head
```

Scrape `/metrics` with Prometheus and alert on readiness flaps, error rates, and
latency. Dashboards and SLO guidance: [operations/metrics.md](../operations/metrics.md),
[operations/observability.md](../operations/observability.md),
[operations/logging.md](../operations/logging.md),
[operations/tracing.md](../operations/tracing.md).

For capacity and growth, see [operations/performance.md](../operations/performance.md)
and [operations/scalability.md](../operations/scalability.md).

---

## 9. Backup and disaster recovery

PostgreSQL is the **system of record**; Redis is a disposable cache. Your
backup/DR plan must protect the database:

- Take regular logical and/or physical backups of PostgreSQL and test restores.
- Redis can be rebuilt from PostgreSQL — it does not require backup.
- Keep secrets (signing key, service token, DB credentials) in your secrets
  manager with their own backup/rotation lifecycle.

See [operations/backup-strategy.md](../operations/backup-strategy.md) for the
detailed strategy and the [runbooks](../../runbooks/README.md) for procedures.

---

## 10. Further reading

- [User Manual](user-manual.md) — using the API as operator/viewer
- [Developer Guide](developer.md) — building and extending the platform
- [Configuration](../operations/configuration.md) · [Secrets](../operations/secrets-management.md)
- [Security Policy](../../security/README.md) · [Threat Model](../security/threat-model.md) · [Trust Boundaries](../security/trust-boundaries.md)
- [Architecture Overview](../architecture/overview.md) · [Business Logic](../architecture/business-logic.md)
- [Architecture Decision Records](../../adr/README.md)
