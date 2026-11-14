# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Session read consistency (proposed on top of pilot-aruba-rc5; NOT a candidate)
- `GET /api/v1/sessions/{id}` and `GET /api/v1/sessions` now read each
  session, its switch, device, binding, decisions, CoA requests and events
  from one read-only REPEATABLE READ snapshot, and build `last_coa`,
  `coa_requests`, liveness, the timeline and the explanation from those
  records only.
  - A response can no longer mix the session before an operator action or
    re-authentication with the CoA after it (CI, PR #5).
  - `last_decision` is the decision the session applies, not a newer
    decision row the session does not name yet.
  - No enforcement, RADIUS, CoA, writer, schema or isolation-default
    change. See `docs/architecture/session-read-consistency.md` for the
    contract, the deterministic PostgreSQL tests and the writer-ordering
    report.

### pilot-aruba-rc5 (proposed successor; NOT under qualification; rc4 stays frozen)
- `pilot-aruba-rc4` plus the console wording fix **U2**, and nothing else.
  On a role switch the policy action is labelled "Apply active policy"
  (it sends a role CoA; nothing re-authenticates). VLAN switches keep
  "Re-authenticate (apply active policy)". The API action `reauthenticate`
  is unchanged. No enforcement, RADIUS, adapter, reload-path or installer
  change.

### pilot-aruba-rc4 (proposed; supersedes pilot-aruba-rc3 for the D2 wording defect)
- `pilot-aruba-rc3` plus the `pilot-rc14` liveness wording fix D2 (merged),
  and nothing else. No enforcement change.

### pilot-aruba-rc3 (proposed; supersedes pilot-aruba-rc2, whose D1 retest failed)
- `pilot-aruba-rc2` plus the `pilot-rc13` reload-race fix (merged), and
  nothing else.

### pilot-aruba-rc2 (proposed; supersedes pilot-aruba-rc1 before any lab run)
- `pilot-aruba-rc1` plus the known defect D1 fix from `pilot-rc12`, and
  nothing else. Merged, not re-implemented. On the Aruba path, Configured
  also follows what FreeRADIUS loaded.

### pilot-aruba-rc1 (superseded; owner-approved bounded Aruba integration; carries D1)
- **Aruba AOS-CX role enforcement.**
  - A switch has a `platform` (`arista-eos` | `aruba-aos-cx`) and an
    `enforcement` (`vlan` | `role`).
  - On a role switch the decision selects a configured local role, returned
    as `Aruba-User-Role`, with no Tunnel-* attributes. The VLAN and the IP
    never change.
  - A missing mapping rejects; there is no fallback.
- **Session-targeted role CoA for Quarantine, Restore and Re-authenticate.**
  - `acked` is final, and never presented as traffic verification.
  - A Restore or Re-authenticate the policy would turn into a deny is
    refused (`422`), and the session keeps its override.
- **The identity-scoped quarantine override persists** as on the VLAN path.
- **Session API and console:** a new `enforcement` block (platform,
  strategy, mechanism, observation, limitations) and `requested.role`.
  Observation is `unavailable` for AOS-CX.
- **Switch form:** platform choice, role fields and AOS-CX reference
  configuration. Validation accepts AOS-CX port names (`1/1/1`).
- **FreeRADIUS outage path** also strips `Aruba-User-Role`.
- **Tests:**
  - `test/integration/aruba_role_test.go`
  - Aruba checks in `scripts/e2e-freeradius.sh`
  - `scripts/ui-aruba-role.js`
  - an Arista CoA regression assertion
- **Migration `0006`:** adds the switch, decision, session and CoA columns.
  Existing switches become `arista-eos` / `vlan`.
- **Scope and lab plan:** `docs/pilot/aruba-role-path.md`. Known defect D1
  is not fixed in this candidate; see pilot-aruba-rc2.
### pilot-rc14 (proposed; operator-status wording fix D2; not yet lab-run)
- **Session liveness wording (D2, ASTRA `pilot-aruba-rc3` console run).**
  - With accounting enabled, an active session that had a stored accounting
    Start still said "NAC receives no RADIUS accounting and no fresh switch
    observation". That appeared in the session panel, "Why" and the CoA
    confirmation.
  - The note now states the latest accounting packet NAC **accepted** for
    the session (Start or Interim-Update, its age and Acct-Session-Id, "no
    Stop since"), and why that does not prove the endpoint is connected now.
    The `basis` is `accounting Start, no Stop`.
  - Without accounting it says "has received no RADIUS accounting", as
    before. Packets ignored as stale don't count.
- **Not changed:** liveness states, decisions, CoA, enforcement, the
  adapter, the installer and the database schema. The change is in nacd
  only.
- **Tests:** a unit test for the wording cases; an integration assertion
  through the API with a real accounting Start. That assertion fails on
  rc13 with ASTRA's exact symptom.

### pilot-rc13 (proposed; replacement for pilot-rc12 after ASTRA's D1 retest; not yet lab-run)
- **Missed trailing reload fixed.** ASTRA's chaos run on `pilot-aruba-rc2`
  failed C2, C3 and cleanup: FreeRADIUS kept an older list after a burst,
  and no further reload ran.
  - `nac-freeradius-reload` waits 1 s to coalesce a burst, reloads, then
    re-hashes the file and reloads again if it changed (at most 10 passes).
    A lock serializes runs; children never inherit it, and the wait is
    bounded at 120 s. The record's `at` has milliseconds.
  - `nac-freeradius-reload.service`: `StartLimitIntervalSec=0`,
    `TimeoutStartSec=5min`.
  - **nacd backstop:** a list still unconfirmed 20 s after it was written
    is rewritten identically, at most every 30 s, to fire the path unit
    again. The status grace period counts from when the list content
    changed, so a dead reload path still ends in `no`.
- **`scripts/chaos-freeradius-reload.sh`:**
  - "loaded" compares the record's SHA-256 with the current file, and the
    FreeRADIUS start time (µs) with the file mtime (ns)
  - each scenario polls up to `CHAOS_DEADLINE_SECONDS`, default 60, and
    reports the time to converge
- **Changed files versus rc12:**
  - `deployments/pilot/nac-freeradius-reload`
  - `deployments/pilot/systemd/nac-freeradius-reload.service`
  - nacd

  Re-run the installer.

### pilot-rc12 (superseded; replacement for the D1 reload-path defect; its D1 retest failed)
- **Known defect D1 fixed** (ASTRA chaos run on rc10, `5eb1ff7`: C4 FAIL).
  - **Input:** `radius_source_ip` and `nas_ip` must be dotted IPv4.
    IPv4-mapped spellings get `422`. Stored mapped rows are normalized at
    start, and the client file never carries a non-IPv4 or duplicate
    address.
  - **Status:** the reload path now runs
    `/usr/local/sbin/nac-freeradius-reload`, which records the result and
    the client-file SHA-256 in `/var/lib/nac/freeradius-reload.status`.
    Switch **Configured** and the new `freeradius_load` (switch status and
    `/api/v1/radius/health`) are `yes` only when FreeRADIUS restarted with
    exactly the current list and is running. Otherwise they are `pending`
    or `no`, with the reason.
  - **Recovery:** a failed FreeRADIUS is restarted once the list is valid;
    a deliberately stopped one is left stopped.
- **Console:** "Loaded by FreeRADIUS" on the overview and the switch page,
  and a `pending` state.
- **`scripts/chaos-freeradius-reload.sh`:**
  - compares nacd's claim with the ground truth after every scenario
  - adds C4b (hex mapped spelling) and C6 (FreeRADIUS deliberately stopped)
- **Changed files beyond nacd:**
  - `deployments/pilot/nac-freeradius-reload` (new)
  - `nac-freeradius-reload.service`
  - `install.sh`, which installs the helper

  Re-run the installer to get them. No database migration.

### pilot-rc11 (proposed; not yet qualified; carries D1 and D2)
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
- **Known defect D1 (switch → FreeRADIUS reload path), fixed in
  `pilot-rc12`.** "Configured"
  reflects nacd writing the clients file, not FreeRADIUS loading it. An
  IPv4-mapped `radius_source_ip` (`::ffff:10.x`) is accepted, the reload is
  skipped, and FreeRADIUS fails at its next restart. Workaround and fix
  scope are in the known limitations. The fix will be a separate replacement
  candidate. Its chaos test is `scripts/chaos-freeradius-reload.sh`
  (non-production nodes only).
- **DHCP server comparison (ASTRA, `8db6fe8`):** an authoritative dnsmasq
  moves Linux recovery from lease expiry to T2. That is 23–52 min on a
  1-hour lease: improved, not prompt. ASTRA proposed an acceptance target
  of 60 s, not yet agreed. See "Recovery boundary" in the known
  limitations.
- **Short T1/T2 remedy: FAILED** (ASTRA, `e86242e`). T1 = 30 s and T2 = 45 s
  on a 1-hour lease recovered the Quarantine direction in 25 s, but the
  Restore direction did not recover within 165 s: the T2 rebind was not
  seen on either segment. Not recommended. The next candidate is an
  endpoint hook for managed Linux. A port flap would be a product/switch
  change needing owner approval.
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
