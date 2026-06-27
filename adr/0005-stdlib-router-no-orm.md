# 0005. Standard-library router and no ORM

- **Status:** Accepted
- **Date:** 2026-06-27
- **Deciders:** Platform engineering

## Context

Routing and data access are areas where third-party libraries are common but
add dependency, abstraction, and supply-chain surface. Go 1.22+ added
method-aware pattern routing to `net/http`, and `pgx` provides a fast,
well-maintained PostgreSQL driver without an ORM.

## Decision

- **Routing:** use the standard-library `http.ServeMux` with method+pattern
  syntax (e.g. `GET /api/v1/endpoints/{id}`) and a small, explicit middleware
  chain. No third-party router.
- **Data access:** use `pgx` directly with hand-written, parameterized SQL and
  small scan helpers. **No ORM.**

## Consequences

### Positive
- Minimal dependencies → smaller attack surface, faster builds, less churn.
- SQL is explicit and reviewable; query plans are predictable; no hidden N+1s.
- `r.Pattern` gives bounded-cardinality metric labels for free.

### Negative / trade-offs
- More boilerplate for scanning rows and wiring routes. Accepted: the
  boilerplate is mechanical, centralized in helpers, and easy to audit.
- No automatic migrations from struct definitions; we ship explicit SQL
  migrations and an embedded runner instead (clearer and safer).

### Alternatives considered
- **chi/gin/echo:** ergonomic but unnecessary given stdlib capabilities;
  rejected to minimize dependencies.
- **GORM/sqlc:** ORM hides cost and complicates security review; `sqlc` is a
  reasonable future option for type-safe queries but adds a build step —
  deferred.
