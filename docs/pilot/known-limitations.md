# NAC pilot: known limitations

## Support boundary (read first)

**Functional qualification and recovery time are separate results.**

- **Functional (`pilot-rc10`, PASS):** a real vEOS switch passed the full
  operator workflow on a fresh instance:
  - onboarding, binding, preview/publish, Quarantine/Restore and Account
  - enforcement of the old segment within about 1–2.5 s of every CoA

- **Recovery time on the new segment (measured, not a pass criterion):**
  it depends on the switch, the endpoint's DHCP client and the DHCP server.
  The results hold only for these tested combinations:

  | Switch | Endpoint | DHCP server, lease | Measured recovery |
  |---|---|---|---|
  | vEOS-lab 4.29.2F | Windows Server 2025 Standard Evaluation, Desktop Experience, build 26100 | BusyBox udhcpd; 120 s and 3600 s, at the lease ages tested | about 30–65 s |
  | vEOS-lab 4.29.2F | Linux, systemd-networkd | BusyBox udhcpd; 120 s | 24–117 s |
  | vEOS-lab 4.29.2F | Linux, systemd-networkd | BusyBox udhcpd; 3600 s | 1,811.9 s for one Restore about 30 min before expiry |
  | vEOS-lab 4.29.2F | Linux, systemd-networkd (Ubuntu 22.04.5) | dnsmasq 2.91, authoritative; 3600 s; default T1/T2 | at T2, by NAK then Discover: early Quarantine 3,112.6 s, near-T1 Restore 1,369.6 s ([`8db6fe8`](https://github.com/KidCarmi/nac/tree/8db6fe899809c2a20db741d08f72e8e3711680e1/lab/evidence/dhcp-server-comparison-rc10-20261107)) |

- **Unresolved: long leases with Linux clients.** Measured:
  - With BusyBox udhcpd, a Linux endpoint reached its new VLAN only when its
    old lease expired.
  - With an authoritative dnsmasq it recovered at T2 (87.5 % of the lease),
    which is still 23–52 min on a 1-hour lease.

  With ordinary long leases, that is a material usability limitation. No
  remedy has met a recovery target yet. The remedy evaluation is under
  "Recovery boundary" below.
- **Untested:** physical Arista hardware, Windows 10/11, other DHCP clients
  and servers, and other accepted CoA forms.

## Known defect D1: "Configured" does not prove FreeRADIUS loaded the switch (found after `pilot-rc10`)

**Status:**
- **Confirmed by ASTRA** on `pilot-rc10` (C4 FAIL, an actual RADIUS outage
  after a forced restart):
  [`5eb1ff7`](https://github.com/KidCarmi/nac/tree/5eb1ff7dddabbaaff553bc1b8c85cb4f23090382/lab/evidence/aruba-reload-chaos-20261109).
  In the same run, C1–C3 reached a valid loaded state within 12 s, and C5
  recovered.
- **Present in** `pilot-rc10`, `pilot-rc11` and `pilot-aruba-rc1`.
- **`pilot-rc12` / `pilot-aruba-rc2`: partly fixed; retest FAILED.** ASTRA
  ran the chaos test on exact `pilot-aruba-rc2` (clean Ubuntu 24.04.5,
  FreeRADIUS 3.2.5, burst 8, settle 12 s): 17 PASS, 3 FAIL (C2 burst, C3
  secret rotation, cleanup).
  - **What failed:** after a burst, the record said `ok` for an older list.
    FreeRADIUS ran that older list, and no further reload followed.
  - **What held:** nacd correctly said `pending`, never a false `yes`. C4
    inputs got `422`; C6 (stopped) said `no`.
  - **Runner flaw:** the runner's whole-second ground truth marked
    same-second cases as loaded.
- **Fixed in `pilot-rc13` / `pilot-aruba-rc3`**: the missed trailing
  reload, plus the hash-based runner. See "The fix" below.
  - **Lab: PASS on exact `pilot-aruba-rc3`** (`d96bb74`), which carries the
    rc13 reload path. ASTRA's chaos run on real systemd and FreeRADIUS
    passed **20/20**: eight rapid registrations, eight secret rotations and
    cleanup all converged to the exact current file hash, plus process kill
    and recovery, and outage.
  - Evidence:
    [`148e196`](https://github.com/KidCarmi/nac/tree/148e19614d58a9239226d5f212cfad9fac4ac504/lab/evidence/aruba-rc3-qualification-20261109),
    77/77 manifest entries verified here.
  - This supersedes the rc2 D1 failure for that code. **`pilot-aruba-rc2`
    stays FAIL**
    ([`a764e52`](https://github.com/KidCarmi/nac/tree/a764e52f6118f09c8e3dabecbff22613e99db983/lab/evidence/aruba-rc2-qualification-20261109)).
  - `pilot-rc13` itself (the Arista line) has not been lab-run; it carries
    the same reload code.

**The defect.** A switch's "Configured: yes" means nacd **wrote**
`/var/lib/nac/freeradius-clients.conf`. It does not mean FreeRADIUS loaded
that file. If the reload fails, the console still says "yes".

**Reproduced here** (nacd, and FreeRADIUS 3.2.5 `-C` on the rendered file):
1. **IPv4-mapped source IP breaks the reload.** `radius_source_ip` entered as
   `::ffff:10.70.99.1` is accepted and stored as typed.
   - `freeradius -C` rejects the file ("Invalid address"), so the reload
     unit skips the restart.
   - The running FreeRADIUS keeps the **old** client list, while nacd says
     "configured".
   - At the next FreeRADIUS restart (reboot, package update), the unit's own
     configuration check fails. That is a **RADIUS outage for every switch**.
2. **Hex mapped form silently breaks one switch.** `::ffff:a46:6301` passes
   `-C`, but FreeRADIUS treats it as an IPv6 client. The switch's IPv4
   packets then come from an unknown client, so all its authentications fail.
3. **The mapped spelling bypasses the uniqueness check.** The same switch
   can be registered twice.

**Not reproduced here:** systemd coalescing of rapid writes, and the reload
unit hitting its start limit. The build container has no systemd.
`scripts/chaos-freeradius-reload.sh` covers these on a disposable control
node (scenarios C1–C3).

**Workaround until fixed:**
- Enter `radius_source_ip` and `nas_ip` in dotted IPv4 only (`10.70.99.1`).
- After every switch create, update or delete, confirm on the control node:
  - `systemctl status nac-freeradius-reload.service` shows the last run
    succeeded
  - `freeradius -C` passes

  Do not rely on the console's "Configured" alone.

**The fix (`pilot-rc12`):**
- **Input.** Only dotted IPv4 is accepted for `radius_source_ip` and
  `nas_ip` (`422` otherwise). Rows an earlier version stored in mapped form
  are rewritten to dotted IPv4 at nacd start (audited as
  `switch.normalize_ip`). The client file never contains a non-IPv4 or
  duplicate address.
- **Status.** `nac-freeradius-reload` (run by the path unit) checks the
  configuration, restarts FreeRADIUS and records the result with the
  SHA-256 of the client file it validated. nacd's **Configured** and
  **Loaded by FreeRADIUS** (`freeradius_load`) are:
  - `yes` only when that record says the restart succeeded with exactly the
    list nacd would write now **and** a `freeradius` process is running
  - `pending` for up to 60 s after a write
  - `no`, with the reason, otherwise
- **Recovery.** A FreeRADIUS that **failed** (for example on an earlier
  invalid list) is restarted once the list is valid. One that was
  **deliberately stopped** is left stopped and reported `no`.
- **Bursts (`pilot-rc13`).** A change that lands while a reload runs can be
  missed by the systemd path unit, and a burst can hit a start-rate limit.
  Three layers make the reload converge:
  1. **The helper converges on its own.** It waits 1 s to coalesce a burst,
     reloads, then re-hashes the file. If the file changed, it reloads
     again (at most 10 passes). A lock serializes runs. The helper's
     children never inherit the lock, and the lock wait is bounded (120 s),
     so a stuck run fails visibly instead of stalling every later reload.
  2. **No start-rate limit** on the reload service
     (`StartLimitIntervalSec=0`).
  3. **nacd backstop.** If the reload record still names another list 20 s
     after the current list was written, nacd rewrites the identical file,
     so the path unit fires again. It does this at most every 30 s, and
     only while the list is unconfirmed. The 60 s grace period counts from
     when the list content first changed, so a reload path that never
     answers ends in `no`, not an endless `pending`.
- **Record precision.** The record's `at` now has milliseconds.
- **Rehearsed here** with real nacd, the real helper and a real FreeRADIUS
  process, behind a simulated path unit that **drops** every change landing
  while a reload runs:
  - **ASTRA's C2, C3 and cleanup** (8 changes each, back to back or 0.4 s
    apart): each converged in 2.5–3.1 s. 21 of 28 triggers were dropped,
    and the helper's re-hash caught every miss.
  - **One trailing write, re-hash disabled:** recovered by the nacd
    backstop in 24.7 s.
  - **Not covered:** systemd itself. The lab run repeats this on real
    units.
- **Chaos runner ground truth (`pilot-rc13`).** "Loaded" requires all of:
  - FreeRADIUS is active
  - its main process started after the client file's last write (systemd
    microseconds against the file's nanosecond mtime)
  - the reload record says `ok` for exactly the SHA-256 of the current file
  - `freeradius -C` passes

  Each scenario polls until loaded, up to `CHAOS_DEADLINE_SECONDS`
  (default 60), and reports how long it took.

**What is left:**
- After a manual start of FreeRADIUS, the status says `no` until the next
  reload (`systemctl start nac-freeradius-reload.service`). It errs toward
  "not loaded", never toward "loaded".
- `NAC_FREERADIUS_PROCESS_CHECK=off` disables the process check when
  FreeRADIUS runs on another host. That deployment is outside the pilot.

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
  accepted, not that the endpoint is connected now. The note states what
  NAC has:
  - "has received no RADIUS accounting", or
  - "received RADIUS accounting Start (or Interim-Update) … and no Stop
    since", with the time and Acct-Session-Id. That shows the switch opened
    the session, not that the endpoint is still there: NAC learns of a
    disconnect only from a Stop, which can be lost.

  Before `pilot-rc14` the note said "no RADIUS accounting" even when a Start
  was stored (ASTRA, `pilot-aruba-rc3` console run): defect D2, wording only.
  - **D2 fix lab result: PASS** in the API and the console, on exact
    `pilot-aruba-rc4` nacd (`dd2b6137…`, independently built), which
    carries the rc14 code. ASTRA [`2609fdb`](https://github.com/KidCarmi/nac/tree/2609fdb8cd7759c12f7d88b73027201481bc26d2/lab/evidence/aruba-rc4-d2-retest-20261109), 4/4 manifest entries verified
    here.
  - **Method:** a targeted retest. nacd was swapped in place on the
    existing rc3 guest, with the same adapter, switch and session; this was
    not a clean install. Liveness stayed `unknown`; the basis read
    "accounting Start, no Stop", and the console confirmation said a Start
    was received, there was no Stop, and this does not prove a current
    connection.
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
| Windows Server 2025 Standard Evaluation, Desktop Experience (build 26100), one NIC | **about 30–65 s** at every tested lease length and age. Windows changes its address, DNS and route by itself. Windows 10/11 are unverified | 120 s and 3600 s leases; early, near-T1 and console transitions ([evidence `f2cbf3a`](https://github.com/KidCarmi/nac/tree/f2cbf3aeeecba95075aa7e9f1881421006092b30/lab/evidence/windows-lease-rc10-20261107)) |
| Linux (systemd-networkd), one NIC | **only when the old lease expires**. The delay is the lease time left at the CoA. *Inference for this setup:* the worst case is about the full lease. The measured 1-hour run had about 30 min left | 120 s lease: 24–117 s. 3600 s lease: **1,811.9 s** (30 min) for a Restore about 30 min before expiry ([evidence `a6df762`](https://github.com/KidCarmi/nac/tree/a6df7627bcdf87d9e4b3c81d412dc91a949c4bd8/lab/evidence/linux-lease-rc10-20261107)) |

**What this supports:**
- **Windows Server 2025 build 26100:** the tested lease lengths and ages
  (120 s; 3600 s early and near T1).
- **Linux endpoints:** only where an outage of up to the remaining lease is
  acceptable after a quarantine change or a restore. In practice that means
  short leases, and 120 s is the only short lease measured.
- **Long leases with Linux clients:** unresolved.
  - An authoritative DHCP server only moves recovery from lease expiry to
    T2.
  - Short T1/T2 on a long lease failed the reverse direction (no recovery
    within 165 s).

### Recovery boundary (dnsmasq comparison, `pilot-rc10`)

ASTRA's evidence: [`8db6fe8`](https://github.com/KidCarmi/nac/tree/8db6fe899809c2a20db741d08f72e8e3711680e1/lab/evidence/dhcp-server-comparison-rc10-20261107), 55 files, manifest verified here. The
setup held the NAC binary, switch, Linux client, topology and 3600 s lease
constant. Only the DHCP server changed: Ubuntu dnsmasq-base 2.91,
`dhcp-authoritative`, on both segments.

| Transition (Linux, systemd-networkd) | Old segment denied | New segment reachable | How |
|---|---|---|---|
| Early Quarantine (corp → quarantine) | +1.385 s | **+3,112.573 s** (51 m 53 s) | T1 unicast renewal unanswered; broadcast rebind at T2 → NAK in 5 ms → Discover → ACK |
| Near-T1 Restore (quarantine → corp) | +1.687 s | **+1,369.615 s** (22 m 50 s) | same path at T2 |

- **What changed against BusyBox udhcpd:** its near-T1 Restore took
  +1,811.866 s, so dnsmasq was 442 s faster. The BusyBox early Quarantine
  baseline stops at 148 s, because of intervention, so it is not a valid
  comparator.
- **Classification:** recovery **improved but not prompt**. It now ends at
  T2 instead of at expiry, and still runs to many minutes on long leases.
- **What decides it:** the ACKs advertised the default timers (lease 3600 s,
  T1 1800 s, T2 3150 s). The first broadcast attempt, the rebind at T2, is
  the earliest moment a server on the new VLAN can answer. The
  T1 renewal is unicast to the old server, which is unreachable from the new
  VLAN.

**Proposed acceptance target (ASTRA; proposed, not agreed by the owner. The
short-timer configuration below was measured against it and failed on
vEOS-lab):**
- fresh TCP to the new permitted segment within 60 s of the CoA
- old segment denied within 5 s
- no manual endpoint action
- shown in both directions, at early and near-T1 lease ages, and after a
  restart

**The target interacts with the vEOS floor.** On vEOS-lab, an ACKed,
VLAN-bearing CoA alone interrupted forwarding for up to about 58 s, even
without a VLAN change. Windows VLAN moves measured 30–65 s, and three of them
exceeded 60 s (61.4, 61.9, 63.9 s). On this virtual switch the 60 s target
may be unreachable whatever DHCP does. The owner should decide whether it
applies per platform.

**Remedy 1, short T1/T2 with a long lease: measured, FAILED (not recommended).**

ASTRA, [`e86242e`](https://github.com/KidCarmi/nac/tree/e86242efa7985c1b2f5d19ccb9d1f9762778a772/lab/evidence/short-timers-rc10-20261107), 42 files, manifest verified here. Setup: the same
NAC binary, switch and client as above, with authoritative dnsmasq 2.91 and a
3600 s lease. The only change was DHCP options 58 = 30 s and 59 = 45 s, on
both segments. The ACKs carried 3600 / 30 / 45.

| Transition | Old segment denied | New segment reachable | DHCP path |
|---|---|---|---|
| Quarantine (corp → quarantine) | +0.769 s | **+25.040 s** | T2 broadcast rebind at +22.0 s → NAK → Discover → ACK |
| Restore (quarantine → corp), 2.5 s before T1 | +1.217 s | **none through +165.191 s**, the declared end of observation | T1 unicast at +3.1 s to the old server; T2 broadcast at +17.4 s, which **neither the corp nor the quarantine bridge captured**; no ACK; the endpoint kept 10.70.20.52 |

- NAC's part completed: CoA-ACK, Access-Accept VLAN 10 and EAPOL Success
  were all done by +1.1 s, and the switch later showed Et1 on VLAN 10. The
  failure is in reaching the new address.
- The run fails the proposed 60 s target in the reverse direction. One
  forward success does not make the remedy work.
- **Hypothesis, not established:** the reverse rebind at +17.4 s fell
  inside the vEOS-lab post-CoA forwarding gap (about 21–58 s measured
  above). The forward rebind at +22.0 s got through. After a lost rebind,
  RFC 2131 retransmission waits half the remaining lease, at least 60 s:
  about 29 min on a 3600 s lease. If this is right, short timers make
  recovery depend on whether one DHCP attempt lands inside the switch's
  CoA gap. ASTRA states the vEOS internal cause is not identified.
- **Diagnostic only:** after the primary evidence was preserved, a manual
  `networkctl down/up` got a corp NAK/Offer/ACK, and fresh corp TCP at
  +6.163 s.

**Owner direction (2026-11-08): test isolation without readdressing first.**
- **Priority is not a DHCP fix.** It is a per-session **role/ACL quarantine
  that keeps the endpoint's VLAN and IP**, so that no DHCP recovery is
  needed at all.
- **First platform under test:** the Aruba AOS-CX simulator, a capability
  test owned by ASTRA.
  - **Result on ArubaOS-CX Virtual.10.07.0004 (ASTRA, 2026-11-09,
    [`a8c8207`](https://github.com/KidCarmi/nac/tree/a8c820741e475556095452869f682c0c56a89d2d/lab/evidence/aruba-1007-reference-20261109), verified here): per-session IPv4 role enforcement
    works with a reference RADIUS server. It is not NAC-integrated.**
    - **Image provenance:** third-party mirror, not vendor-verified.
    - **Root cause of the 2026-11-08 failure: configuration, not the
      image.** The global 802.1X authenticator was not enabled, and there
      was no interface-specific supplicant.
    - **What was shown.** FreeRADIUS returned `Aruba-User-Role=quarantine`,
      with no nacd involved. On the same IP and VLAN:
      - corp was blocked
      - a healthy same-subnet peer was blocked for traffic the endpoint
        started
      - remediation stayed reachable
      - the `corp` role restored all three
    - **Direct RADIUS CoA role change: demonstrated (IPv4, reference
      RADIUS, [`d3cf745`](https://github.com/KidCarmi/nac/tree/d3cf745afe9d808bafc3f8ff10cb78f9c77ad7fb/lab/evidence/aruba-1007-coa-20261109), verified here).**
      - A session-targeted CoA moved the session corp → quarantine → corp,
        on the same IP and VLAN.
      - Sampled denial came by +0.283 s after the ACK, and restoration by
        +0.529 s. These are bounds for this run, not guarantees.
      - Remediation was reachable in every sample.
      - No re-authentication appeared in the captured window.
      - Compare the VLAN path on vEOS: denial in 1–2.5 s, then 25 s to
        30 min before the endpoint has a usable address.
    - **Peer-initiated same-subnet traffic ([`3ff53a9`](https://github.com/KidCarmi/nac/tree/3ff53a9d7846998cdfe9827240be531bd84c417e/lab/evidence/aruba-1007-peer-20261109), verified
      here): sessions blocked, packets not isolated.**
      - Under quarantine, a peer's TCP connection to the endpoint could not
        complete, and the `corp` role restored it.
      - The peer's SYN still **reached** the endpoint. Only the endpoint's
        reply was dropped at its ingress port.
      - So this role blocks same-subnet sessions in both directions, but it
        does **not** provide full bidirectional packet isolation.
      - Inbound UDP and IPv6 were not tested.
    - **Established flows ([`42be71f`](https://github.com/KidCarmi/nac/tree/42be71fadd3e88466d626cce56c168ecc995f878/lab/evidence/aruba-1007-established-flow-20261109), verified here): data
      blocked, flows not torn down.**
      - While quarantined, an existing TCP connection passed no data.
      - After restore, the **same connection resumed** (+24.8 s, no new
        handshake).
      - It then delivered the messages queued during quarantine.
      - So the role blocks traffic but does not terminate sessions.
    - **IPv6 ([`c26c3ec`](https://github.com/KidCarmi/nac/tree/c26c3ec8f9179aab319c989e96d51f2e7ed360c8/lab/evidence/aruba-1007-ipv6-20261109), verified here): endpoint-initiated,
      same-subnet fresh TCP denied under quarantine and restored by CoA.**
      - The role policy has IPv4 classes only, so how IPv6 is matched is
        not identified.
      - Not tested:
        - an IPv6 remediation allow
        - routed IPv6
        - peer-initiated IPv6
        - established IPv6 flows
        - DHCPv6
    - **Still unverified:**
      - DHCP
      - shared phone+PC ports
      - the NAC-integrated pass
  - **Not selectable.** The full record is in the ADR-0009 capability
    matrix on PR #2
    (`docs/architecture/enforcement-capability-matrix.md`).
- **What it must show:**
  - permitted and denied traffic, measured with continuous probes, not a
    CoA-ACK or a displayed role
  - IPv4 and IPv6
  - same-subnet peers as well as routed destinations
  - a shared phone+PC port
- **What it is checked against:** the same denial, permitted-service
  continuity and recovery measurements as the VLAN path. The 60 s / 5 s
  thresholds are not automatically inherited by role/ACL enforcement.
- **Unmanaged endpoints do not necessarily need a port flap.** Per-session
  policy may avoid the address change entirely.
- **The VLAN-transition options below stay as a compatibility path.** They
  apply where a platform cannot enforce the required restrictions per
  session, or where L2 separation is required.

**Remaining options for VLAN transitions (none measured against the target):**
1. **An endpoint hook for managed Linux** (next to test; reference hook
   and test plan in [`endpoint-dhcp-hook/`](endpoint-dhcp-hook/README.md),
   unqualified).
   - What it does: after 802.1X success following a re-authentication, a
     supplicant action restarts DHCP (`down/up` or equivalent) when the
     gateway does not answer. It retries until the gateway does.
   - Why it is next: it is the only action measured to recover quickly in
     both diagnostics (6.2 s and 6.9 s). It needs no NAC change.
   - Cost: it only covers endpoints the customer manages, and it needs a
     guard so periodic re-authentication doesn't churn addresses.
2. **Shorter leases.** They bound the wait by the lease itself. A lost
   rebind is followed by expiry within the lease, but the same CoA-gap
   timing applies, and a DHCP outage costs addresses within minutes.
3. **A link-state change on single-endpoint ports: explicit last resort.** A port flap would make
   the client restart DHCP on carrier, but it is a product and switch
   behavior change:
   - it needs a documented, supported switch mechanism; vEOS documents no
     CoA bounce, and eAPI `shutdown` would turn observation into control
   - it needs a shared-port model (ADR-0009) that limits it to ports with
     exactly one endpoint
   - it needs owner approval and its own ADR

   It would need a replacement candidate.

4. **Not proposed:**
   - **A switch port bounce.** vEOS has no documented support for it. It
     disconnects every device on the port, which owner direction forbids for
     a PC's DHCP problem. On a single-device port it would also turn eAPI
     from observation into control, which is a design change.
   - **A NAC product change for DHCP.** NAC already denies the old segment
     within about 1.4–1.7 s. Recovery is a property of the DHCP
     client/server pair, and NAC cannot observe or configure it.
   - The product's part is to state the dependency. Later (ADR-0009), the
     dry-run can show the expected recovery for each segment from declared
     DHCP timers.

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
    address. In these 120 s runs it reached the new VLAN's server (BusyBox
    udhcpd in the lab), which **does not answer** it. A rebind is not
    guaranteed to arrive: in the short-timer Restore run it was seen on
    neither segment.
  - When the lease expires, the client starts over with a Discover. The new
    VLAN's server answers that, about 15 s after the rebind.
  - So the Linux endpoint reaches the new VLAN when its old lease
    **expires**. The delay is the lease time left at the CoA, from a few
    seconds up to, by inference, about **the full lease time**. This explains the 24–117 s spread.
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

**Limitation: on vEOS-lab 4.29.2F, an ACKed CoA that carries VLAN attributes
interrupts the endpoint, even without a VLAN change.** That is the CoA form
NAC sends, and the only accepted form tested. It is a switch/CoA operating
limitation, not a NAC product defect. Physical Arista hardware and other
accepted CoA forms are unverified.

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
  - On a session that should stay where it is, expect an outage of up to
    about 58 s on vEOS-lab (the measured maximum).
  - **Console wording:** the `pilot-rc10` CoA confirmation says
    "about 20 s" and "at its next DHCP renewal". Both are misleading:
    - same-VLAN gaps reached about 58 s
    - the Linux client waits for lease expiry

    The next candidate corrects the wording. It is not a lab-contract
    defect, so it does not move `pilot-rc10`.
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
  - **Single fresh-instance console workflow: PASS** on `pilot-rc10`. Onboarding, binding, preview/publish, Quarantine/Restore and the Account change were all done in the console on one fresh instance with a real vEOS switch ([`af46f02`](https://github.com/KidCarmi/nac/tree/af46f02eb1f9f5f3bb07ef6ac441f12b6840465d/lab/evidence/clean-ui-rc10-20261107)).
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
