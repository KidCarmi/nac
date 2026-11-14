# Threat Model

This threat model follows a STRIDE-per-element approach over the platform's
trust boundaries (see [Trust Boundaries](trust-boundaries.md)). It is a living
document: every feature that changes an entry point, data store, or trust
boundary must update it (see the [Definition of Done](../guides/contributing.md)).

## 1. Scope & assumptions

**In scope:** the `nacd` control-plane service, its REST API, its data stores
(PostgreSQL, Redis), operator authentication/authorization, the
enforcement-point decision endpoint, and the pilot's FreeRADIUS adapter,
switch CoA/eAPI channels and console.

**Out of scope (delegated):** TLS termination (handled by ingress/LB), the
physical network enforcement (handled by switches/WLCs/RADIUS), host/OS
hardening, and the secrets manager itself.

**Assumptions:**
- TLS is terminated in front of `nacd`; plaintext HTTP is never exposed to
  untrusted networks.
- Secrets (JWT key, service token, DB/Redis credentials) are delivered via the
  environment from a secrets manager and are not committed.
- PostgreSQL and Redis are reachable only from the application network.

## 2. Assets

| Asset | Why it matters |
|-------|----------------|
| Operator credentials & sessions | Compromise grants control of network access policy. |
| The policy set | Tampering can grant unauthorized network access. |
| The service token | Allows calling the decision endpoint; leak enables decision probing/abuse. |
| Audit log | Integrity is required for incident investigation and compliance. |
| Endpoint inventory | Reveals network topology and device posture. |

## 3. Entry points (attack surface)

| Entry point | Auth | Notes |
|-------------|------|-------|
| `POST /api/v1/auth/login` | none | Credential intake; rate-limited; generic errors. |
| `/api/v1/endpoints`, `/api/v1/policies`, `/api/v1/audit` | JWT + RBAC | Operator API. |
| `POST /api/v1/access/evaluate` | service token | Machine-to-machine; constant-time token check. |
| `POST /api/v1/radius/{authorize,reject,accounting}` | service token (Basic) | FreeRADIUS adapter over loopback only (pilot). |
| Switch, session, binding, CoA, policy-version and event routes | JWT + operator record + RBAC | Pilot operator API (`network:*`, `session:act`, `policy:*`). |
| `GET /api/v1/auth/me`, `POST /api/v1/auth/password` | JWT + operator record | Self-service; the change needs the current password. |
| `/api/v1/change-requests…` (review-only) | JWT + operator record + RBAC (`policy:read`, mutations `policy:write`) | Proposals only: no route changes policy, sessions, CoAs or switches. Every mutation needs `If-Match: <revision>` and is audited in the same transaction. |
| `/ui/` | none (static) | The console is a client of the same API and holds no privileges of its own. |
| RADIUS UDP 1812/1813 (FreeRADIUS) | per-switch shared secret + EAP-TLS | Only registered switches; requests without EAP are rejected. |
| Host: `nacd.env`, `nac-reset-password` | root | Fully trusted (see Trust Boundaries §12). |
| `/healthz`, `/readyz`, `/version`, `/metrics`, `/openapi.yaml` | none | No sensitive data; metrics carry no secrets. |

Full enumeration: [`security/attack-surface.md`](../../security/attack-surface.md).

## 4. STRIDE analysis

### Spoofing
- **Threat:** Forged JWT to impersonate an operator.
  **Mitigation:** HMAC-SHA256 with a ≥32-byte key; **algorithm is pinned**
  (`alg=none` and RS/HS confusion rejected); issuer, audience, and expiry are
  validated. (`internal/auth/jwt.go`)
- **Threat:** Replaying/guessing the service token.
  **Mitigation:** Constant-time comparison; token length enforced (≥24 chars in
  non-dev); rate limiting; rotate via secrets manager.
- **Threat:** User enumeration via login timing/error differences.
  **Mitigation:** Identical 401 for unknown-user and bad-password; dummy-hash
  verification keeps work constant. (`internal/server/handlers_auth.go`)

### Tampering
- **Threat:** Modifying policies to grant access.
  **Mitigation:** RBAC (write requires operator/admin); all mutations audited;
  DB CHECK constraints reject invalid enums even if a client bypasses the API.
- **Threat:** SQL injection.
  **Mitigation:** All queries are parameterized via `pgx`; search uses bound
  parameters with server-side prefixing, never string interpolation.
- **Threat:** JSON field smuggling / mass assignment.
  **Mitigation:** Strict decoding with `DisallowUnknownFields`; immutable fields
  (e.g. MAC on update) are not accepted.

### Repudiation
- **Threat:** An operator denies performing an action.
  **Mitigation:** Append-only audit log capturing actor, action, resource,
  outcome, request id, and remote address; no update/delete API on audit.

### Information Disclosure
- **Threat:** Leaking internal errors, stack traces, or SQL to clients.
  **Mitigation:** RFC 7807 problem responses; unexpected errors return an opaque
  500 while the detail is logged server-side with a correlation id.
- **Threat:** Secrets in logs.
  **Mitigation:** Logger redacts known secret-bearing keys; password hashes are
  `json:"-"`; secrets are never logged at startup.
- **Threat:** Sensitive data in metrics labels.
  **Mitigation:** Route labels are bounded patterns, not raw paths/IDs.

### Denial of Service
- **Threat:** Request floods / oversized bodies / slowloris.
  **Mitigation:** Per-client rate limiting; `MaxBytesReader` body cap;
  `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`; cached hot path
  keeps the DB off the critical path; HPA scales out.
- **Threat:** Cache outage cascades to the database.
  **Mitigation:** Cache fails open to the DB; short TTL; the DB query is indexed
  and cheap (`policies_enabled_priority_idx`).

### Elevation of Privilege
- **Threat:** Viewer performs writes; operator manages operators.
  **Mitigation:** Per-route permission checks; static, fail-closed RBAC matrix;
  unknown roles grant nothing.
- **Threat:** Enforcement point uses operator credentials (or vice versa).
  **Mitigation:** Separate auth schemes; the service-token endpoint rejects
  JWTs and vice versa (verified by tests).


### Pilot-specific threats (FreeRADIUS, switches, CoA)

| Threat | Mitigation | Residual |
|--------|------------|----------|
| A rejected certificate clears a quarantine or takes over a session on the same port/MAC | Rejects never replace a session or become identity; regression-tested and confirmed in the lab on `fe7c981` | — |
| Spoofed MAC admitted via switch MAB fallback | nacd has no MAB path, and non-EAP requests are rejected before nacd | A switch configured with mac-auth fallback toward **another** RADIUS server can still admit a spoofed MAC (ASTRA MikroTik run). The documentation forbids it |
| Forged CoA-ACK makes NAC report an action that did not happen | Response authenticator verified; CoA "observed" needs a later re-authentication | — |
| nacd outage grants access | Fail closed: FreeRADIUS rejects, no authorization cache | Already-authorized sessions persist on the switch until re-authentication (switch behavior) |
| Stolen operator token outlives a password change | `token_version` checked per request; a change or reset revokes all earlier sessions | Tokens remain valid until expiry if the password is not changed |
| Demoted or disabled operator keeps access | Role and disabled flag read from the operator record per request | — |
| eAPI credentials intercepted | HTTPS; read-only account; sealed at rest | `NAC_EAPI_INSECURE_SKIP_VERIFY=true` (needed for self-signed eAPI) disables certificate checks for all switches |
| FreeRADIUS service token leaked | Loopback-only HTTP; no operator capability | `freeradius -X` debug output prints it |
| Brute force of the current password through a stolen session | Argon2id cost; per-client rate limiting; failures audited | No per-account lockout |

### Review-only change requests (slice A)

These are the threats for the change-request endpoints, written together
with the endpoints, as the implementation plan requires.

| Threat | Mitigation | Residual |
|--------|------------|----------|
| A change request is used to change live policy, a session or a switch | `change.Service` holds only a `PolicyReader` with read methods (reflection test). The package cannot import the enforcement, RADIUS, pilot or storage code (import test). The API test compares policy, versions, draft, sessions, CoAs, switches and decisions before and after exercising every route, and sees no CoA packet | — |
| A reviewed change request is mistaken for authorization, while publish bypasses it | `workflow: review_only` (CHECK-constrained) and a label on every response and page. The API description says policy is published only through the draft | Approval is not mandatory governance yet. Mandatory approvals must later converge publish, restore and activation on one enforced boundary (plan) |
| Lost update: an edit, validation or cancel based on an old view overwrites a newer revision | `If-Match` revision on every mutation (`428` if missing, `409` with the current revision if stale). Compare-and-set under a row lock; tests show one winner among concurrent mutations | — |
| A mutation without an audit record (repudiation) | Each mutation and its `audit_events` row share one transaction; a forced audit failure rolls the mutation back (test) | Reads are not audited |
| History rewritten to hide what was proposed or validated | Revisions and validations are immutable, and change requests are never deleted (database triggers; raw-SQL test) | A database superuser can disable triggers (Trust Boundaries §3) |
| A stale validation is presented as current | Each validation records the content digest and an environment digest. `GET` reports drift (revision, active version, segment mappings) with reasons; nothing is silently invalidated or refreshed | — |
| Injection through title, intent or rule text | Stored and returned as data, never interpreted. The console renders with `textContent` only. Size limits: title 200, intent 4000, 500 rules, request body limit | — |
| Resource exhaustion by many revisions or requests | `policy:write` required; the request body limit; per-client rate limiting | No per-operator quota on change requests |
| A simulation is presented as a measurement of the network | Every simulation and console view carries the "historical replay … not live-network verification" label. Coverage notes say what was not seen | — |
| A stored simulation silently changes, or is recomputed from live data | The input set is persisted before the replay: decision inputs and session overrides are copied, the active version is an immutable reference plus digest, the environment and catalog are copied with digests. Input sets and simulations are immutable (triggers). `…/reproduction` re-runs from the stored set only and compares canonical digests. A test shows that a live Quarantine, a mapping change and a publish do not change the reproduction, and the test fails if reproduction reads live cases | — |
| Copied session evidence leaks through change requests | `…/input-set` requires `network:read`, like `/decisions/{id}`. Simulation rows carry subjects and actions, not raw inputs | Copied inputs (MAC, certificate CN, override) are kept with the immutable input set for as long as the database keeps it; there is no retention policy for change-request history yet |
| A viewer creates or edits proposals | Mutations require `policy:write` (RBAC test); reads require `policy:read` | Validation needs `policy:write` (the proposed contract had `policy:read`), because it records a result and changes the state |

## 5. Residual risks & accepted trade-offs

- **Audit write is best-effort relative to the request.** A transient audit
  sink failure is logged but does not fail the underlying operation. This
  favors availability of legitimate operations; deployments needing strict
  audit-or-fail semantics can tighten this (documented in
  [Audit Logging](audit-logging.md)).
- **In-process rate limiter is per-instance.** Behind N replicas the effective
  limit is N×. A shared Redis limiter is the documented upgrade path
  (`internal/server/middleware.go`, [Scalability](../operations/scalability.md)).
- **MAC-based identity is spoofable** at the network layer. NAC mitigates, not
  eliminates, this; combine with posture and 802.1X certificate identity for
  sensitive segments.

## 6. Security testing

- Unit tests assert fail-closed behavior: alg-none rejection, wrong-key
  rejection, RBAC denials, unknown-operator parity, unknown-operator-fails-closed.
- API tests assert auth separation (operator token rejected on service
  endpoint), unknown-field rejection, and validation responses.
- Run `make lint` (includes `gosec`) and the security review checklist in
  [`security/README.md`](../../security/README.md) before release.
