# Testing Strategy

This document describes how the NAC platform is tested: the shape of the test
suite, what each layer covers, the commands to run them, the security-focused
tests that protect the access-control guarantees, and the Definition of Done for
adding tests with a change. The overarching principle is that **the vast
majority of the suite runs with no infrastructure**, while a smaller,
build-tagged integration layer exercises the real database.

## The test pyramid

The suite is layered so that fast, dependency-free tests dominate and slow,
infrastructure-bound tests are few and explicit.

```
        /  integration (real Postgres, build-tagged)  \      few, explicit
       /------------------------------------------------\
      /        HTTP API tests (httptest + fakes)         \    many
     /----------------------------------------------------\
    /     service-level tests (in-memory fakes)            \   many
   /--------------------------------------------------------\
  /  pure unit tests: engine, auth, validation, config       \  most
 /------------------------------------------------------------\
```

### Pure unit tests

The base of the pyramid. These have no I/O and no infrastructure, and cover the
parts of the system where correctness matters most:

- **The access engine** (`internal/access/engine.go`) — first-match-wins by
  ascending priority, all-conditions-AND, default-DENY when nothing matches
  (configurable via `NAC_DEFAULT_ACTION`), unknown operators failing closed, and
  determinism. Because the engine performs no I/O, it is exhaustively testable in
  pure unit tests and is also the right target for benchmarks (see
  [performance.md](performance.md)).
- **Auth** — argon2 password hashing, JWT handling, and RBAC.
- **Validation** — request and input validation, including rejection of unknown
  fields.
- **Config** — startup configuration loading and validation, including the
  aggregated-error behaviour and the stricter secret requirements in
  non-development environments.

### Service-level tests

The service layer is tested with **in-memory fakes** standing in for the
repositories, so business logic is verified without a database. This keeps the
tests fast and deterministic while still exercising the real service code paths.

### HTTP API tests

The HTTP layer is tested with `net/http/httptest` against the real handler chain
backed by fakes. These verify routing, status codes, the middleware chain
(including auth and the body cap), serialization, and error responses — without
binding a real listener or touching a database.

### Integration tests

The top of the pyramid. Integration tests live in
`test/integration/postgres_test.go`, are guarded by the `integration` build tag,
and require a real database via `NAC_TEST_DB_DSN`. They exercise the real pgx v5
repositories together with the embedded migrations against a live PostgreSQL,
which is the only way to validate SQL, the migration runner, and the partial
index behaviour end to end.

## Commands

| Goal | Command |
|------|---------|
| Run the whole dependency-free suite with the race detector | `go test -race ./...` |
| Run integration tests (requires `NAC_TEST_DB_DSN`) | `go test -tags=integration ./test/integration/...` |
| Integration via make | `make test-integration` |
| Apply migrations for a local integration database | `make migrate` |

Integration tests **do not run** unless the `integration` build tag is supplied,
so a plain `go test ./...` is safe to run anywhere with no database present.

## What must NOT require infrastructure

A hard rule: **everything except the integration layer must run with no
infrastructure.** The pure unit tests, the service-level tests (with in-memory
fakes), and the HTTP API tests (with `httptest` and fakes) must all pass on a
clean machine with nothing but the Go toolchain. Only the build-tagged
integration tests may require a database, and they must be opt-in via the tag and
`NAC_TEST_DB_DSN`. This keeps the inner development loop and the unit CI job fast
and hermetic.

## Coverage expectations

- Run unit tests **with the race detector** (`-race`) and collect coverage; the
  CI `test` job runs unit tests with race and coverage.
- The highest-value code — the pure access engine and the auth primitives —
  should be covered thoroughly, including the failure-closed branches
  (unknown operators, default DENY, alg-none rejection, RBAC denials).
- Treat the engine's correctness branches as non-negotiable coverage: every
  decision rule and every fail-closed path should have an explicit test.
- **Enforced gate:** the `test` job runs `scripts/check-coverage.sh`, failing
  the build if total statement coverage drops below `MIN_COVERAGE` (currently
  60%). The threshold is ratcheted **up, never down**. Run locally with
  `make cover-gate`.

## Fuzz testing

Go native fuzzing protects the parsers on the trust boundary. Targets:
`FuzzNormalizeMAC` (`pkg/validation`), `FuzzTokenParse` and
`FuzzVerifyPassword` (`internal/auth`), and `FuzzEvaluateBody`
(`internal/server`, asserts the API never returns 5xx for arbitrary bodies).
CI runs each for 60s per PR; discovered crashers are uploaded and become seed
corpus. Run locally with `make fuzz` (`FUZZTIME=30s` by default).

## Performance regression

`BenchmarkEvaluate` measures the decision hot path. The `perf.yml` workflow
compares the PR against the base branch with **benchstat** and fails on a
median regression over 20%. Run locally with `make bench`.

## CI mapping

The pipeline maps directly onto this strategy (see
[CI/CD & Supply-Chain Security](ci-cd.md) for the full picture):

| CI job (`ci.yml`) | What it runs |
|--------|--------------|
| **verify** | `make check`, `go vet`, and `golangci-lint` |
| **test** | Unit tests with `-race`, coverage, and the **coverage gate** |
| **fuzz** | Each fuzz target for 60s (matrix) |
| **integration** | Integration tests against a PostgreSQL **service container** |
| **build-image** | Builds the container image |

Additional workflows extend the gate: `security.yml` (govulncheck, OSV, gosec,
gitleaks, Trivy, Grype), `codeql.yml`, `migrations.yml` (PostgreSQL 15/16/17),
`perf.yml`, `openapi.yml`, `iac.yml`, `dast.yml`, `scorecard.yml`, and
`release.yml` (signed, attested, reproducible releases).

Linting is configured in `.golangci.yml` (v2) and enables `errcheck`, `govet`,
`ineffassign`, `staticcheck`, `unused`, `gofmt`, `goimports`, `misspell`,
`bodyclose`, `errorlint`, and `gosec`. Notably `gosec` (security) and
`bodyclose` (resource-leak) run on every change, so security and
resource-hygiene checks are part of the standard gate.

## Security tests

Because this is an access-control platform, several tests exist specifically to
protect security invariants. These must never regress:

- **`alg=none` rejection.** JWTs presented with the `none` algorithm are
  rejected, preventing signature-stripping forgery.
- **RBAC denials.** Operators are denied actions outside their role; the denial
  paths are tested, not just the allow paths.
- **Auth separation.** The two authentication mechanisms are kept distinct — the
  static `NAC_SERVICE_TOKEN` (constant-time compared) guards the machine-facing
  evaluate path, while operator JWT/RBAC guards the administrative path — and
  tests confirm one cannot be used in place of the other.
- **Unknown-field rejection.** Requests carrying unknown fields are rejected
  rather than silently accepted, closing a class of input-confusion bugs.
- **Fail-closed engine behaviour.** Unknown operators and the no-match default
  resolve to DENY, verified by the engine unit tests.

## Adding tests: Definition of Done

A change is not done until its tests are in place at the right layer:

1. **Pick the lowest layer that proves the behaviour.** Pure logic gets a unit
   test; service logic gets a service test with fakes; an HTTP contract gets an
   `httptest` test. Reserve integration tests for things that genuinely need a
   real database (SQL, migrations, indexes).
2. **Keep new tests infrastructure-free unless they belong in the integration
   layer**, in which case put them behind the `integration` build tag in
   `test/integration` and have them read `NAC_TEST_DB_DSN`.
3. **Cover the failure-closed path**, not just the happy path — especially for
   anything touching authorization, the engine, or input validation.
4. **Ensure the suite passes under `-race`** and that `golangci-lint` (including
   `gosec` and `bodyclose`) is clean.
5. **Confirm the relevant CI job covers the new test** (unit in `test`,
   database-backed in `integration`).

## Related documents

- [performance.md](performance.md) — benchmarking the pure engine.
- [observability.md](observability.md) — signals exercised under test.
- [release-process.md](release-process.md) — CI gates and the pre-release
  security checklist.
