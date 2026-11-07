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
  "unavailable" and **Verify** was "decision only". eAPI observation and
  accounting passed later (`pilot-rc7` onward); see
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

Neither means the endpoint has a usable address in that VLAN. The session
page and API show the three facts separately: switch authorization, the
switch's observation, and endpoint reachability (`not_observed`, because NAC
never tests the endpoint). The third one is new in `pilot-rc10`.

### Operating envelope (measured, `pilot-rc10`)

The lab DHCP servers are BusyBox udhcpd and send no T1/T2 options. The
**old** segment is denied within about 1–2.5 s of the CoA in every run,
whatever the endpoint or lease. How long the endpoint takes to reach the
**new** segment depends on the endpoint's DHCP client:

| Endpoint | Recovery on the new segment | Measured with |
|---|---|---|
| Windows Server 2025 Standard Evaluation, Desktop Experience (build 26100), one NIC | **about 30–65 s**, whatever the lease length or lease age. Windows changes its address, DNS and route by itself | 120 s and 3600 s leases; early, near-T1 and console transitions ([evidence `f2cbf3a`](https://github.com/KidCarmi/nac/tree/f2cbf3aeeecba95075aa7e9f1881421006092b30/lab/evidence/windows-lease-rc10-20261107)) |
| Linux (systemd-networkd), one NIC | **only when the old lease expires**: the delay is the lease time left at the CoA, so the worst case is about the full lease | 120 s lease: 24–117 s. 3600 s lease: **1,811.9 s** (30 min) for a Restore about 30 min before expiry ([evidence `a6df762`](https://github.com/KidCarmi/nac/tree/a6df7627bcdf87d9e4b3c81d412dc91a949c4bd8/lab/evidence/linux-lease-rc10-20261107)) |

**What this supports:**
- **Windows endpoints:** the tested lease lengths (120 s and 1 h).
- **Linux endpoints:** only where an outage of up to the full lease is
  acceptable after a quarantine change or a restore. In practice that means
  short leases. 120 s is the only short lease measured.

**Not yet measured:**
- **Shorter leases.** They shorten the Linux worst case proportionally, at
  the cost of more DHCP traffic and more dependence on the server.
- **A DHCP server that NAKs a rebind for an address from another subnet.**
  This should end the Linux wait at T2 (87.5 % of the lease). The lab udhcpd
  stays silent instead.
- **An endpoint hook that renews the address after 802.1X succeeds.**

None of these is recommended until it has been run in the lab.

**Linux 1-hour details (ASTRA, [`a6df762`](https://github.com/KidCarmi/nac/tree/a6df7627bcdf87d9e4b3c81d412dc91a949c4bd8/lab/evidence/linux-lease-rc10-20261107)):**
- **Restore near T1:**
  - The old segment was denied at +1.419 s.
  - At T1, a unicast renewal went to the old server, which was out of reach.
  - From T2 (13:09:09 UTC), four broadcast rebinds reached the corp udhcpd,
    which did not reply.
  - At expiry (13:16:40), a Discover/Offer/Request/ACK took about 8 ms.
    Fresh corp TCP succeeded at +1,811.866 s.
  - Throughout, the switch showed SUCCESS VLAN 10, both services were
    healthy, and no one touched the endpoint.
- **Early Quarantine:** there was no automatic recovery within the
  148 s observation window. The diagnostics then started, so the longer-term
  automatic outcome is **blocked by intervention**, not a failure.
  Diagnostics, recorded separately:
  - `networkctl renew` did not help: it sends the same unicast renewal.
  - `networkctl down` / `up` reached quarantine in 6.9 s.

Earlier measurements (EVE, vEOS-lab 4.29.2F, one-NIC Linux DHCP endpoint):
- The endpoint moved corp → quarantine → corp without manual renewal,
  reconnect or port bounce, on `pilot-rc7` to `pilot-rc10`. This held for
  CoA and for rule-only policy changes.
- The **old** segment was denied within about 0.1–2.2 s of the CoA.
- The **new** segment was reachable 24–117 s after the CoA. A CoA
  re-authentication keeps the link (carrier) up, so the DHCP client keeps
  its old lease until it gets a new one.

| Run | DHCP lease | Old segment denied | New segment reachable |
|---|---|---|---|
| `pilot-rc7` | not reported | 0.1–2 s | 87–117 s |
| `pilot-rc8`, after a full restart | not reported | 0.703 / 0.685 s | 112.7 / 84.2 s |
| Clean `pilot-rc9` instance, console Quarantine then Restore | **120 s** | 1.436 s / 2.201 s | 75.9 s / 116.1 s |
| `pilot-rc10` Quarantine CoA / Restore CoA | 120 s | 1.964 s / 1.386 s | 24.4 s / 110.9 s |
| `pilot-rc10` rule-only publish v3 (corp → quarantine) / v4 (back) | 120 s | 1.065 s / 1.601 s | 72.1 s / 79.5 s |
| `pilot-rc10` **Windows Server 2025**, Q1 / R1 / Q2 / R2 (console) | 120 s | 2.027 / 1.776 / 1.728 / 2.290 s | 33.4 / 61.4 / 63.9 / 34.4 s |
| `pilot-rc10` Windows after a full lab restart, Q / R (console) | 120 s | 2.378 / 2.345 s | 62.6 / 35.5 s |

- **Why it takes this long (confirmed in the `pilot-rc10` captures).**
  - The DHCP server sends no T1/T2 options, so the client uses the
    defaults.
  - At about 60 s (half the 120 s lease) it renews by unicast to its old
    DHCP server, which is not reachable from the new VLAN.
  - At about 105 s (87.5 % of the lease) it broadcasts a rebind for its old
    address. That reaches the new VLAN's server (BusyBox udhcpd in the lab),
    which **does not answer** it.
  - When the lease expires, the client starts over with a Discover. The new
    VLAN's server answers that, about 15 s after the rebind.
  - So the Linux endpoint reaches the new VLAN when its old lease
    **expires**. The delay is anywhere from a few seconds to about **the
    full lease time**, depending on where in its lease the endpoint was at
    the CoA. This explains the 24–117 s spread.
  - Correction: earlier versions of this page said the rebind succeeds
    (worst case 0.9 × the lease). The rc10 captures show it does not
    (`runs/rc10-core/findings.md` in the closeout package, and the 1-hour
    run).
  - A DHCP server that refuses (NAKs) a rebind for an address from another
    subnet could end the wait at T2 instead. That is untested.
  - Shorter leases on the corp and quarantine scopes shorten the worst
    case proportionally, at the cost of more DHCP traffic. This follows
    from the measured behavior but was not run as a separate lab
    configuration. The 1-hour lease confirmed recovery at expiry (see the
    operating envelope above).
- **Windows Server 2025** (Desktop Experience, machine EAP-TLS, one NIC)
  also recovered without manual action, within 33–64 s of the CoA on a
  120 s lease, and within 33–62 s on a 1-hour lease, at any lease age. Its
  recovery does not wait for a lease timer. Windows Server
  Core lacks the wired 802.1X service (`dot3svc`) and cannot be used. Other
  DHCP clients are untested.

**Limitation: on vEOS-lab 4.29.2F, any accepted CoA interrupts the endpoint,
even without a VLAN change.** This is a switch/CoA operating limitation, not
a NAC product defect. Physical Arista hardware is unconfirmed.

ASTRA measured it on exact `pilot-rc10` ([evidence `7b34ef5`](https://github.com/KidCarmi/nac/tree/7b34ef594e71f620fdc657b7e82bf0b57845d2db/lab/evidence/timing-rc10-20261107): 62 files, manifest verified here):
- **Endpoint:** a one-NIC Windows Server 2025 Standard Evaluation (Desktop
  Experience, build 26100) EAP-TLS endpoint on corp VLAN 10, with a 120 s
  lease.
- **Held constant:** the certificate, port, address (10.70.10.80) and probe
  targets. There was no manual intervention.

| Trigger (same session, same VLAN 10) | Switch response | Fresh corp TCP |
|---|---|---|
| Idle control | — | 0/41 failures |
| NAC **Re-authenticate** CoA, ×3 | CoA-ACK (Error-Cause 200), then Access-Accept VLAN 10 | failures from +1.9 / +2.1 / +2.1 s; recovery +21.2 / +50.6 / +50.3 s |
| `radclient` CoA from outside NAC with the same VLAN attributes, ×3 | CoA-ACK (Error-Cause 200) | failures from +1.8 / +1.8 / +2.2 s; recovery +21.3 / +50.1 / +57.7 s. In the third run Windows briefly lost its address |
| `radclient` CoA with no VLAN attributes | **CoA-NAK, Error-Cause 404 Invalid-Request** | 0 failures (nothing happened) |
| Switch-local `dot1x re-authenticate Ethernet 1`, ×3 | Access-Accept, EAP Success | 0 failures (72 / 71 / 53 samples) |

- **The radclient packet matched NAC's.** Its VLAN attributes were byte-for-byte
  identical to NAC's (`64:40060000000d`, `65:410600000006`, `81:51043130`).
  The interruption reproduces without nacd, so it comes from how vEOS
  handles a VLAN-bearing CoA.
- **A CoA without VLAN attributes is not a substitute.** vEOS refuses it,
  and the switch-local re-authentication has no CoA equivalent on this
  build. NAC therefore keeps the lab-validated attribute set.
- **Quarantine and Restore send the same kind of CoA.** Their measured
  recovery times (DHCP table above) may include part of this effect. The
  Linux DHCP explanation comes from captures and stands. The two effects are
  not separated for Windows.
- **Operator guidance:**
  - Use Re-authenticate only when the active policy should change a
    session's VLAN.
  - On a session that should stay where it is, expect a 20–60 s outage on
    vEOS.
  - The console's CoA confirmation says "about 20 s", which understates
    this. It also says a VLAN change reaches the endpoint "at its next DHCP
    renewal". That is wrong for the Linux client, which waits for lease
    expiry. The wording will be corrected in the next candidate. It is not a
    lab-contract defect, so it does not move `pilot-rc10`.
- `pilot-rc8` showed the same effect in quarantine: two pauses of about
  20 s.
- **Established connections:** one established corp TCP connection
  failed 2.8 s after a Quarantine on Windows and did not reconnect
  ([`f2cbf3a`](https://github.com/KidCarmi/nac/tree/f2cbf3aeeecba95075aa7e9f1881421006092b30/lab/evidence/windows-lease-rc10-20261107)). Applications must reconnect after a VLAN
  change.

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
  - Console switch onboarding, certificate binding, policy and CoA passed
    with a real vEOS switch on a clean `pilot-rc9` instance.
  - `pilot-rc10` passed the closeout qualification: clean install, real vEOS
    switch, CoA and rule-only policy, Linux and Windows Server 2025 DHCP,
    invalid certificate, nacd outage, verified eAPI TLS, the console Account
    page, and a full lab restart. Evidence: https://github.com/KidCarmi/nac/tree/fa2950cbe18191d118661c00aff719a4f638a4fe/lab/evidence/pilot-closeout-20261107
  - **Single fresh-instance console workflow: PASS** on `pilot-rc10`. Onboarding, binding, preview/publish, Quarantine/Restore and the Account change were all done in the console on one fresh instance with a real vEOS switch ([`0e16356`](https://github.com/KidCarmi/nac/tree/0e16356c152c561721b05345b9bae408713fcc68/lab/evidence/clean-ui-rc10-20261107)).
  - Not lab-run: a clean-instance install of exact `pilot-rc8` (blocked in
    the lab). Revocation of a separately kept pre-change token is now
    lab-run: 401 after the password change.
  - Untested: physical Arista hardware (unplug/reconnect, MAB) and endpoint
    operating systems other than Linux and Windows Server 2025.
- **The switch must save the NAC configuration.** After a full lab restart,
  the vEOS switch came back with an older saved RADIUS target and the
  default TLS profile, and the NAC configuration had to be reapplied (rc10
  closeout). Once the rc10 configuration was saved and verified, it survived
  a switch reboot: Windows admission and Quarantine/Restore passed with no
  reapplication (ASTRA, rc10). NAC does not configure or check the switch's saved
  configuration. Run `write memory` after applying the generated
  configuration and the eAPI TLS profile. After a switch reboot, compare
  `show running-config` with **Generated EOS configuration**.
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
- **eAPI TLS settings are global.**
  - Verified TLS (installation §7a: `SSL_CERT_FILE` plus a switch
    certificate whose SAN names the eAPI host) applies one trust store to
    all switches. There is no per-switch certificate pinning.
  - The unverified fallback (`NAC_EAPI_INSECURE_SKIP_VERIFY=true`, plus
    `NAC_EAPI_TLS_ALLOW_RSA_KEX=true` for stock EOS 4.29.2F) also applies to
    all switches.
  - Verified TLS passed on vEOS-lab 4.29.2F (clean `pilot-rc9` instance):
    Probe and Verify succeeded, and a wrong IP SAN was rejected.
- **Reproducible build:** `nacd` builds to the same SHA-256 from the same
  commit and `VERSION`. The bundle tarball does not, because of archive
  timestamps.
