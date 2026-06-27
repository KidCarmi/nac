# Developer Guide

This guide is for engineers building and extending the **NAC** platform. It
covers prerequisites, getting started, the repository layout, the hexagonal
architecture rules, how to add a feature end to end, testing, tooling, the
Definition of Done, and debugging with correlation ids.

If you are deploying or operating the platform, see the
[Administrator Guide](administrator.md). For day-to-day API usage, see the
[User Manual](user-manual.md). Before sending a PR, read
[Contributing](contributing.md) and [Coding Standards](coding-standards.md).

---

## 1. Prerequisites

- **Go 1.24+** (module `github.com/kidcarmi/nac`).
- **Docker / Docker Compose** for the local stack (PostgreSQL + Redis + `nacd`).
- **`golangci-lint`** for linting.
- **`curl`** and **`jq`** for the smoke script.

The stack is intentionally **boring and proven**: Go standard-library
`net/http`, PostgreSQL via `pgx` v5, optional Redis via `go-redis` v9,
Prometheus, `slog`, `golang-jwt` v5, and `argon2`.

---

## 2. Getting started

```bash
# Bring up Postgres + Redis + nacd locally.
make up

# Run unit tests with the race detector and coverage.
make test

# Exercise the running API end to end (needs curl + jq).
./scripts/smoke.sh

# Tear the stack down (DESTROYS volumes).
make down
```

The Compose stack seeds a development bootstrap admin (`admin@nac.local` /
`ChangeMe123!`) and a dev service token
(`dev-service-token-change-me-please-1234`). These are **development only**.

Useful targets (run `make help` for the full list):

| Target | Action |
|--------|--------|
| `make build` | Build `./bin/nacd` |
| `make run` | `go run ./cmd/nacd` (expects Postgres/Redis reachable) |
| `make test` | Unit tests (`-race` + coverage) |
| `make test-integration` | Integration tests against `TEST_DSN` |
| `make cover` | Total coverage from the last test run |
| `make lint` | `golangci-lint run ./...` |
| `make fmt` / `make vet` / `make tidy` | Formatting / vetting / `go mod tidy` |
| `make migrate` | Apply embedded migrations (`nacd -migrate`) and exit |
| `make ci` | `tidy + fmt + vet + lint + test` |
| `make check` | Verify `go.mod` tidy and code formatted (CI gate) |

---

## 3. Repository layout

```
cmd/nacd                 Composition root (wiring, flags, -migrate)
internal/
  domain                 Core types + ports (interfaces). Imports nothing internal.
  access                 Pure decision engine + access service
  endpoint               Endpoint service
  policy                 Policy service
  audit                  Audit service
  auth                   Password (argon2id), JWT (pinned HS256), RBAC, context principal
  cache                  Redis + noop cache implementations
  config                 Environment configuration loading
  observability          Logging / metrics / tracing
  server                 Router + middleware + handlers + render (RFC 7807 mapping)
  storage/postgres       pgx repositories + embedded migrations
pkg/
  problem                RFC 7807 problem+json helpers
  validation             Input validation helpers
  version                Build version metadata
api/openapi              Embedded OpenAPI spec (served at /openapi.yaml)
deployments/             Docker, Compose, Kubernetes
docs/, adr/, rfcs/, runbooks/, security/   Documentation and decisions
```

---

## 4. Hexagonal architecture rules

The platform follows **hexagonal (ports & adapters)** architecture. The
authoritative decision is [ADR 0002](../../adr/0002-hexagonal-architecture.md).

The dependency rule is strict and **one-directional**:

```
  server (HTTP adapter)            storage/postgres (DB adapter)
        │                                   │
        ▼                                   ▼
  ┌──────────────────────────────────────────────────┐
  │   services: access / endpoint / policy / audit    │
  └──────────────────────────────────────────────────┘
        │
        ▼
  ┌──────────────────────────────────────────────────┐
  │   domain: types + ports (interfaces)              │
  │   imports NOTHING internal                        │
  └──────────────────────────────────────────────────┘
```

Rules:

- **`internal/domain` imports nothing internal.** It defines the core types and
  the **ports** (interfaces) that the rest of the system depends on.
- **Services depend on ports, not adapters.** A service never imports
  `storage/postgres` or `server`; it accepts a repository interface (defined as
  a port) and is wired to a concrete adapter in `cmd/nacd`.
- **Adapters depend inward.** `storage/postgres` implements domain ports;
  `server` invokes services. Neither is imported by the services or domain.
- **The decision engine (`internal/access`) is pure.** It has no I/O and is
  exhaustively unit-tested. See
  [ADR 0003](../../adr/0003-default-deny-policy-engine.md).
- **`cmd/nacd` is the only composition root.** All concrete wiring of adapters
  to services happens there.

Interfaces are small and **defined by the consumer** (the service that needs
them), not by the implementer.

---

## 5. Adding a feature end to end

Every feature flows inward-out, then back out through the adapters. Use this
checklist (and keep tests alongside each step):

1. **Domain type** — add or extend the type in `internal/domain`. Keep it free
   of internal imports and infrastructure concerns.
2. **Port** — declare/extend the interface the service needs (e.g. a repository
   method) in `internal/domain`.
3. **Repository (adapter)** — implement the port in `internal/storage/postgres`
   using **parameterized SQL only** (`pgx`). If schema changes are needed, add a
   new embedded migration under `internal/storage/postgres/migrations` (never
   edit an applied migration).
4. **Service + tests** — implement business logic and validation in the service
   package (`internal/{endpoint,policy,access,audit}`). **Validation lives in
   the service layer.** Add table-driven unit tests with no infrastructure.
5. **Handler + route** — add the HTTP handler in `internal/server`, register it
   on the Go 1.22+ `ServeMux` (method+path patterns), and enforce RBAC on the
   route. Map errors with `server/render.go` so responses are RFC 7807.
6. **OpenAPI** — update the embedded spec in `api/openapi` so
   `GET /openapi.yaml` stays accurate.
7. **Docs** — update the relevant guide(s), architecture, and operations docs in
   the **same change** (see the Definition of Done).
8. **CHANGELOG** — add an entry to [`CHANGELOG.md`](../../CHANGELOG.md).

Wire the new adapter to the service in `cmd/nacd`.

---

## 6. Testing approach

Tests are **required for every feature**.

### 6.1 Unit tests (no infrastructure)

- Live next to the code (`_test.go`), use **table-driven** style, and require no
  database or network.
- The decision engine and services are tested purely; repositories are stubbed
  via their ports.

```bash
make test           # go test -race -coverprofile=coverage.out ./...
make cover          # total coverage
```

### 6.2 Integration tests (build-tagged)

- Tagged `//go:build integration` under `test/integration/...` and run against a
  real PostgreSQL pointed to by `TEST_DSN`.

```bash
make test-integration TEST_DSN='postgres://nac:nac@localhost:5432/nac?sslmode=disable'
```

The Makefile passes the DSN through as `NAC_TEST_DB_DSN` to the integration
suite.

### 6.3 API smoke

`./scripts/smoke.sh` drives the running API (login → policy → endpoint →
evaluate) and is a quick post-deploy sanity check.

---

## 7. Formatting, vetting, and linting

```bash
make fmt     # go fmt ./...
make vet     # go vet ./...
make lint    # golangci-lint run ./...
make check   # CI gate: go.mod tidy + gofmt clean
make ci      # full local pipeline: tidy + fmt + vet + lint + test
```

Formatting uses `gofmt` plus `goimports` with the local prefix
`github.com/kidcarmi/nac` (configured in
[`.golangci.yml`](../../.golangci.yml)). Enabled linters include `errcheck`,
`govet`, `staticcheck`, `unused`, `errorlint`, `bodyclose`, and `gosec`. Full
style rules are in [Coding Standards](coding-standards.md).

---

## 8. Definition of Done

A change is **done** only when it produces production code plus unit,
integration, and API tests; updates every affected artifact (README,
architecture, threat model, OpenAPI, user/admin guides, runbooks, migration
docs, diagrams, ADRs, CHANGELOG) as applicable; contains **no TODOs or
placeholders**; and passes `make ci` green. Security-relevant changes must also
clear the security checklist. The authoritative, normative definition lives in
[Contributing → Definition of Done](contributing.md#definition-of-done).

---

## 9. Debugging with request_id / trace_id

`nacd` uses **structured logging via a context logger**. Every request is
assigned a correlation id that surfaces in three places:

- The **`instance`** field of every RFC 7807 error body equals the **request
  id**.
- The same id appears in the structured log line for the request.
- W3C trace propagation ties the request to a `trace_id` for distributed
  tracing.

To debug a failing request:

1. Capture the `instance` (request id) from the `problem+json` response.
2. Grep the JSON logs for that id to find the full structured context.
3. Correlate with the trace via `trace_id` for cross-service timing.

```bash
# Example: pull the request id out of an error response.
curl -sS -X POST "$BASE/api/v1/policies" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{}' | jq '{title, status, instance, errors}'
```

See [operations/logging.md](../operations/logging.md) and
[operations/tracing.md](../operations/tracing.md) for the field schema and trace
setup.

---

## 10. Code-style notes (summary)

- Errors are wrapped with `%w` against domain sentinels
  (`ErrNotFound`, `ErrConflict`, `ErrInvalidInput`, `ErrUnauthorized`,
  `ErrForbidden`) and mapped to HTTP in `server/render.go`.
- **No panics** in handlers or the recoverer path.
- **Context-first** function parameters.
- **Parameterized SQL only**; no string-built queries.
- Comments explain **why**, not what.
- **Fail-closed** security defaults (default-deny engine, unknown roles get
  nothing, unknown operators don't match).

Full rules: [Coding Standards](coding-standards.md).
