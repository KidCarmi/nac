# Integration boundary (standalone product, future Culvert reuse)

The NAC pilot is a **standalone product**: nacd, FreeRADIUS and the embedded
console install and run on their own. This document defines the boundary a
future integration, such as the Culvert UI, uses to reuse the NAC backend
**without moving NAC business logic into another UI**. It records what holds
today, the rules that keep it true, and what is still missing.

## 1. Where the business logic lives

All NAC decisions and workflows are in nacd:

| Concern | Package |
|---------|---------|
| Authorization decision, evidence and freshness | `internal/access`, `internal/domain/evidence.go` |
| RADIUS integration (authorize, reject, accounting, correlation) | `internal/radiusauth` |
| Versioned policy (draft, validate, preview, publish, restore) | `internal/policyver` |
| Switch onboarding, sessions, bindings, CoA, eAPI observation, outage import | `internal/pilot` |
| Authentication, RBAC, token revocation | `internal/auth`, `internal/server/middleware.go` |

The console (`web/ui/`) is a **thin client**:

- It is static HTML/JS, embedded in nacd and served at `/ui/`.
- Every action it takes is one call to the documented API: a single `api()`
  helper that prefixes `/api/v1`, plus `POST /api/v1/auth/login`.
- There are no console-only endpoints and no server-rendered pages.
- What the console shows, such as session explanations, liveness, CoA state,
  preview rows and validation messages, is computed by nacd and returned by
  the API.

So another UI built on the same API shows the same results and enforces
nothing on its own.

## 2. The contract: versioned, documented API

- **Specification:** `api/openapi/openapi.yaml` (OpenAPI 3.1). It is embedded
  in the binary and served at `GET /openapi.yaml`, so a running nacd always
  serves the contract it implements.
- **Coverage is enforced:** `internal/server/openapi_coverage_test.go` fails
  if a registered route is missing from the spec, or if the spec documents a
  route the server does not register.
- **Versioning:**
  - `/api/v1` is the compatibility boundary. Within v1, changes are additive
    (new endpoints, new response fields, new optional request fields), and
    clients must ignore unknown fields.
  - Removing a field or endpoint, or changing what an existing one means,
    requires `/api/v2`. `info.version` in the spec records the revision.
- **Errors:** RFC 7807 `application/problem+json`. Validation failures are
  422, with a human-readable `detail` and per-field `errors`.

## 3. Server-side authorization

- Every operator request carries a bearer JWT. nacd verifies it, then loads
  the operator record and rejects unknown, disabled or revoked principals.
  The role it enforces is the one in the record, not the one in the token.
  See [AuthN & AuthZ](../security/authn-authz.md).
- Each operation requires one RBAC permission, listed in the spec as
  `x-required-permission`, and nacd checks it on every request. A client
  hiding a button is a convenience only.
- Machine credentials are separate. The FreeRADIUS service token
  authorizes only `/api/v1/radius/*` and `/api/v1/access/evaluate`, and grants
  no operator capability. **An integration must never hold it.**

## 4. Explicit operator attribution

nacd takes the actor from the authenticated principal, never from a request
body, so no client can act under another operator's name.

| Action | Where the operator is recorded |
|--------|--------------------------------|
| Switch create, update, delete, verify, secret reveal | audit log (`switch.*`) |
| Identity binding create, delete | `identity_bindings.created_by`, audit (`binding.*`) |
| CoA quarantine, restore, re-authenticate | `coa_requests.created_by`, `radius_sessions.override_by`, audit (`session.*`) |
| Policy publish, restore | `policy_versions.created_by`, `policy_activations.activated_by`, audit (`policy.*`) |
| Policy draft edits, devices | audit (`policy.*`, `endpoint.*`) |
| Login, password change | audit (`operator.login`, `operator.password_change`) |
| Host password reset | audit (`operator.password_reset`, actor `host:nacd -reset-password`) |

The audit log (`GET /api/v1/audit`) records actor id, actor email, action,
resource, outcome, request id and remote address.

## 5. How an integration connects today

Supported now:

1. Each person acting through the integration has a NAC operator account
   with the right role.
2. The integration obtains that operator's token with
   `POST /api/v1/auth/login` and calls the documented `/api/v1` operations
   with it. Every action is then authorized and attributed per person.
3. The integration can call nacd from its own backend (recommended), or
   from a browser served under the **same origin** (for example behind one
   reverse proxy). nacd sends no CORS headers, so cross-origin browser calls
   are refused by the browser.

Not supported yet (needed before Culvert integration):

- **Delegated identity / SSO.** Operators are local NAC accounts. Mapping
  Culvert users to NAC operators (for example OIDC with role claims) needs
  a design decision and an API addition, and must keep per-person
  attribution. A shared "Culvert" operator account would erase
  attribution, so don't use one.
- **Operator management API.** One bootstrap admin per installation, plus
  self-service password change. Creating, disabling and assigning roles to
  operators is not exposed.
- **CORS.** Only needed if a Culvert browser app calls nacd cross-origin.
  It would need an explicit origin allowlist. It is not enabled by default.
- **Push notifications.** No webhooks or streaming. Clients poll
  (`/api/v1/auth-events`, sessions, CoA state).

## 6. Rules that keep the boundary

- New behavior goes into a nacd service and is exposed through `/api/v1`
  first. The console then uses it like any other client.
- Every new route is added to the spec in the same change (the coverage
  test fails otherwise), with its `x-required-permission`.
- Never trust a client for authorization, for the actor, or for derived
  state such as decisions, liveness or CoA outcome.
