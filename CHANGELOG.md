# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### pilot-rc11 (proposed; not yet qualified; rc10 `b39649f` stays the tested baseline)
- **Console CoA confirmation wording only.** The text no longer says
  "about 20 s" or "at its next DHCP renewal". It now states the measured
  lab behavior, conditional on the tested setup:
  - same-VLAN interruptions of up to about 58 s on vEOS-lab 4.29.2F
  - Windows Server 2025 got a new address within about 65 s
  - a Linux (systemd-networkd) endpoint only at DHCP lease expiry

  The panel hint under "Change access (CoA)" now says the timing depends on
  the endpoint's DHCP client and lease.
- `scripts/ui-walkthrough.js` checks the new wording.
- No API, decision, CoA packet or installer change.

### Open after pilot-rc10
- **Single fresh-instance all-console workflow: PASS** (ASTRA, `af46f02`).
  This closes the coverage gap. It also lab-tested pre-change token
  revocation (401).
- **vEOS CoA limitation (ASTRA), not a NAC defect.** On vEOS-lab 4.29.2F,
  an ACKed, VLAN-bearing CoA (the form NAC sends) interrupted the endpoint
  for up to about 58 s, even when the VLAN did not change. Physical switches
  and other CoA forms are unverified. An identical CoA sent from outside NAC reproduces it. vEOS
  refuses a CoA without VLAN attributes (NAK 404). No product change; rc10
  stays pinned. Next candidate: correct the console's "about 20 s" warning.
- A saved rc10 switch configuration survived a switch reboot without
  reapplication.
- **Operating envelope measured (ASTRA, 1-hour lease):** Windows Server 2025
  build 26100 recovered in about 30–65 s at the tested leases. Linux (systemd-networkd)
  recovers only at lease expiry (1,811.9 s measured), because the lab DHCP
  server ignores rebinds for an address from another subnet. The docs now
  state which deployments this supports.

### pilot-rc10 (`b39649f`; qualified by ASTRA closeout 2026-11-07, including Windows Server 2025 and the console Account page)
- **Endpoint reachability is a separate, explicit fact.** Session views
  (API `endpoint_reachability`, always `not_observed`) and the console's
  third session panel keep it apart from switch authorization and the
  switch's observed VLAN.
- **Verified eAPI TLS documented** (installation §7a): `SSL_CERT_FILE` with
  the switch CA, and a switch certificate whose SAN names the eAPI host.
  No code change: rc9 already verifies when skip-verify is off.
- `scripts/ui-walkthrough.js`: a browser check of the whole operator
  walkthrough on a fresh nacd.
- ADR-0006 (pilot architecture) and ADR-0007 (session revocation,
  superseding ADR-0004's revocation trade-off).
- Docs: the tested installation and recovery setups, and corrected
  clean-install records (rc8 in-place only).

### pilot-rc9 (`3e92642`; console UI passed in the lab on 2026-11-07)
- **Console confirmations are in-page.** Native `window.confirm()` blocked
  the browser: a stale dialog from an rc7 tab blocked UI qualification of
  rc8. It can also be silently suppressed by the browser. All seven
  confirmations now use an in-page `<dialog>` with stable ids for automation
  (`#confirm-dialog`, `#confirm-ok`, `#confirm-cancel`), and Escape cancels.
- The CoA confirmation warns that a re-authentication can pause traffic
  briefly, also without a VLAN change, and that a VLAN change reaches the
  endpoint at its next DHCP renewal.
- Docs: rc8 results, and the unattributed same-VLAN re-authentication pause
  (about 20 s on vEOS).

### pilot-rc8 (after pilot-rc7 `e7d4162`, which passed the API workflows in the lab)
- **Switch Verified status read as current** (ASTRA, rc7). After a newer
  session replaced the verified one, the badge still said verified=yes,
  naming the ended session, while reachability was "no". Verified is now
  marked `historical`, with a `note` saying whether its session is still
  active. The console labels it "Verified test (record)".
- Docs: measured DHCP behavior on corp ⇄ quarantine transitions (old segment
  denied in ≤ 2 s; new segment reachable at the next DHCP renewal, 87–117 s
  in the lab, because the link stays up), and the distinction between switch
  authorization and endpoint reachability. The qualification status records
  the rc7 results and corrects the rc6 entry: its lab run was completed, not
  superseded before qualification.

### pilot-rc7 (supersedes pilot-rc6 `cf89206`, which was never qualified)
- `install.sh` restarts nacd. Re-running it on an active installation used
  `enable --now`, which left the old nacd process running the previous
  binary and console certificate. The installer now also fails unless the
  running nacd reports the installed commit, and records it in the install
  report.

### pilot-rc6 (supersedes pilot-rc5 `23349b7`, which was never qualified)
- **Security: concurrent exchanges could bypass quarantine** (Codex review
  of rc5). After an accounting Stop, a rejected and a valid exchange could
  race to create the session. When the Reject won, the valid exchange used
  the identity-less winner as-is, without its identity or inherited
  override. An insert conflict now re-runs the full session resolution
  (bounded, failing closed), so the winner is treated like any existing
  session.

### pilot-rc5 (`23349b7`, superseded before qualification; supersedes pilot-rc4 `76a53fd`)
- **Security: a replacement session could exist without its inherited
  quarantine** (Codex review of rc4). The session was inserted first and the
  override written afterwards. A failure in between left a session with an
  identity but no override, which the next login reused without
  inheriting, and a concurrent request could read it. The override is now
  decided before the insert and written by the same INSERT. A session
  created by a failed attempt gets the override before its identity.
- **Preview used stale override evidence** (Codex, P2). After a Restore
  whose CoA failed, the recorded `session.override` still said quarantine.
  Preview now evaluates both versions with the session's current override.

### pilot-rc4 (`76a53fd`, superseded before qualification; supersedes pilot-rc3 `74e5a1e`)
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
