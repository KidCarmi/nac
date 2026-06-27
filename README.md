# NAC — Network Access Control Platform

An enterprise-grade, open-source **Network Access Control** platform. `nac`
decides whether a device joining the network is authorized, and at what level
(network segment), by evaluating the device's identity and posture against an
ordered set of operator-defined policies. Network enforcement points (switches,
wireless controllers, RADIUS bridges) consult the platform's decision endpoint;
operators manage devices and policies through an authenticated REST API.

> **Status:** Foundation release. The control-plane API, policy engine,
> persistence, authentication/authorization, observability, and deployment
> tooling are implemented, tested, and documented. The React web console is a
> placeholder (`/web`) and tracked as future work.

---

## Why this exists

Most NAC products are closed, expensive, and opaque about *why* a device was
granted or denied access. `nac` is built around a small, pure, fully-audited
**decision engine** whose every verdict is explainable and reproducible, with
the operational rigor (observability, security, runbooks) expected of an
infrastructure product.

## Highlights

- **Deterministic policy engine** — first-match-wins, default-deny, pure and
  exhaustively unit-tested (`internal/access`).
- **Secure by default** — Argon2id password hashing, pinned-algorithm JWTs,
  RBAC, RFC 7807 errors, strict input validation, constant-time secret
  comparison, audit logging, secure-by-default configuration.
- **Boring, proven stack** — Go, PostgreSQL, Redis, `net/http`, `pgx`,
  Prometheus. Minimal dependencies.
- **Operable** — structured logs, Prometheus metrics, W3C trace propagation,
  liveness/readiness probes, graceful shutdown, embedded migrations.
- **Kubernetes-ready** — distroless non-root image, HPA, PDB, hardened
  securityContext, 12-factor configuration.
- **Hardened CI/CD** — coverage gate, Go fuzzing, benchmark-regression gate,
  multi-version migration validation, govulncheck/OSV/CodeQL/Trivy/Grype,
  gitleaks, Checkov/kube-linter, OpenAPI breaking-change detection, OWASP ZAP
  DAST, and signed, SBOM-attested, SLSA-provenanced reproducible releases. See
  [CI/CD & Supply-Chain Security](docs/operations/ci-cd.md).

## Architecture at a glance

```
            ┌──────────────┐        ┌───────────────────────────────┐
 Operators ─┤  REST API    │        │            nacd               │
   (JWT)    │  /api/v1/... ├───────▶│  HTTP edge → services → ports  │
            └──────────────┘        │   ┌─────────────────────────┐ │
 Enforcement┌──────────────┐        │   │  access decision engine │ │
   points  ─┤ /access/     ├───────▶│   │  (pure, default-deny)   │ │
 (svc token)│  evaluate    │        │   └─────────────────────────┘ │
            └──────────────┘        └───────┬───────────────┬───────┘
                                            │               │
                                      ┌─────▼─────┐   ┌──────▼──────┐
                                      │ PostgreSQL│   │    Redis    │
                                      │ (system   │   │ (policy     │
                                      │  of record)│  │  cache)     │
                                      └───────────┘   └─────────────┘
```

See [`docs/architecture/overview.md`](docs/architecture/overview.md) for the
full architecture, component, and sequence diagrams.

## Quick start (local)

Requirements: Docker + Docker Compose, or Go 1.24+ with a local PostgreSQL.

```bash
# Bring up Postgres, Redis, and nacd with development secrets.
make up

# Exercise the API end to end (requires curl + jq).
./scripts/smoke.sh
```

The compose stack bootstraps an admin operator (`admin@nac.local` /
`ChangeMe123!` — development only) and exposes the API on `:8080`.

### Build and test from source

```bash
make build           # build ./bin/nacd
make test            # unit tests with -race + coverage
make test-integration TEST_DSN=postgres://nac:nac@localhost:5432/nac?sslmode=disable
make lint            # golangci-lint
make ci              # tidy + fmt + vet + lint + test
```

## API

The full contract is the embedded OpenAPI 3.1 document, served live at
`GET /openapi.yaml` and committed at
[`api/openapi/openapi.yaml`](api/openapi/openapi.yaml).

| Method | Path | Auth | Description |
|-------|------|------|-------------|
| POST | `/api/v1/auth/login` | none | Operator login → JWT |
| GET/POST | `/api/v1/endpoints` | JWT (RBAC) | List / create endpoints |
| GET/PUT/DELETE | `/api/v1/endpoints/{id}` | JWT (RBAC) | Manage an endpoint |
| GET/POST | `/api/v1/policies` | JWT (RBAC) | List / create policies |
| GET/PUT/DELETE | `/api/v1/policies/{id}` | JWT (RBAC) | Manage a policy |
| POST | `/api/v1/access/evaluate` | service token | **Access decision (enforcement-point facing)** |
| GET | `/api/v1/audit` | JWT (RBAC) | Read audit log |
| GET | `/healthz` `/readyz` `/metrics` `/version` | none | Operational endpoints |

## Repository layout

```
api/            OpenAPI specification (embedded into the binary)
cmd/nacd/       Service entrypoint / composition root
internal/       Application code (not importable externally)
  access/         Pure policy-decision engine + access service
  audit/          Append-only audit recorder
  auth/           Argon2id, JWT, RBAC, request principal
  cache/          Redis + no-op PolicyCache
  config/         12-factor config loading & validation
  domain/         Core types, errors, and ports (interfaces)
  endpoint/       Endpoint lifecycle service
  observability/  Logging, metrics, trace propagation
  policy/         Policy lifecycle service
  server/         HTTP edge: router, middleware, handlers
  storage/postgres/  pgx repositories + embedded migrations
pkg/            Reusable libraries (problem, validation, version)
deployments/    Docker, Compose, Kubernetes
docs/           Architecture, security, operations, guides, database
adr/            Architecture Decision Records
rfcs/           Design proposals
runbooks/       Operational procedures
security/       Attack surface & security policy
test/           Integration tests (build-tagged)
```

## Documentation

Start at the **[Documentation Index](docs/README.md)**. Key entry points:

- [Architecture Overview](docs/architecture/overview.md)
- [Business Logic & Domain Model](docs/architecture/business-logic.md)
- [Threat Model](docs/security/threat-model.md) · [Trust Boundaries](docs/security/trust-boundaries.md)
- [Database Design & ERD](docs/database/schema.md)
- [Administrator Guide](docs/guides/administrator.md) · [Developer Guide](docs/guides/developer.md) · [User Manual](docs/guides/user-manual.md)
- [Runbooks](runbooks/README.md) · [Observability](docs/operations/observability.md)
- [CI/CD & Supply-Chain Security](docs/operations/ci-cd.md)
- [Architecture Decision Records](adr/README.md)

## Contributing

See the [Contributing Guide](docs/guides/contributing.md) and
[Coding Standards](docs/guides/coding-standards.md). All changes must satisfy
the [Definition of Done](docs/guides/contributing.md#definition-of-done):
production code, tests, and documentation move together.

## License

Licensed under the Apache License 2.0. See [`LICENSE`](LICENSE).
