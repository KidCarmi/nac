# Coding Standards

These are the engineering standards for the **NAC** platform. They exist to keep
the codebase **boring, secure, and consistent** — properties that matter more in
infrastructure software than cleverness. They are enforced by review and by
`make ci`.

See also: [Developer Guide](developer.md) (architecture and feature workflow),
[Contributing](contributing.md) (PR flow and Definition of Done).

---

## 1. Formatting and imports

- All code is **`gofmt`-clean**. `make check` fails the build on any unformatted
  file.
- Imports are grouped with **`goimports`** using the local prefix
  `github.com/kidcarmi/nac` (configured in
  [`.golangci.yml`](../../.golangci.yml)): standard library, third-party, then
  local packages.
- Run before pushing:

  ```bash
  make fmt && make vet && make lint
  ```

---

## 2. Errors

- **Wrap with `%w`** against the domain sentinels, never with opaque strings:

  ```go
  if err := repo.Get(ctx, id); errors.Is(err, sql.ErrNoRows) {
      return fmt.Errorf("get endpoint %s: %w", id, domain.ErrNotFound)
  }
  ```

- The canonical sentinels are `ErrNotFound`, `ErrConflict`, `ErrInvalidInput`,
  `ErrUnauthorized`, and `ErrForbidden`. They are **mapped to HTTP status codes
  in `internal/server/render.go`** — handlers return wrapped sentinels and let
  `render.go` translate them.
- Compare with `errors.Is` / `errors.As`, never string matching (`errorlint`
  enforces this).
- **Every error returned by every error-returning call is checked** (`errcheck`).
- **All HTTP error responses are RFC 7807** `application/problem+json` with
  `type`, `title`, `status`, `detail`, `instance` (the request id), and, for
  `422`, a field-level `errors` map. Build them via `pkg/problem`. Never leak
  internal detail (stack traces, SQL, dependency errors) into the response body.

---

## 3. No panics in request handling

- **Never panic in handlers** or in the recoverer middleware path. Return a
  wrapped error and let `render.go` produce the response.
- A top-level recoverer exists as a last resort, not as control flow. Panicking
  to signal a normal error is forbidden.

---

## 4. Context and concurrency

- **Context-first parameters:** `ctx context.Context` is the first argument of
  any function that does I/O, may block, or crosses a boundary.
- Propagate `ctx` to all downstream calls (DB, cache, HTTP). Respect
  cancellation and deadlines.
- Use the **context logger** for structured logging so every line carries the
  request id / trace id. Never construct ad-hoc global loggers in request paths.

---

## 5. Interfaces and package layout

- **Small interfaces, defined by the consumer.** The service that needs a
  capability declares the interface (port) it depends on; the adapter merely
  satisfies it. Do not pre-declare broad "repository" interfaces on the
  implementer side.
- **Package layout rules** (hexagonal; see
  [ADR 0002](../../adr/0002-hexagonal-architecture.md)):
  - `internal/domain` holds core types and ports and **imports nothing
    internal**.
  - Services (`internal/{access,endpoint,policy,audit}`) depend on ports, not on
    adapters (`internal/server`, `internal/storage/postgres`, `internal/cache`).
  - Adapters depend inward and are wired to services only in `cmd/nacd`.
  - The decision engine (`internal/access`) stays **pure** (no I/O).
- Reusable, dependency-free helpers go under `pkg/` (`problem`, `validation`,
  `version`); everything else stays in `internal/`.

---

## 6. SQL and persistence

- **Parameterized SQL only** via `pgx`. Never build queries by string
  concatenation or formatting with user input — this is non-negotiable for
  injection safety and is checked by `gosec`.
- Close every `pgx.Rows`/HTTP body you open (`bodyclose` enforces HTTP bodies).
- Schema changes ship as **new embedded migrations** under
  `internal/storage/postgres/migrations`; never edit an applied migration.

---

## 7. Validation

- **Validation lives in the service layer**, not in handlers or repositories.
  Handlers decode (strict JSON, no unknown fields) and delegate; services
  enforce the domain rules and return `ErrInvalidInput` (rendered as `422` with
  a field-level `errors` map).
- Validate and **bound** all inputs (e.g. pagination `limit` max 200, `offset`
  >= 0; MAC normalized to lowercase colon form).

---

## 8. Security defaults

- **Fail closed.** The policy engine is default-deny, unknown RBAC roles get
  nothing, and unknown condition operators do not match. New code must preserve
  these defaults.
- **Per-route RBAC** with deny-by-default; add authz tests for every new route.
- **No secrets in logs.** Signing keys, service tokens, passwords, and DSNs must
  never be logged or echoed in errors. Compare secrets in constant time.
- Secrets are **required outside development** (`NAC_JWT_SIGNING_KEY` >= 32
  bytes, `NAC_SERVICE_TOKEN` >= 24 chars) — the service refuses to start
  without them.
- For security-relevant changes, complete the checklist in
  [`security/README.md`](../../security/README.md) and update the
  [threat model](../security/threat-model.md).

---

## 9. Observability

- Log via the **context logger** in structured form (`slog`, JSON in
  production). Include the request id; never log secrets.
- **Bound metric label cardinality.** Do not use unbounded values (MACs, IDs,
  request ids, raw paths) as Prometheus label values — they explode cardinality.
  Use bounded labels (route template, method, status class). See
  [operations/metrics.md](../operations/metrics.md).

---

## 10. Comments and naming

- **Comments explain *why*, not *what*.** The code already says what it does;
  comments justify non-obvious decisions, trade-offs, and invariants.
- Exported identifiers have doc comments that begin with the identifier name.
- Names are precise and idiomatic Go: short receiver names, no stutter
  (`policy.Service`, not `policy.PolicyService`), no Hungarian notation.

---

## 11. Testing standards

- **Table-driven tests** are the default. Each case names the scenario and the
  expected result.
- **Unit tests require no infrastructure** and run under `make test` with the
  race detector and coverage. Stub adapters via their ports.
- **Integration tests are build-tagged** (`//go:build integration`) under
  `test/integration/...` and run against a real PostgreSQL via `make
  test-integration TEST_DSN=...`.
- **Tests are required for every feature** (Definition of Done). Cover happy
  paths, error paths (RFC 7807), and authz (deny-by-default).

---

## 12. Dependency policy

- **Minimal and proven.** The approved stack is Go stdlib `net/http`, `pgx` v5,
  `go-redis` v9 (optional), Prometheus, `slog`, `golang-jwt` v5, and `argon2`.
  See [ADR 0001](../../adr/0001-go-postgres-redis-stack.md) and
  [ADR 0005](../../adr/0005-stdlib-router-no-orm.md).
- No ORM; routing uses the Go 1.22+ standard-library `ServeMux`
  (method+path patterns).
- Adding a dependency requires justification in the PR; keep `go.mod` tidy
  (`make tidy`, verified by `make check`). `govulncheck` is reviewed before
  release.

---

## 13. Linters enabled

`make lint` runs `golangci-lint` with the set defined in
[`.golangci.yml`](../../.golangci.yml):

| Linter | Catches |
|--------|---------|
| `errcheck` | Unchecked errors |
| `govet` | Suspicious constructs |
| `ineffassign` | Ineffectual assignments |
| `staticcheck` | Comprehensive static analysis |
| `unused` | Unused code |
| `gofmt` | Formatting |
| `goimports` | Import grouping (local prefix `github.com/kidcarmi/nac`) |
| `misspell` | Common misspellings |
| `bodyclose` | Unclosed HTTP response bodies |
| `errorlint` | Correct error wrapping/comparison |
| `gosec` | Security issues |

Test files relax `gosec` and `errcheck` (weaker randomness and intentionally
ignored errors are acceptable in tests only).

---

## 14. The bar

If a change is harder to review than it is to write, simplify it. Favor clarity,
fail-closed behavior, and tested code over cleverness. When in doubt, match the
surrounding code and ask in review.
