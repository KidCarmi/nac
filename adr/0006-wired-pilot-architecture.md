# 0006. Wired pilot: FreeRADIUS front end, nacd decision service, versioned policy, identity-scoped operator override

- **Status:** Accepted
- **Date:** 2026-11-07
- **Deciders:** pilot build owner (OPUS), lab qualification (ASTRA), repository owner

## Context

The pilot must let an administrator who has never seen the code:
- connect an Arista EOS switch
- admit wired EAP-TLS endpoints
- explain each authorization
- change access through a versioned policy
- quarantine and restore a live session

It must survive restarts and stay usable by a later UI (Culvert) without
moving NAC logic out of nacd.

Constraints from the lab and the base platform:
- EOS 4.29.2F speaks RADIUS (802.1X, accounting, RFC 5176 CoA) and optional
  eAPI.
- The platform is Go, PostgreSQL, a hexagonal core and a default-deny engine
  (ADR-0001 to 0003).
- Security requirement: fail closed, and never trust a value from the
  network that the NAC cannot attribute to a source.

## Decision

1. **FreeRADIUS 3.2.5 terminates RADIUS and EAP-TLS. nacd decides.**
   - FreeRADIUS validates the certificate chain, then asks nacd once per
     request through `rlm_rest`, authenticated with a service token over
     loopback.
   - nacd returns the VLAN, and every returned value has `do_xlat=false`.
   - Requests without EAP (MAB) are rejected before nacd.
   - There is no authorization cache. If nacd is unreachable, FreeRADIUS
     rejects and logs the reject, and nacd imports those rejects later.
2. **Identity is the validated certificate, issuer + serial.** It maps to a
   device only through an explicit identity binding. Every decision input
   carries its source, observation time and status (present, missing or
   stale). Conditions are three-valued, so missing evidence never matches.
3. **Policy is versioned.**
   - The draft is edited, validated against the switch VLAN maps, and
     previewed over recorded inputs; the preview is labelled a prediction.
   - Publishing atomically activates exactly the previewed draft (digest
     check). Restore records a new activation.
   - History is append-only, enforced by database triggers.
   - Publish and restore send no CoA. They apply at the next authentication.
4. **The operator quarantine override belongs to an identity on a port**
   (switch + port + MAC + certificate issuer and serial), not to a session
   row.
   - It lasts until Restore, across accounting Stop, session replacement,
     reconnects, failed attempts in between and concurrent session creation.
   - It is written with the session in one statement, and resolution fails
     closed.
   - It takes precedence over policy at every authentication.
5. **CoA follows a stdlib RFC 5176 codec and an explicit state machine:**
   requested → sent → acked / nak / timeout / failed → observed.
   - Retries are byte-identical and never re-targeted.
   - At most one CoA is in flight per session.
   - "observed" means a re-authentication returned the requested VLAN, also
     on a replacement session.
6. **Three facts are kept apart in the API and the console:** switch
   authorization (requested), the switch's own observation (eAPI: VLAN,
   freshness, liveness), and endpoint reachability. NAC never measures
   reachability and always reports it as `not_observed`. ASTRA measured the
   gap: the endpoint reaches a new VLAN at its next DHCP renewal. There is
   no automatic port bounce: it has no documented vEOS support and would
   disconnect every device on the port.
7. **One versioned API is the only boundary** (`/api/v1`, OpenAPI 1.1.0,
   coverage-tested).
   - The embedded console is a thin client of it.
   - Authorization is checked server-side on every request against the
     operator record.
   - Every mutation is attributed to the authenticated operator.

   See `docs/architecture/integration-boundary.md`.
8. **One control node** (Ubuntu 24.04: PostgreSQL, nacd, FreeRADIUS),
   installed by an idempotent `install.sh`.
   - The installer keeps operator settings across re-runs, restarts nacd,
     and checks that the running build is the installed one.
   - Release bundles are reproducible for `nacd`: pinned toolchain,
     `-trimpath`, commit-time build date, explicit `VERSION`.

## Consequences

### Positive
- Decisions, their inputs and their policy version are auditable, and the
  console explains them without guessing.
- A nacd outage cannot grant access.
- Quarantine cannot be dropped by switch session churn. This was the root
  cause of one lab finding and four review findings.
- A future UI reuses the backend unchanged.

### Negative / trade-offs
- FreeRADIUS is a second process to operate. Its debug output prints the
  service token, and it restarts when switches change.
- No authorization cache means no new logins during a nacd outage. Sessions
  that are already up stay up until they re-authenticate (switch behavior).
- Quarantine is per identity on a port. A different valid certificate or
  another port is a different identity, and that must be handled with
  bindings or policy.
- Operators must check endpoint reachability themselves after a VLAN
  change.
- Single node, no high availability. eAPI TLS settings are global.

### Alternatives considered
- **nacd as the RADIUS server (no FreeRADIUS).** Rejected: a new EAP-TLS
  implementation is a large security surface, and FreeRADIUS is proven on
  the lab switch.
- **An authorization cache in FreeRADIUS for outages.** Rejected: it could
  grant access that policy no longer allows. Fail closed was required.
- **Override stored on the session row** (the first implementation).
  Superseded: accounting Stop and session replacement dropped it (ASTRA,
  pilot-rc2). Storing it in its own per-identity table is the planned next
  step. It would remove the copy-between-sessions step that is the
  remaining complexity.
- **CoA port bounce to force DHCP renewal.** Rejected for the pilot: no
  documented switch support, it affects every device on the port, and
  endpoints recovered at their normal renewal.
