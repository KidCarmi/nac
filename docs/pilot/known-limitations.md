# NAC pilot: known limitations

## Scope

- **One site, Arista EOS 4.29.2F only.** The generated configuration was
  validated on vEOS-lab 4.29.2F (ASTRA). Other EOS versions and other vendors
  are untested.
- **EAP-TLS only.** MAC Authentication Bypass (MAB) and EAP-TLS→MAB fallback
  are not supported. vEOS-lab 4.29.2F rejects `dot1x mac based
  authentication`, so they can only be validated on suitable physical Arista
  hardware.
  - The FreeRADIUS site rejects any request without EAP in `authorize`, so
    nacd is never asked and no policy can admit a MAC. This includes the MAC
    of a bound endpoint. `scripts/e2e-freeradius.sh` checks it. The
    generated EOS configuration enables no MAB.
  - **Do not enable MAB or mac-auth fallback on NAC ports, toward any RADIUS
    server.** MAC addresses are spoofable. ASTRA ran MikroTik CHR 7.24.6 with
    `auth-types=dot1x,mac-auth` and a fixed MAB rule outside this product.
    There, an endpoint whose certificate EAP-TLS rejected stopped EAP while
    using an allowlisted printer MAC, fell back to MAB and reached that MAC's
    VLAN (evidence: `astra/lab-evidence` @ `43980c6`,
    `lab/runs/mikrotik-mab-20261106/`).
  - If MAB is added later, it must:
    - be an explicit, lower-assurance path, labelled as such in decisions
    - be scoped to named switch ports
    - never assign a segment an EAP-TLS identity can get
    - define how a MAC treats a recent EAP-TLS reject on the same port.
- **Two segments per switch** in the console form (corp, quarantine). The API
  accepts more.
- **FreeRADIUS 3.2.5** (Ubuntu 24.04) only. 3.0.26, the earlier lab version,
  is not supported. **Single host**: PostgreSQL, nacd and FreeRADIUS on one
  node, with no high availability.

## Decision service outage (no authorization cache)

- While nacd is unreachable, FreeRADIUS rejects every new login and
  re-authentication ("NAC decision service unavailable"). No authorization is
  cached anywhere, and FreeRADIUS TLS session resumption is disabled.
- Sessions already authorized stay up on the switch until they
  re-authenticate. That is switch behavior, not a NAC decision. A switch
  re-authentication timer therefore bounds how long existing sessions survive
  an outage. The lab configuration has none, and the generated configuration
  shows one only as an optional, unvalidated comment.
- While nacd is down, nothing is recorded in NAC. FreeRADIUS logs the rejects,
  and nacd imports them after recovery.

## Sessions without accounting or eAPI (the `fe7c981` lab setup)

- **Session end.** NAC ends a session only on a RADIUS accounting Stop, or
  when a different certificate authenticates on the same port and MAC. The lab
  switch sends no accounting. A session therefore stays "active" after the
  endpoint unplugs, until the next authentication on that port and MAC.
- **Liveness.** Without a fresh eAPI observation, the session page says
  liveness is **unknown**: "active" means the last authentication was
  accepted, not that the endpoint is connected now.
- **Switch-observed state** comes only from eAPI (`show dot1x hosts | json`).
  The `fe7c981` lab run had no eAPI configured, so observation showed
  "unavailable" and **Verify** was "decision only". The eAPI and accounting
  paths are being qualified on `pilot-rc2`; see
  [qualification status](qualification-status.md).
- **CoA retry guard.** Before every retransmission, nacd checks that the
  captured session is still current in NAC: not ended, same start time, same
  port. If not, nacd abandons the CoA and never re-targets it. Without
  accounting, NAC cannot know the endpoint left, so the real guard is the
  switch. The CoA names the switch (`NAS-IP-Address`), the port (`NAS-Port-Id`)
  and the MAC (`Calling-Station-Id`, exactly as the switch sent it), so it
  cannot affect a different endpoint. If that MAC is no longer on that port,
  the switch answers NAK 503 "session context not found". The 503 behavior on
  vEOS is expected from RFC 5176 but has not been captured in the lab.
- **Acknowledged is not enforced.** `acked` means only that the switch accepted
  the CoA. `observed` means its re-authentication received the requested VLAN.
  Neither proves reachability. In the virtual lab, ASTRA measured about
  17–22 s from switch SUCCESS to TCP reachability, and 21.3 s from a Restore
  request to permitted TCP on `fe7c981`.

- **Quarantine follows one identity on one port.** An operator override
  applies to switch + port + MAC + certificate (issuer and serial).
  - It lasts until an operator clicks **Restore**. It survives
    re-authentication, an accounting Stop followed by a new session (as EOS
    sends around a CoA re-authentication with accounting on), and unplugging
    and reconnecting. Each new session for that identity inherits it, and the
    timeline says so.
  - A *rejected* certificate never replaces the session, so a failed attempt
    cannot clear a quarantine.
  - A *different valid* certificate on the same port and MAC, or the same
    certificate on a different port, is a different identity and is decided
    by the active policy without the override. To keep a device quarantined
    regardless of certificate or port, remove its identity bindings or
    publish a policy for it.
  - **`pilot-rc1` (`fe7c981`) and `pilot-rc2` (`c121a95`) lose the override**
    when the switch sends accounting Stop before re-authenticating: the
    endpoint is re-authorized by policy (ASTRA, rc2, accounting enabled).
    Fixed in `pilot-rc6` and later (the replacement scenario passed in the lab on `pilot-rc7`), which also closes the variants found in review of
    `pilot-rc3` (a failed attempt between the Stop and the valid login),
    `pilot-rc4` (a replacement briefly stored without the override) and
    `pilot-rc5` (a rejected and a valid exchange racing to create the
    session). Do not enable switch accounting with earlier candidates.

## Switch authorization is not endpoint reachability

NAC controls and reports **switch authorization**: which VLAN the switch is
told to put a session in, and (with eAPI) which VLAN the switch reports.
- CoA **observed** means a re-authentication returned the requested VLAN.
- A fresh **Observed on the switch** with "matches requested" means the
  switch reports that VLAN.

Neither means the endpoint has a usable address in that VLAN.

Measured on `pilot-rc7` (EVE, vEOS-lab 4.29.2F, one-NIC Linux DHCP
endpoint, ASTRA):
- The endpoint moved corp → quarantine → corp without manual renewal and
  without a port bounce.
- The **old** segment was denied within 0.1–2 s of the CoA.
- The **new** segment was reachable 87–117 s after the CoA. A CoA
  re-authentication keeps the link (carrier) up, so the DHCP client keeps
  its old lease until its normal renewal. Reachability on the new segment
  arrives with that renewal.
- Expect an outage of roughly one DHCP renewal interval after quarantine or
  restore. To shorten it, use shorter leases on the corp and quarantine
  scopes: renewal normally starts at half the lease time. This sizing is
  guidance derived from the measurement, not a separately lab-tested
  setting.
- **Windows endpoints are untested.** Other DHCP clients may renew sooner
  or later.

**Re-authentication can briefly interrupt traffic, even without a VLAN
change.** On `pilot-rc8`, a same-VLAN re-authentication (CoA on a session
already in quarantine) twice interrupted fresh quarantine TCP for about 20 s.
Carrier, address and the switch state (Et1 SUCCESS, VLAN 20) stayed
unchanged.
- The cause is **not attributed**: NAC sends a single CoA-Request (the
  lab-validated attribute set, including the VLAN) and the switch runs a full
  EAP re-authentication. A comparable 20–30 s forwarding convergence was seen
  on vEOS after VLAN moves.
- Physical Arista hardware is untested. Avoid unnecessary Re-authenticate
  actions on production sessions. The console's confirmation says so.

NAC has no automatic re-addressing action, by design. A port bounce has no
documented support on vEOS 4.29.2F, would disconnect every device on the
port, and the lab shows endpoints recover without one.

## Security and operations

- The FreeRADIUS service token is visible in FreeRADIUS **debug** output
  (`freeradius -X` prints expanded configuration). Do not share debug logs.
- The console certificate is self-signed unless one is supplied.
- Switch RADIUS secrets are encrypted in the database with `NAC_SECRETS_KEY`.
  FreeRADIUS needs them in clear in `/var/lib/nac/freeradius-clients.conf`
  (mode 0640, `nac:freerad`).
- Registering, changing or deleting a switch restarts FreeRADIUS (about a
  second). In-progress EAP exchanges retry.
- Posture has no integration in the pilot: it is set through the device
  record (API/console), is considered stale after `NAC_POSTURE_MAX_AGE`
  (24 h), and is never taken from RADIUS.
- **Validation:** see [qualification status](qualification-status.md).
  - The documented install passed as a clean-instance install on
    `pilot-rc7` (about 63 s) and `pilot-rc9` (about 44 s from bundle
    extraction to running services, with an automatic recovery after
    reboot). `pilot-rc8` was installed in place over rc7 only. In-place
    upgrades preserved settings, and the running build matched.
  - CoA, accounting, eAPI observation, DHCP transitions, outage and
    full-restart recovery passed on vEOS-lab 4.29.2F (`pilot-rc7`,
    `pilot-rc8`, through the API).
  - The console workflows (sign-in, status, policy edit, preview, publish
    and restore, quarantine and restore CoA) passed on `pilot-rc9`.
  - Not lab-run:
    - the console **Account** page (password change and host reset passed
      through the API and `nac-reset-password` on the clean `pilot-rc9`
      instance)
    - a clean-instance install of exact `pilot-rc8` (blocked in the lab)
    - switch onboarding on the clean `pilot-rc9` instance. Switch
      registration through the console last passed on `pilot-rc1`; the rc9
      UI run used an already registered switch.
  - Untested: physical Arista hardware (including MAB and unplug) and
    Windows endpoints.
- **One operator account.** The bootstrapped administrator can change its own
  password (console **Account**, or `POST /api/v1/auth/password`), and root on
  the control node can reset it (`nac-reset-password`). There is no operator
  management (more accounts, roles, disabling) in the console or API, no
  per-account lockout after failed attempts (Argon2id cost and per-client
  rate limiting only), and no single sign-on.
- **Confirmation dialogs.**
  - Publish, restore, quarantine/restore/re-authenticate, binding removal,
    secret reveal and switch deletion ask for confirmation in an in-page
    dialog (from `pilot-rc9`), with stable ids for automation:
    `#confirm-dialog`, `#confirm-ok`, `#confirm-cancel`. Escape cancels.
  - Earlier candidates used the native `window.confirm()`. It blocks the
    browser and blocked ASTRA's UI qualification on rc7/rc8.
- **eAPI TLS settings are global.** `NAC_EAPI_INSECURE_SKIP_VERIFY=true`
  (needed for self-signed eAPI certificates) disables certificate checking
  for every switch's eAPI. `NAC_EAPI_TLS_ALLOW_RSA_KEX=true` (needed for stock
  EOS 4.29.2F eAPI without an ECDHE SSL profile) allows RSA key exchange for
  every switch's eAPI. Neither affects other connections. Per-switch
  certificate pinning is not implemented.
- **Reproducible build:** `nacd` builds to the same SHA-256 from the same
  commit and `VERSION`. The bundle tarball does not, because of archive
  timestamps.
