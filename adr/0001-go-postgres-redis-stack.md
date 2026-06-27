# 0001. Go + PostgreSQL + Redis technology stack

- **Status:** Accepted
- **Date:** 2026-06-27
- **Deciders:** Platform engineering

## Context

We are building a NAC control plane that must be correct, secure, operable, and
horizontally scalable, with a hot decision path called on every device
authentication. We want boring, proven technologies that the broadest pool of
infrastructure engineers can operate, and a single self-contained deployable.

## Decision

- **Language: Go.** Single static binary, excellent concurrency and HTTP
  story, strong standard library, fast builds, first-class container/Kubernetes
  ecosystem, and a culture of explicit error handling that suits
  security-sensitive code.
- **Database: PostgreSQL.** Mature, transactional, rich types (JSONB, partial
  indexes, CHECK constraints) that let us push invariants into the schema.
- **Cache: Redis.** Ubiquitous, simple, ideal for caching the small, slowly
  changing enabled-policy set off the hot path.

## Consequences

### Positive
- One binary, trivial to containerize as distroless/non-root.
- Operators already know how to run Postgres and Redis.
- JSONB + partial indexes match our open-ended `attributes`/`conditions` and
  hot-path query exactly.

### Negative / trade-offs
- Go's verbosity for data mapping (no ORM) means hand-written SQL — accepted as
  a readability/control benefit (see ADR-0005).
- Redis adds an operational dependency; mitigated by making the cache optional
  and fail-open (ADR-0002).

### Alternatives considered
- **Rust:** stronger guarantees but smaller operator pool and slower iteration
  for this team; rejected for time-to-value.
- **MySQL:** weaker JSON and partial-index support; rejected.
- **In-process cache only:** doesn't share across replicas; Redis chosen but
  made optional.
