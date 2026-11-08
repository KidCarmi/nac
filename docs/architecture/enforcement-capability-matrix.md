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

**Status: BLOCKED on this image.** Not selectable.

**Build:**
- build SHA `64b88cbfc38a48a03d7b8b6229137e37f59c9310`
- image SHA-256 `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25`

**Provenance:** the image is a third-party mirror supplied by the owner,
**not vendor-verified**. This was a compatibility experiment.

**Topology:** a separate six-node EVE topology. The rc10 NAC baseline was
untouched.

**Evidence:** ASTRA, 2026-11-08, sanitized package held locally by ASTRA.
It is not yet published to `astra/lab-evidence`, so not verified here.
Publish it with a manifest before this record is cited as verified.

| Requirement | Status | Observation |
|---|---|---|
| per-session role/ACL | **unsupported on this image** | <ul><li>a local quarantine role/policy displayed "Applied", but no port-access session formed</li><li>the HPE 10.07.0004 simulator release notes state classifier policy is **configurable but nonfunctional** ([5200-8082](https://arubanetworking.hpe.com/techdocs/AOS-CX/10.07/OVA/5200-8082.pdf))</li></ul> |
| `deny_routed`, `allow` via a **static port ACL** | not counted | a static ingress port ACL changed corp to timeout while remediation stayed HTTP 200, and removing it restored both. It is **port-wide**, not per-session, so it is not evidence for `role_acl` |
| MAC authentication (MAB) | unverified (inconclusive) | the MAC-auth trial produced **zero** RADIUS requests in a bounded capture; corp stayed reachable |
| `deny_same_subnet_peers`, `cut_established_flows`, `ipv6`, phone+PC port | unverified | not reachable on this image |
| NAC-integrated (RADIUS role + CoA) | **blocked** | no port-access session formed, so there was no RADIUS or CoA path to test |

**Next valid gate:** physical AOS-CX hardware, with same-subnet peer probes,
continuous traffic probes and the ADR-0009 requirement set. A
vendor-provided simulator image could count only if its own release notes
do not list classifier policy as nonfunctional, and even then it would
need the same traffic proof.

## Aruba AOS-CX, strategy `vlan`

Not tested.
