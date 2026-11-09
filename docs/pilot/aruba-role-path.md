# Aruba AOS-CX role path (`pilot-aruba-rc4`)

**Status: owner-approved bounded integration, not lab-qualified.**
- `pilot-rc10` (`b39649f`) stays the tested baseline. This candidate does
  not replace it.
- `pilot-aruba-rc4` is `pilot-aruba-rc3` plus the D2 session-liveness
  wording fix (`pilot-rc14`), merged, and nothing else. `pilot-aruba-rc3` is
  `pilot-aruba-rc1` plus the known defect D1 fix and its reload-race fix
  (`pilot-rc13`). A switch
  shows as Configured only after FreeRADIUS has loaded it, also under
  bursts; see [known limitations](known-limitations.md). rc1 and rc2 are
  superseded: rc2's D1 retest failed.
- The candidate also carries the rc11 console wording change.
- Pins (commit, nacd and adapter hashes) are in
  [qualification status](qualification-status.md).

## What it does

| | Arista EOS (unchanged) | Aruba AOS-CX (this candidate) |
|---|---|---|
| Strategy | `vlan` | `role` |
| Access-Accept | `Tunnel-Type`, `Tunnel-Medium-Type`, `Tunnel-Private-Group-Id` | `Aruba-User-Role` (vendor 14823, type 1) naming a **local role on the switch**; no Tunnel-* attributes |
| Quarantine / Restore CoA | same Tunnel-* attributes; the switch re-authenticates the session | User-Name, NAS-IP-Address, NAS-Port-Id, Calling-Station-Id and `Aruba-User-Role`; the switch changes the session's role **in place, without re-authentication** |
| VLAN and endpoint IP | change with the segment | **never change** |
| Switch observation | eAPI (`show dot1x hosts`), optional | **none**: no AOS-CX adapter |

A decision for a segment with no role mapping is a **reject**. NAC never
falls back from role to VLAN, or to an unrestricted accept.

**Out of scope:**
- MAB and Disconnect-Request
- switch observation for AOS-CX
- creating roles or policies on the switch: the operator configures them
- any stronger isolation profile, which stays ineligible
- a general multi-vendor strategy framework

## What the quarantine role does (and does not do)

What the quarantine role blocks is **switch configuration, not NAC**. NAC
selects the role; the role's policy decides the traffic. Only the following
was demonstrated, on ArubaOS-CX Virtual 10.07 with a reference RADIUS server
and **without nacd**: one endpoint, IPv4, policy "permit remediation, drop
other IPv4" (evidence in the
[capability matrix](../architecture/enforcement-capability-matrix.md)).

| Behavior under the quarantine role | Lab result |
|---|---|
| Fresh TCP to corp | denied |
| Fresh TCP to a same-subnet peer, both directions | denied (sessions cannot complete) |
| Remediation | allowed |
| Data on an established TCP flow | blocked while quarantined |
| **Packet isolation** | **not met**: a peer's inbound SYN still reached the endpoint |
| **Tear down existing TCP state** | **did not happen**: after Restore the same connection resumed, and data queued during quarantine was delivered |
| IPv6 | only endpoint-initiated same-subnet fresh TCP was tested (denied). Everything else is unverified |
| IPv4 UDP, endpoint-initiated | corp **denied**, remediation allowed (direct role CoA, no nacd; [`f441a25`](https://github.com/KidCarmi/nac/tree/f441a25b217fad4c1a4977da339c03d872c91da6/lab/evidence/aruba-udp-reference-20261109), 8/8 manifest entries verified here). Under quarantine, 3 corp datagrams were on the endpoint tap and 0 at the corp service tap; remediation 3/3 requests and replies. After restore, corp and remediation each 3/3. Peer-initiated, routed-ingress and IPv6 UDP, and already-open UDP state, are not tested |
| DHCP (one Linux client, one server) | **kept working under the role** (direct role CoA, no nacd; [`b0e5f47`](https://github.com/KidCarmi/nac/tree/b0e5f47d242ad15c04085f727b9d00c52e8f42f7/lab/evidence/aruba-dhcp-reference-20261109), 10/10 manifest entries verified here). Ubuntu 22.04.5 systemd-networkd, BusyBox udhcpd 1.30.1, 120 s lease / T1 60 / T2 105. Under quarantine the T1 unicast renewal got no reply, and the T2 broadcast rebind got an ACK; the address stayed usable past the original expiry. Same address, route and DNS through quarantine and restore. First corp SYN after the quarantine ACK was denied (+0.797 s); first corp HTTP 200 after the restore ACK at +0.533 s. 0 EAPOL and 0 new RADIUS authentications in both CoA windows. Other clients, servers and lease lengths are unverified |

## Setup

1. **Register** the switch in the console (**Switches**):
   - Platform: **Aruba AOS-CX**
   - corp role, quarantine role (defaults `nac-corp`, `nac-quarantine`)
   - access VLAN, access ports (`1/1/1`), CoA UDP port (3799)
   - the FreeRADIUS address the switch sends to

   The same through the API:
   ```json
   {"name": "aos-cx-1", "platform": "aruba-aos-cx", "radius_source_ip": "10.70.99.11",
    "radius_secret": "...", "segment_roles": {"corp": "nac-corp", "quarantine": "nac-quarantine"},
    "access_vlan": 10, "access_ports": ["1/1/1"], "radius_server_ip": "10.70.99.2"}
   ```
   `radius_source_ip` and `nas_ip` must be dotted IPv4 (`422` otherwise).
   The switch shows **Configured: yes** only once FreeRADIUS has loaded it.
   Wait for that before testing logins.
2. **Apply** the generated reference configuration (**Switch** page). It
   includes the global `aaa authentication port-access dot1x authenticator`
   → `enable`. Without it the switch sends no RADIUS at all; that was the
   earlier lab failure.
3. **Add the quarantine policy** to the quarantine role on the switch. The
   generated configuration leaves a comment where it goes; NAC does not
   write switch policy.
4. **Check on the switch:** `show port-access clients` after a login and
   after each CoA.

## API and console semantics

Every client (console, API, MCP) gets the same contract from
`GET /api/v1/sessions/{id}`:
- `enforcement`: platform, strategy, mechanism, observation, limitations
- `requested.role`
- `observed.freshness = unavailable`
- `endpoint_reachability.state = not_observed`

| Action | NAC records | Sent to the switch | Result states and meaning |
|---|---|---|---|
| Authentication | decision with `role`; session `requested.role` | Access-Accept + `Aruba-User-Role` | missing role mapping → Access-Reject |
| **Quarantine** | identity-scoped override, **first** | CoA with the quarantine role | `requested` → `sent` → `acked` / `nak` / `timeout` / `failed` |
| **Restore** | clears the override | CoA with the role the active policy grants | as above. If the policy would grant **no** role: `422`, nothing recorded, the session **stays quarantined** (a role CoA cannot carry a reject) |
| Re-authenticate | — | CoA with the active policy's role | same `422` rule |

What the states mean:
- **`acked` is final for a role CoA.** It means the switch accepted the
  request. It is **not** verification that traffic is restricted, and NAC
  does not observe the switch. A later re-authentication that returns the
  same role is noted in the CoA detail, but never turns it into `observed`.
- **`nak` / `timeout` / `failed`:** shown as such, never as applied. The
  override stays recorded and applies at the session's next authentication.
- **Stale session:** CoA only targets an `active` session. Before each
  retry NAC re-checks that the captured session is still current. If it
  ended or changed, the CoA is `failed` and is never re-sent to another
  session.
- **Persistence:** the quarantine override follows the identity (switch +
  port + MAC + certificate issuer and serial). It survives:
  - re-authentication
  - accounting Stop/Start and session replacement
  - a failed EAP attempt
  - a nacd restart

  Only Restore clears it, exactly as on the VLAN path.

## Lab results (NAC-driven)

### `pilot-aruba-rc4`: targeted D2 retest PASS (ASTRA, 2026-11-09, [`2609fdb`](https://github.com/KidCarmi/nac/tree/2609fdb8cd7759c12f7d88b73027201481bc26d2/lab/evidence/aruba-rc4-d2-retest-20261109), 4/4 manifest entries verified here). **Not fully qualified**

**Scope.** Exact `0148bf88985c5da9086b54aafd002fbfbfe71b75`, built with
Go 1.25.0, linux/amd64, CGO off; the nacd SHA-256 matched `dd2b6137…`.
**nacd was swapped in place** on the existing isolated rc3 guest, with the
same FreeRADIUS adapter, switch and session. This was not a clean rc4
install.

| Check | Result (ASTRA) |
|---|---|
| D2 wording, API and console | **PASS.** An accepted accounting Start (Acct-Session-Id `1794254508980`): liveness `unknown`, basis "accounting Start, no Stop". The console action confirmation says a Start was received, there was no Stop, and this does not prove a current connection |
| Regression smoke: console Quarantine / Restore | **PASS within the observed scope.** Quarantine ACK 20:12:13.978 UTC: corp fresh TCP timed out, remediation 200. Restore ACK 20:14:07.617 UTC: corp 200, remediation 200. Final switch session: dot1x `Success`, role `corp`, VLAN 10 on 1/1/1 |
| Not run on rc4 (BLOCKED in ASTRA's package) | A clean rc4 install, admission, policy, chaos, restart, Arista, Windows and physical-switch regression. rc4 is **not** fully qualified; the unchanged code's rc3 results are in the next section |

, A1–A9 PASS, operator-status wording FAIL (ASTRA, 2026-11-09, [`148e196`](https://github.com/KidCarmi/nac/tree/148e19614d58a9239226d5f212cfad9fac4ac504/lab/evidence/aruba-rc3-qualification-20261109), 77/77 manifest entries verified here)

**Scope.** Exact `d96bb741e8f36019af4c6d5152491c2e0a874fe6`, nacd
`01e88d8b…` (independently built; the installed hashes matched the pins),
real systemd, FreeRADIUS 3.2.5, ArubaOS-CX Virtual 10.07.0004, one-NIC Linux
DHCP endpoint. This result is kept **separate from rc2, which stays FAIL**.

**One finding: D2, operator-status wording** (`U1` in ASTRA's package). The
liveness note said "NAC receives no RADIUS accounting" after a real
accounting Start. It is fixed in `pilot-aruba-rc4`, a nacd-only change.
**No enforcement defect was found.**

| Case | Result (ASTRA) |
|---|---|
| Clean install and onboarding | **PASS.** Fresh guest, `--test-pki`; binary, adapter and config hashes matched the pins |
| **D1 chaos, real systemd** | **PASS, 20/20.** Eight rapid registrations, eight secret rotations and cleanup converged to the exact current file hash; process kill and recovery; outage. Supersedes the rc2 D1 failure for this code |
| A1 admission | **PASS.** No authentication: corp timed out. Valid but unbound certificate: denied. After binding the validated issuer and serial and publishing v2: admitted to `corp` |
| A2–A4 Quarantine / Restore, **API** | **PASS.** Quarantine ACK 19:21:15.166296Z: first blocked corp SYN +0.220 s, first remediation HTTP 200 +0.225 s; Restore: corp back at +0.611 s. No re-authentication, no EAPOL, no DHCP address change |
| Quarantine / Restore, **console** | **PASS** (same session, signed in). Quarantine ACK 19:50:24.612334Z: first denied corp SYN +0.301 s; 29 corp successes before, 0 after; remediation continued. Restore ACK 19:52:36.429036Z: first corp HTTP 200 +0.168 s. No EAPOL, no authentication, no address change |
| A5 persistence | **PASS.** The quarantine stayed applied after accounting Stop/Start created a new session, after a nacd restart and after a control-guest reboot (not a full EVE restart) |
| A6 NAK | **PASS.** Quarantine mapped to a nonexistent role: NAK, Error-Cause 506. The switch and fresh TCP stayed `corp`, and the pending override applied at the next authentication |
| A7 timeout | **PASS.** Only candidate → switch UDP/3799 dropped: 3 requests over 6 s, none reached the switch. Reported as timeout; the pending Restore applied at the next authentication |
| A8 stale session | **PASS.** Quarantine on an ended session: `422` "session is ended, not active", no re-targeting |
| A9 traffic limits | As the reference: an established corp TCP socket delivered 0 of 8 payloads during quarantine (no FIN or RST) and resumed after Restore. Peer → endpoint fresh TCP was blocked, although the peer's SYN reached the endpoint and the SYN-ACK was suppressed. Not packet isolation |
| Invalid client certificate | **PASS.** Disposable self-signed client certificate on a freshly cleared port: Access-Reject, switch `Server-Reject` with no role, corp timed out. The valid certificate then authenticated again |
| Rule-only policy change and restore (console) | **PASS.** The console changed only the existing rule's segment `corp` → `quarantine`; the preview predicted `allow corp` → `allow quarantine` for the active identity, and v3 was published. The existing corp session stayed reachable until a switch-local re-authentication: publication applies at the next authentication, by design. Then, for the same certificate and port: the v3 decision matched the same rule ID; the real Access-Accept carried `Aruba-User-Role` `quarantine`; the switch role was `quarantine` on VLAN 10; fresh corp and peer TCP timed out; remediation answered 78/78. The console restored v2, and quarantine persisted until a second switch-local re-authentication, after which NAC, RADIUS, the switch and the probes all returned to `corp` |
| Session status wording | **FAIL (D2):** "NAC receives no RADIUS accounting" despite a real Start. Fixed in rc4 |
| Not run | The full clean-instance console workflow, a full EVE restart, and the Arista regression on this exact candidate |
| Blocked | Windows 10/11, physical Aruba (none authorized); MAB is outside this pilot |

### `pilot-aruba-rc2`: candidate FAIL (D1); A1–A3 PASS via API (ASTRA, 2026-11-09, [`a764e52`](https://github.com/KidCarmi/nac/tree/a764e52f6118f09c8e3dabecbff22613e99db983/lab/evidence/aruba-rc2-qualification-20261109), 17/17 manifest entries verified here)

**Scope.** This ran on `pilot-aruba-rc2`, which **FAILED its D1 retest**
(missed trailing reload). ASTRA recorded a manual reload so that it could
continue; that intervention is recorded and is not a pass. Quarantine and
Restore were **product API** actions: the console was blocked at the
disposable certificate warning. The package also preserves the failed chaos
log, the install report, the A1–A3 CoA PCAPs and the endpoint probes. The
role path is unchanged in `pilot-aruba-rc3`; only the reload path changed.

| Step | Result (ASTRA) |
|---|---|
| Clean install | PASS. Installed nacd `333feaa7…` and adapter `3c45063e…` match the pins |
| Onboarding | ArubaOS-CX 10.07.0004 at `10.70.99.1` registered through the API, roles `corp`/`quarantine`; FreeRADIUS loaded = yes. Only the isolated switch's RADIUS moved to the candidate (`10.70.99.5`); the reference FreeRADIUS was stopped |
| Unauthenticated port | corp blocked |
| A1 first login | Valid EAP-TLS certificate rejected as unbound. The decision traced the authenticated issuer `/CN=NAC Test CA` and serial `5D3DC6EF4E31072E45ADABD4F8CC7338A2C0A963`. After session bind, publishing `bound-corp` (v2) and an endpoint re-authentication: Access-Accept with `Aruba-User-Role` `corp`, switch `Success` `corp`, fresh corp TCP HTTP 200 |
| A2 Quarantine (API) | CoA-ACK at 18:58:49.235421Z; switch `quarantine`; endpoint kept `10.70.10.50` (DHCP). First fresh corp SYN after the ACK at +0.374 s, and 0 corp HTTP 200 thereafter. Remediation first HTTP 200 at +0.378 s. No EAPOL and no new RADIUS authentication |
| A3 Restore (API) | CoA-ACK at 19:00:23.540160Z; switch `corp`; corp first HTTP 200 at +0.113 s. Same IP; no EAPOL and no authentication |
| Contract | `endpoint_reachability` `not_observed`; AOS-CX switch observation `unavailable` |

**ASTRA's verdict:** **FAIL** (D1: C2, C3 and cleanup did not converge;
after more than 60 s nacd reported `no`; a manual
`systemctl start nac-freeradius-reload.service` restored `yes`).
- **Mapped inputs:** `422`.
- **Deliberately stopped FreeRADIUS:** reported `no`.
- **Console actions:** BLOCKED.
- **A5–A10:** not run on this candidate.
- **Windows and physical Aruba:** not tested.

**Open:**
- D1 retest on `pilot-aruba-rc3` with real systemd (ASTRA, in progress)
- A2/A3 through the console
- A4–A10

## Verified in the build session (no real switch)

- **Integration tests**, PostgreSQL (`test/integration/aruba_role_test.go`):
  - the role reply carries no Tunnel-*
  - the CoA is session-targeted and carries the VSA but no Tunnel-*
  - `acked` with "not verification"
  - the quarantine persists through re-authentication, accounting
    Stop/Start, session replacement and a nacd restart
  - a CoA timeout is reported as `timeout`
  - a Restore the policy would turn into a deny is refused, and the session
    stays quarantined
  - stale-session refusal
  - validation: Aruba with VLANs, missing quarantine role, duplicate role,
    bad role name, eAPI on Aruba, Arista with roles
- **Arista regression:**
  - the VLAN CoA still carries Tunnel-* and never the VSA
  - all earlier integration tests pass
  - `scripts/ui-walkthrough.js`: 13/13
- **Real FreeRADIUS 3.2.5 adapter** (`scripts/e2e-freeradius.sh`, 12/12):
  - the Access-Accept carries `Aruba-User-Role = "nac-corp"` and no Tunnel-*
  - an operator quarantine applies at the next login as
    `Aruba-User-Role = "nac-quarantine"`
  - the outage path strips `Aruba-User-Role`
- **Console** (`scripts/ui-aruba-role.js`, 13/13, fake AOS-CX CoA
  responder):
  - Aruba switch form
  - reference configuration
  - corp → Quarantine → Restore through in-page confirms
  - role CoA packets
  - acked stays acked after re-authentication
  - no native dialogs or page errors

None of this is a lab result. The traffic table above is the only traffic
evidence, and it was gathered without nacd.

## Lab qualification plan (ASTRA)

**Fixed:**
- the pinned candidate (qualification status)
- ArubaOS-CX Virtual 10.07.0004 (the image used for the reference evidence)
- FreeRADIUS 3.2.5 with this candidate's adapter
- a fresh database, in the separate Aruba topology
- `pilot-rc10` and its topology stay untouched

### Exact pins (`pilot-aruba-rc4`)

Deployment files are byte-identical to `pilot-aruba-rc3`; only nacd changed.


| What | Value |
|---|---|
| Commit | `0148bf88985c5da9086b54aafd002fbfbfe71b75` |
| nacd (`/usr/local/bin/nacd`; `BUNDLE-MANIFEST.txt` and `/etc/nac/install-report.txt` `nacd_sha256`) | `dd2b61371585d317bf07684733996c69e265dfdb9e762e354eeca4f7821b49d7` |
| `freeradius_adapter_sha256` line in `/etc/nac/install-report.txt` | `3c45063efb451ff86dac23d695e9c57c307b86ef8054b790a759cbb63e80db32` |
| Running commit (install report, `GET /version`) | `0148bf8` |

The installer copies these files verbatim, so the installed files have the
same SHA-256 as at the commit:

| Installed as | Source in the bundle | SHA-256 |
|---|---|---|
| `/etc/freeradius/3.0/sites-available/nac` | `deployments/freeradius/sites-available/nac` | `d992840840bad3647c7bbb71afae59eaf4bd0981303b9465d7469c0306b75d1e` |
| `/etc/freeradius/3.0/mods-available/nac_eap` | `deployments/freeradius/mods-available/nac_eap` | `ffcf910c2bdda8b792e5f6073c00cfc10a5792c5b337eab2f4f26a87a7d2b8cc` |
| `/etc/freeradius/3.0/mods-available/nac_rest` | `deployments/freeradius/mods-available/nac_rest` | `168ae4550ff79b9cfb5065a2113f66b5655ba2993ca93665beb77dcfb5078546` |
| `/etc/freeradius/3.0/mods-available/nac_json` | `deployments/freeradius/mods-available/nac_json` | `6008b75fa581390d0beb46a4d7645b816ba537e73959d539413b20ae5724cefc` |
| `/etc/freeradius/3.0/mods-available/nac_outage` | `deployments/freeradius/mods-available/nac_outage` | `c483927350e1b70e688d1180bfb382aa324bed3f1890e4631556b60dcb5a8095` |
| `/usr/local/sbin/nac-freeradius-reload` | `deployments/pilot/nac-freeradius-reload` | `a8a31d6d1b5b576df4856317d4b65de8cd25e56aabd2889451b209ba6161a113` |
| `/usr/local/sbin/nac-reset-password` | `deployments/pilot/nac-reset-password` | `a5c17570fc0d488e508f73a1d1b7a25124a0c6b9fa2ae50c43b40b4a24618a10` |
| `/etc/systemd/system/nac-freeradius-reload.path` | `deployments/pilot/systemd/nac-freeradius-reload.path` | `fc1dd669de91bda5d3a4c709f5e61dd702b081c46e43e7cf39dfcb6b03e74495` |
| `/etc/systemd/system/nac-freeradius-reload.service` | `deployments/pilot/systemd/nac-freeradius-reload.service` | `c738690d8a3940ed934733df2dde31b20b17409b377be23376390649584b59e4` |
| `/etc/systemd/system/nacd.service` | `deployments/pilot/systemd/nacd.service` | `162f610a20a7f5923543cf052e839ca41d199b0bb7bf16be3f2e0753ad6307eb` |
| — (installer itself) | `deployments/pilot/install.sh` | `a80fe96707b05a7e5cc2b25edee6e4dcfb299e5669b905ee5d68ae73a8b76a5c` |
| — (FreeRADIUS setup step) | `deployments/pilot/freeradius-setup.sh` | `4d838f3e8303f9aaae22fffb3fab777d66c0912463036e3bbe221f7dfba6d124` |

The bundle tarball's own hash changes with file times; verify nacd and the
files above instead.

### Clean install (fresh Ubuntu 24.04 control node)

1. **Build** the bundle (Go toolchain `go1.25.0` is fetched if needed):
   ```bash
   git checkout 0148bf88985c5da9086b54aafd002fbfbfe71b75
   make pilot-bundle VERSION=pilot-aruba-rc4
   ```
   `nac-pilot-pilot-aruba-rc4/BUNDLE-MANIFEST.txt` inside the tarball must
   show the nacd hash above.
2. **Copy** `dist/nac-pilot-pilot-aruba-rc4.tar.gz` to the control node,
   then extract it:
   ```bash
   tar -xzf nac-pilot-pilot-aruba-rc4.tar.gz && cd nac-pilot-pilot-aruba-rc4
   ```
3. **Install** with the CA that issued the endpoint's certificate, and a
   server certificate for FreeRADIUS. Add `--proxy URL` if apt needs one.
   ```bash
   sudo ./deployments/pilot/install.sh --ca ca.pem --server-cert radius.pem --server-key radius.key
   ```
   `--test-pki` generates a disposable CA instead, and the endpoint would
   then need a client certificate from it.
4. **Verify the install:**
   - `/etc/nac/install-report.txt`: `nacd_sha256`,
     `freeradius_adapter_sha256` and `running nacd: commit 0148bf8`
   - the file hashes above, with `sha256sum`
   - `systemctl status nacd freeradius nac-freeradius-reload.path` (all
     active)
   - `curl -fs http://127.0.0.1:8080/readyz` returns ready
5. **Sign in** at `https://<control-node>:8443/` with the credentials in
   `/root/nac-initial-admin.txt`, then change the password (**Account**).

### Aruba switch onboarding

1. **One RADIUS server only.** Stop the reference FreeRADIUS used for the
   platform runs. Later, remove its `radius-server host` and
   `radius dyn-authorization client` entries from the switch.
2. **Register the switch** (console **Switches**):

   | Field | Value |
   |---|---|
   | Platform | Aruba AOS-CX |
   | Name | any |
   | RADIUS source IP | the switch address FreeRADIUS sees, in dotted IPv4. With `vrf mgmt`, that is the management interface address |
   | NAS-IP-Address for CoA | leave empty if it is the same address |
   | RADIUS shared secret | 16+ characters |
   | Corp role, Quarantine role | the role names **as they exist on the switch** (`corp`, `quarantine` in the reference topology) |
   | Access VLAN | 10 |
   | Access ports | `1/1/1` |
   | FreeRADIUS address | the control node's IP; it is also the CoA source |
   | CoA UDP port | 3799 |

3. **Wait for Configured: yes** ("Loaded by FreeRADIUS"). `pending`
   settles within about 12 s in the chaos runs; `no` states the reason.
4. **Apply the generated configuration.** On the **Switch** page, use
   **Show with secret** (audited), or copy it and replace
   `<RADIUS_SHARED_SECRET>`. For the reference values it reads:
   ```
   radius-server host <control-node> key plaintext <secret> vrf mgmt
   aaa authentication port-access dot1x authenticator
       enable
   radius dyn-authorization enable
   radius dyn-authorization client <control-node> vrf mgmt secret-key plaintext <secret>
   port-access role corp
       vlan access 10
   port-access role quarantine
       vlan access 10
   interface 1/1/1
       no shutdown
       no routing
       vlan access 10
       aaa authentication port-access dot1x authenticator
           enable
   ```
   - Compare it with the running configuration and apply what differs.
   - The role blocks only set `vlan access`. They do **not** remove the
     existing `q-policy` association on `quarantine`, and that association
     must stay.
   - If the switch reaches the control node outside the management VRF,
     drop `vrf mgmt` from both RADIUS lines.
   - Finish with `write memory`.
5. **First login** (unbound): **Sessions** shows the endpoint denied, with
   the validated certificate serial and issuer.
6. **Bind and publish:**
   - **Devices**: add the record (MAC `00:50:00:00:3e:00`).
   - **Session**: **Bind**.
   - **Policy**: rule `identity.bound eq true` → allow `corp`, then
     **Preview**, then **Publish**.
7. **Re-authenticate** with switch-local `port-access reauthenticate` (as
   in the reference runs), or reconnect the endpoint.
   - The session shows role `corp`, strategy `role`, and the switch "not
     observed".
   - FreeRADIUS sends `Aruba-User-Role = "corp"` and no Tunnel-\*
     attributes.
   - The switch shows `corp` in `show port-access clients`.
8. **Lab state:** the endpoint is on DHCP in the corp role. After any
   component reboot, restart the transient `aruba-lab-dhcp.service` on the
   corp service before testing.

**Cases.** Each is an uninterrupted run. Record:
- the product action time
- CoA request, ACK/NAK and its time
- `show port-access clients`
- endpoint address, VLAN and route
- fresh TCP to corp, remediation and a peer
- the console and API state

| # | Case | Expected |
|---|---|---|
| A1 | corp login | Access-Accept with `Aruba-User-Role=nac-corp`, no Tunnel-*; console shows role `nac-corp`, strategy `role`, switch not observed |
| A2 | Quarantine (console) | CoA with `nac-quarantine` to the session; CoA-ACK; switch role `nac-quarantine`; **DHCP address and VLAN unchanged**; corp denied, remediation allowed; console `acked` and says it is not traffic verification |
| A3 | Restore (console) | CoA with `nac-corp`; ACK; corp reachable; address and VLAN unchanged |
| A4 | ACK versus endpoint reachability | Record any gap between the ACK and the first denied / first successful probe. NAC claims only the ACK |
| A5 | Override persistence | Quarantine, then: switch-local re-authentication; accounting Stop/Start (endpoint reconnect); nacd restart; full restart. The endpoint gets `nac-quarantine` each time until Restore |
| A6 | CoA NAK | Make the switch refuse, e.g. with the wrong CoA secret or a wrong NAS-Port-Id. Console shows `nak` (or `timeout`, if the switch drops it silently) and never applied; the override applies at the next login. **Also record**, without assuming, what the switch answers for a role name it does not have |
| A7 | CoA timeout | Block UDP/3799. Console shows `timeout`; same override rule |
| A8 | Stale session | Disconnect the endpoint (accounting Stop), then Quarantine. Expect `422`, or a CoA `failed` without re-targeting |
| A9 | Traffic limitations | Repeat the established-flow and peer-initiated checks through NAC. Expected: the same limits as the reference (not packet isolation, no TCP teardown) |
| A10 | Arista regression | On the rc10 vEOS topology, with this candidate in a disposable instance only: `scripts/ui-walkthrough.js` plus one Quarantine/Restore. VLAN behavior identical to rc10 |

**Verdict:**
- **PASS** needs A1–A8 and A10 as expected.
- A9 records limits; it does not fail the candidate unless the behavior is
  worse than the reference.
- A pass qualifies only this role/policy combination on this image. It
  does not qualify packet isolation, IPv6 beyond what was tested, MAB,
  DHCP on other servers, or physical AOS-CX.
