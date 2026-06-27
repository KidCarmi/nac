# 0002. Hexagonal architecture with a pure decision engine

- **Status:** Accepted
- **Date:** 2026-06-27
- **Deciders:** Platform engineering

## Context

The access-decision logic is the most security-critical part of the system and
must be exhaustively testable and auditable. Mixing it with HTTP, SQL, and
caching concerns would make it hard to reason about and slow to test.

## Decision

Adopt a hexagonal (ports-and-adapters) architecture:

- `internal/domain` defines entities and **ports** (repository/cache
  interfaces) and imports no other internal package.
- `internal/access` contains a **pure** `Engine` (no I/O, injectable clock) and
  an access service that depends only on small interfaces.
- Infrastructure (`storage/postgres`, `cache`) and delivery (`server`) are
  **adapters** that depend inward on the domain, never the reverse.

## Consequences

### Positive
- The engine is tested with table-driven unit tests and zero infrastructure.
- Services are tested with in-memory fakes; the HTTP layer with `httptest` and
  fakes — fast, deterministic, no DB required for `go test ./...`.
- Swapping an adapter (e.g. a new cache or datastore) doesn't touch the core.

### Negative / trade-offs
- More interfaces and indirection than a monolithic handler-to-SQL design.
  Accepted: the boundaries are few and the testability payoff is large.

### Alternatives considered
- **Transaction-script handlers calling SQL directly:** simpler initially but
  couples business rules to transport and storage and resists unit testing;
  rejected.
