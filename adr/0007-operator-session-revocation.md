# 0007. Operator sessions are revocable through a per-operator token version

- **Status:** Accepted (supersedes the revocation trade-off of ADR-0004)
- **Date:** 2026-11-07
- **Deciders:** pilot build owner (OPUS), repository owner

## Context

ADR-0004 kept operator JWTs fully stateless. A disabled operator's tokens
stayed valid until they expired, and the role in the token was trusted. The
pilot adds a self-service password change and a host-local password reset.
After either one, every earlier session of that operator must stop working
at once. A stolen token must not outlive a password change.

## Decision

- Every token carries `tv`, the operator's `token_version` (migration
  0005).
- On every request, `requireOperator` loads the operator record. It rejects
  unknown or disabled operators and any token whose `tv` differs from the
  record, and it enforces the record's current role.
- A password change (`POST /api/v1/auth/password`, which requires the
  current password) and `nac-reset-password` both increment
  `token_version`. The change returns a fresh token for the session that
  made it.
- Tokens issued before migration 0005 read `tv` as 0, which matches the
  column default, so upgrading signs nobody out.

## Consequences

### Positive
- A password change, a reset, a disablement or a demotion takes effect on
  the next request.
- Lab-verified on pilot-rc9: after a change, the old token and the old
  password are rejected, and the reset revokes the active token.

### Negative / trade-offs
- One indexed primary-key lookup per operator request.
- Revocation is per operator, not per token: it signs out all of that
  operator's sessions.

### Alternatives considered
- **`jti` denylist.** Rejected: it needs a store and cleanup, and it cannot
  express "everything before now" as simply.
- **Short TTL only (ADR-0004).** Rejected: it leaves a window after a
  password change.
