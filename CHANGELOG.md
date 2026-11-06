# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### pilot-rc4 (supersedes pilot-rc3 `74e5a1e`, which was never qualified)
- **Security: quarantine bypass through a failed attempt** (Codex review of
  rc3). After an accounting Stop, one deliberately failed EAP-TLS attempt
  created an identity-less session, and the next valid login reused it
  without inheriting the override, so policy decided. Inheritance now
  applies whenever a session first acquires a validated identity, not only
  when it is created.

### pilot-rc3 (`74e5a1e`, superseded before qualification; after pilot-rc2 `c121a95`)

Fixed (from ASTRA's rc2 qualification):
- **Quarantine lost on session replacement.** With switch accounting on, the
  Stop sent before a CoA re-authentication ended the session, and the new
  session had no operator override, so the endpoint got corp. The override
  now follows the identity on the port (switch + port + MAC + certificate)
  until Restore, and the CoA is correlated with the replacement session.
  Affects pilot-rc1 and pilot-rc2 whenever accounting is enabled.
- **eAPI TLS handshake failure** with stock EOS 4.29.2F (RSA key exchange
  only). Adds `NAC_EAPI_TLS_ALLOW_RSA_KEX`, which applies to the eAPI client
  only, and a Probe error that names the fix.
- **Accounting NAC cannot attribute** is shown under Events instead of being
  dropped silently.

Added:
- Operator password change (`POST /api/v1/auth/password`, console
  **Account**). It revokes the operator's earlier sessions (token version).
  Host-local recovery with `nac-reset-password`. `GET /api/v1/auth/me`.
  The operator record is authoritative on every request (current role,
  disabled operators).
- OpenAPI 1.1.0 covering every route, with permissions and compatibility
  rules, and a test that keeps it complete.
- Docs: integration boundary (versioned API, server-side authorization,
  attribution), pilot trust boundaries and threat model, qualification
  status, eAPI/accounting setup.

Changed and fixed:
- Re-running `install.sh` keeps operator settings in `nacd.env` and lines
  added to it, and never writes an initial password that does not match the
  account.
- Paged lists (including audit) apply a default and a maximum. Failed logins
  are audited. Switch creation no longer returns 201 with an empty body.
  Restore accepts an empty chunked body.

### pilot-rc2 (`c121a95`, after pilot-rc1 `fe7c981`)
- Publish and restore read only through their own transaction.
- eAPI observation covers every active session of a switch.
- The session timeline keeps the newest events.
- Preview applies the operator quarantine override.
- An e2e check that MAB requests never reach a NAC decision, and a warning
  against MAB fallback.

### Added: wired pilot (pilot-rc1)
- **Supported RADIUS integration**: a FreeRADIUS 3.2.5 adapter
  (`deployments/freeradius`) validates EAP-TLS and asks nacd for each
  decision. Identity bindings map certificate issuer and serial to a device.
  Decision inputs carry source, freshness and status. There is no
  authorization cache (fail closed), and outage rejects are imported from the
  FreeRADIUS outage log.
- **Switch onboarding**: registration with configurable VLANs, secrets
  encrypted at rest, the EOS 4.29.2F configuration generator, the FreeRADIUS
  client file, and separate configured / reachable / auth-seen / verified
  status. Optional eAPI session observation.
- **Session troubleshooting**: requested vs switch-observed state, liveness,
  decision inputs, rule trace, timeline and explanation.
- **Versioned policies**: draft, validation, preview over recorded inputs,
  atomic publication by digest, restore, and immutable history.
- **Active-session CoA**: quarantine, restore and re-authenticate, with
  requested / sent / acked / nak / timeout / failed / observed states and
  retries that never re-target.
- **Operator console** embedded in nacd at `/ui/`.
- **Installer**: Ubuntu 24.04 `install.sh`, systemd units, `make pilot-bundle`,
  and an HTTPS operator listener.
- Docs: `docs/pilot/` (installation, walkthrough, known limitations, ASTRA
  contract).

### Fixed
- `cmd/nacd` was never committed (excluded by `.gitignore`), so `make build`
  failed.
- Missing evidence could satisfy a rule: `ne` matched an absent field, and
  posture `unknown` was comparable. Conditions are now three-valued.
- Validation errors now return their reason instead of a generic message.

### Added (foundation)
- **Control-plane API (`nacd`)** with REST endpoints for endpoints, policies,
  access evaluation, audit, and authentication.
- **Access-decision engine** (`internal/access`): pure, deterministic,
  first-match-wins with secure default-deny.
- **Authentication & authorization**: Argon2id password hashing, HMAC-SHA256
  JWT operator sessions with pinned algorithm, role-based access control
  (admin/operator/viewer), and a static service token for enforcement points.
- **PostgreSQL persistence** via `pgx` with an embedded, transactional
  migration runner and the initial schema (operators, endpoints, policies,
  audit_events).
- **Redis policy cache** with a no-op fallback that preserves correctness.
- **Observability**: structured JSON logging (slog) with secret redaction,
  Prometheus metrics, and W3C trace-context propagation.
- **Audit logging**: append-only record of security-relevant actions.
- **Operational endpoints**: `/healthz`, `/readyz`, `/metrics`, `/version`.
- **Deployment**: distroless non-root Docker image, Docker Compose stack, and
  hardened Kubernetes manifests (Deployment, Service, HPA, PDB, ConfigMap,
  Secret template).
- **CI**: lint, vet, unit tests with race detector, integration tests against
  PostgreSQL, and container build.
- **Documentation**: architecture, threat model, database design, runbooks,
  guides, ADRs, and OpenAPI 3.1 specification.

[Unreleased]: https://github.com/kidcarmi/nac/commits/main
