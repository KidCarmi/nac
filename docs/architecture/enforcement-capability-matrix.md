# Enforcement capability matrix (ADR-0009)

[ADR-0009](../../adr/0009-access-intent-and-enforcement-strategies.md)
selects an enforcement strategy only where a capability record shows that
it covers every requirement of the decided access profile. This file holds
those records:
- one row per **platform build + configuration + strategy**
- per requirement, one of:
  - **demonstrated**: shown in a lab run with real traffic, with an evidence
    reference
  - **unverified**: not shown either way
  - **unsupported**: shown or documented as not working on that build

Vendor documentation never counts as demonstration. CLI acceptance, a
displayed role or a CoA-ACK is not enforcement.

Requirement keys follow ADR-0009 §1:
- `deny_routed`
- `deny_same_subnet_peers`
- `allow` (remediation)
- `cut_established_flows`
- `ipv6`

Extra rows:
- **per-session:** the restriction applies to one 802.1X/MAB session, not
  the whole port
- **phone+PC port:** shared-port behaviour
- **NAC-integrated:** driven by nacd through RADIUS and CoA, not by a
  reference harness or manual CLI

## Arista vEOS-lab 4.29.2F, strategy `vlan` (today's product path)

The NAC candidate is `pilot-rc10` (`b39649f`, nacd `35699eac…`).
Changes address: **yes**.

| Requirement | Status | Evidence / caveat |
|---|---|---|
| per-session | demonstrated | the CoA targets one session (NAS-Port-Id, MAC); `fa2950c`, `af46f02` |
| `deny_routed` (corp from quarantine) | demonstrated | old segment denied within about 1–2.5 s in every run (closeout `fa2950c`, timing `7b34ef5`, leases `f2cbf3a`/`a6df762`, `8db6fe8`, `e86242e`) |
| `allow` (quarantine service) | demonstrated | quarantine TCP reachable after re-addressing (same packages) |
| `cut_established_flows` | demonstrated, once | one established corp TCP connection failed 2.8 s after Quarantine and did not reconnect (`f2cbf3a`) |
| `deny_same_subnet_peers` | unverified | the endpoint moves to another subnet; peers *inside* the quarantine VLAN were not probed |
| `ipv6` | unverified | IPv4 only |
| phone+PC port | unverified | one endpoint per port |
| NAC-integrated | demonstrated | nacd → FreeRADIUS → vEOS, VLAN-bearing CoA |
| caveats | — | <ul><li>an accepted VLAN-bearing CoA interrupts forwarding for up to about 58 s (`7b34ef5`)</li><li>re-addressing time depends on the DHCP client and server ("Recovery boundary" in `docs/pilot/known-limitations.md`)</li></ul> |

## Arista vEOS-lab 4.29.2F, strategy `role_acl`

Not modelled. No capability record, so not selectable.

## Aruba AOS-CX simulator ArubaOS-CX Virtual.10.07.0004, strategy `role_acl`

**Status: qualification blocked; root cause unresolved.** Not selectable.

This corrects an earlier version of this record, which said "BLOCKED on
this image" and called per-session role/ACL unsupported. The evidence does
not establish that.

| | |
|---|---|
| Build SHA | `64b88cbfc38a48a03d7b8b6229137e37f59c9310` |
| Image SHA-256 | `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25` |
| Provenance | Third-party mirror supplied by the owner, **not vendor-verified**. A compatibility experiment in a separate six-node EVE topology; the rc10 NAC baseline was untouched |
| Evidence | ASTRA, 2026-11-08. The sanitized package is held locally and not yet published to `astra/lab-evidence`, so it has not been verified here |

There are two gates, recorded as separate facts:

| # | Fact | Kind | What it does and does not show |
|---|---|---|---|
| 1 | No port-access client session formed. A local quarantine role/policy displayed "Applied" | **observed** | The **authentication / session-formation** gate failed in this configuration. The first missing protocol step (EAPOL → Access-Request → response → authenticated session) has not been identified. Configuration or integration error is not excluded |
| 2 | MAC-auth trial: **zero RADIUS requests in the server-side capture**; corp stayed reachable | **observed** | No Access-Request reached the server capture. It does **not** show that the switch never sent one: there was no concurrent switch-side or uplink capture. MAB stays out of the product adapter |
| 3 | HPE's 10.07.0004 simulator release notes say **classifier policy** is configurable but nonfunctional ([5200-8082](https://arubanetworking.hpe.com/techdocs/AOS-CX/10.07/OVA/5200-8082.pdf)) | **documented limitation** | It applies to classifier policies. HPE distinguishes these from port-access policies tied to roles. It does not, on its own, establish that role-based enforcement is unsupported |
| 4 | Per-session data-plane enforcement: does an authenticated client's role restrict its traffic, and can it be changed by dynamic authorization while the client keeps its IP and VLAN? | **untested** | Never reached, because gate 1 failed |
| 5 | A static ingress port ACL moved corp to timeout while remediation stayed HTTP 200; removing it restored both | **observed, not counted** | It is port-wide, not per-session, so it is not evidence for `role_acl` |

**Requirement status:** every ADR-0009 requirement for this strategy is
**unverified**. That covers per-session, `deny_routed`, `allow`,
`deny_same_subnet_peers`, `cut_established_flows`, `ipv6` and phone+PC
port. NAC-integrated has not started.

**Next step: ASTRA's bounded diagnostic run, set by the owner on PR #1.**
- Reproduce HPE's reference 802.1X lab: one endpoint, one switch, reference
  FreeRADIUS, no nacd.
- Capture EAPOL and switch-to-RADIUS traffic at the same time.
- Identify the first missing step.
- Then test a minimal local port-access role: permit one service, deny
  another.
- Then test a role change through dynamic authorization while the client
  keeps its IP and VLAN.
- End with a verdict: configuration/integration defect, image/version
  limitation, or unresolved, with the missing evidence named.
- Physical hardware or another image comes in only if that verdict calls
  for it. A newer image is not assumed to fix anything.

**Downstream requirements still to qualify** once a session works:
- shared phone + PC port
- same-subnet peers
- IPv4 and IPv6
- established flows
- remediation continuity

## Aruba AOS-CX, strategy `vlan`

Not tested.
