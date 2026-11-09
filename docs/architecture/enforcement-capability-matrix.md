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

**Status: platform capability partly demonstrated, with a reference RADIUS
server.** That covers per-session IPv4 role enforcement and a direct role
change by RADIUS CoA. **Not NAC-integrated, so not selectable.**

| | |
|---|---|
| Build SHA | `64b88cbfc38a48a03d7b8b6229137e37f59c9310` |
| Image SHA-256 | `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25` |
| Provenance | Third-party mirror supplied by the owner, **not vendor-verified** |
| Topology | Separate EVE topology; the rc10 NAC baseline was untouched |
| Reference diagnostic (2026-11-09) | [`a8c8207`](https://github.com/KidCarmi/nac/tree/a8c820741e475556095452869f682c0c56a89d2d/lab/evidence/aruba-1007-reference-20261109), 22 manifest entries, verified here |
| Direct RADIUS CoA role change (2026-11-09) | [`d3cf745`](https://github.com/KidCarmi/nac/tree/d3cf745afe9d808bafc3f8ff10cb78f9c77ad7fb/lab/evidence/aruba-1007-coa-20261109), 24 manifest entries, verified here |
| Peer-initiated same-subnet check (2026-11-09) | [`3ff53a9`](https://github.com/KidCarmi/nac/tree/3ff53a9d7846998cdfe9827240be531bd84c417e/lab/evidence/aruba-1007-peer-20261109), 8 manifest entries, verified here |
| Original experiment, corrected | [`a8c8207`](https://github.com/KidCarmi/nac/tree/a8c820741e475556095452869f682c0c56a89d2d/lab/evidence/aruba-1007-compat-20261108), 10 entries, verified here |

**Root cause of the earlier failure:** configuration, not the image.
- The per-port 802.1X authenticator was enabled, but the global
  `aaa authentication port-access dot1x authenticator` → `enable` was
  absent.
- EAPOL-Start reached the endpoint-side and switch-side taps, and the
  switch sent no EAP response and no RADIUS request.
- With the global authenticator enabled and an interface-specific wired
  supplicant (`wpa_supplicant -i ens3 -D wired`), EAP-TLS completed:
  - switch `dot1x Success`, endpoint `Authorized`
  - switch-originated Access-Request, Challenge and Accept
- The earlier MAB run had no interface-specific supplicant and only a
  server-side capture, so it established nothing about the simulator.

**What was tested:** a reference FreeRADIUS server with **no nacd** returned
`Aruba-User-Role` (VSA 14823/1). The setup was one endpoint on one port,
with the same certificate, VLAN and static IPv4 address `10.70.10.50`
throughout. Quarantine role: local policy `q-policy`, which permits the
remediation server `10.70.20.10/32` and drops everything else. Each role
change was done by editing the fixed RADIUS reply, then running
switch-local `port-access reauthenticate`.

| Requirement | Status | Evidence / scope |
|---|---|---|
| per-session (802.1X client role) | **demonstrated** | role `quarantine`, then `corp`, applied to the authenticated session; RADIUS VSA, switch role and endpoint probes correlate |
| keeps address (no readdressing) | **demonstrated** | same IP and VLAN throughout |
| `deny_routed` (corp from quarantine), IPv4 | **demonstrated**, fresh TCP | corp TCP timed out (3.0 s) under `quarantine`; HTTP 200 again under `corp` |
| `allow` (remediation), IPv4 | **demonstrated** | remediation HTTP 200 under both roles |
| `deny_same_subnet_peers`, IPv4, **TCP sessions** | **demonstrated, both directions (sessions cannot complete)** | <ul><li>endpoint-initiated: TCP to the peer `10.70.10.11` timed out</li><li>peer-initiated ([`3ff53a9`](https://github.com/KidCarmi/nac/tree/3ff53a9d7846998cdfe9827240be531bd84c417e/lab/evidence/aruba-1007-peer-20261109)): under `quarantine` the peer's fresh TCP to an endpoint service timed out; under `corp`, before and after, it returned HTTP 200</li></ul> |
| `deny_same_subnet_peers`, IPv4, **packet isolation** (inbound peer packets must not reach the endpoint) | **not met by this role/policy** | the peer's SYN **reached** the endpoint (endpoint tap 17:43:32.360707 UTC), and the endpoint answered SYN-ACK at .360965. The SYN-ACK never reached the peer tap, because the ingress policy at the endpoint's port drops return traffic. Inbound UDP payloads and IPv6 were not tested. Does not meet a requirement for full bidirectional L2/packet containment |
| `cut_established_flows` | unverified | fresh flows only |
| `ipv6` | unverified | IPv4 only |
| role change by **RADIUS CoA** (dynamic authorization), keeping IP and VLAN | **demonstrated, IPv4** ([`d3cf745`](https://github.com/KidCarmi/nac/tree/d3cf745afe9d808bafc3f8ff10cb78f9c77ad7fb/lab/evidence/aruba-1007-coa-20261109)) | <ul><li>`radclient` sent a session-targeted CoA to UDP/3799 with User-Name, NAS-IP-Address, NAS-Port-Id, Calling-Station-Id and `Aruba-User-Role`</li><li>corp → quarantine: CoA-ACK; switch role `quarantine`; the first sampled corp SYN after the ACK got no SYN-ACK at **+0.283 s**; the peer timed out</li><li>quarantine → corp: CoA-ACK; corp HTTP 200 by **+0.529 s**</li><li>address `10.70.10.50/24` and VLAN 10 unchanged; no operator re-authentication, endpoint reconnect, port bounce or DHCP renewal</li><li>the times are EVE-host packet bounds for this run, not latency guarantees</li></ul> |
| no re-authentication on CoA | **observed, captured window only** | zero EAPOL on the access taps before their 1,200-packet caps; zero UDP/1812 in the 58 s control-tap windows |
| remediation continuity during the change | **demonstrated, this run** | remediation HTTP 200 in all 72 (quarantine) and 83 (restore) probe samples |
| DHCP behavior | unverified | static address |
| phone+PC port | unverified | one endpoint |
| NAC-integrated (nacd rules + CoA) | **not started** | the reference RADIUS server is a platform-capability harness, not a KidCarmi/nac pass |
| MAB | unverified | out of scope for this diagnostic and the adapter |
| documented limitation | — | the 10.07.0004 simulator release notes say *classifier* policy is configurable but nonfunctional ([5200-8082](https://arubanetworking.hpe.com/techdocs/AOS-CX/10.07/OVA/5200-8082.pdf)). The class-based port-access role policy above **did** enforce in this run, so that note does not describe this mechanism's behavior here |
| environment note | — | in this EVE setup, the endpoint and service virtio NICs needed `ethtool -K ens3 tx off tso off gso off` |

**Next steps toward selectability:**
1. ~~A role change by RADIUS CoA, keeping the IP and VLAN.~~ Done:
   demonstrated in `d3cf745` (IPv4, reference RADIUS).
2. Established-flow cutting, IPv6, and peer-initiated same-subnet traffic.
3. The NAC-integrated pass: a nacd decision returns the role, and a nacd
   CoA changes it.
4. Shared phone+PC port.
5. A vendor-verified image or physical hardware, for provenance.

## Aruba AOS-CX, strategy `vlan`

Not tested.
