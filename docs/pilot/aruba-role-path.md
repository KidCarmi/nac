# Aruba AOS-CX role path (`pilot-aruba-rc2`)

**Status: owner-approved bounded integration, not lab-qualified.**
- `pilot-rc10` (`b39649f`) stays the tested baseline. This candidate does
  not replace it.
- `pilot-aruba-rc2` is `pilot-aruba-rc1` plus the known defect D1 fix from
  `pilot-rc12`, and nothing else. A switch shows as Configured only after
  FreeRADIUS has loaded it; see [known limitations](known-limitations.md).
  Use rc2. rc1 carries D1.
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
| DHCP | unverified (static address in that lab) |

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

**Install:**
1. Build: `make pilot-bundle VERSION=pilot-aruba-rc2` at the pinned commit.
   Check `BUNDLE-MANIFEST.txt` `nacd_sha256` against the pin.
2. Install as in [installation](installation.md), on a clean control node.
3. Compare the adapter files with the pinned hashes.

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
