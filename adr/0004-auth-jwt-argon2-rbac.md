# 0004. JWT sessions, Argon2id, and static RBAC

- **Status:** Accepted; revocation trade-off superseded by [ADR-0007](0007-operator-session-revocation.md)
- **Date:** 2026-06-27
- **Deciders:** Platform engineering, security

## Context

The platform has two distinct callers with different needs: human **operators**
managing the system, and machine **enforcement points** asking for decisions.
We need authentication for both, authorization for operators, and password
storage that resists offline cracking.

## Decision

- **Operator sessions: stateless JWT (HS256)** with a ≥32-byte signing key.
  The verifier **pins the algorithm** and validates issuer, audience, and
  expiry. Tokens carry the operator id, email, and role.
- **Passwords: Argon2id** (memory-hard) in PHC string format, with parameters
  encoded in each hash so they can be raised over time. Verification is
  constant-time.
- **Enforcement points: a static service token**, compared in constant time,
  on a dedicated endpoint that rejects operator JWTs (and vice versa).
- **Authorization: a static role→permission matrix** (admin/operator/viewer)
  defined in code, fail-closed for unknown roles, enforced per route.

## Consequences

### Positive
- Stateless JWTs scale horizontally with no shared session store.
- Algorithm pinning defeats `alg=none` and confusion attacks (tested).
- Argon2id resists GPU/ASIC cracking far better than bcrypt/PBKDF2 at
  comparable cost.
- The RBAC model is reviewable in one file and cannot be mutated by input.

### Negative / trade-offs
- Stateless JWTs cannot be revoked before expiry. Mitigated by short TTLs
  (default 1h) and the `disabled` operator flag checked at login. A
  revocation list is a documented future option.
- A single shared service token for enforcement points is coarse; per-device
  credentials (mTLS) are a future enhancement (new ADR when needed).

### Alternatives considered
- **Server-side sessions:** require a shared store and add a hot-path lookup;
  rejected for the stateless property.
- **bcrypt:** acceptable but weaker against memory-hard attacks; Argon2id
  chosen per current OWASP guidance.
