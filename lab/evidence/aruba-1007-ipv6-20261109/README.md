# Aruba CX 10.07 IPv6 endpoint-initiated probe — 2026-11-09

**Result: PASS for denial and restoration of one fresh, same-subnet IPv6 TCP service.** A disposable IPv6 corp service returned HTTP 200 from the wired endpoint in role `corp`. A direct role CoA to `quarantine` was ACKed; the same endpoint probe timed out. The endpoint-facing EVE tap saw its IPv6 SYN and retransmission, while the corp-facing tap saw no IPv6 TCP packet. After restoring `corp` by CoA, the IPv6 probe returned HTTP 200 again. IPv4 corp access was also denied and restored in the same sequence.

This is an isolated reference RADIUS platform test on ArubaOS-CX Virtual.10.07.0004 (build SHA `64b88cbfc38a48a03d7b8b6229137e37f59c9310`; image SHA-256 `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25`, image excluded and provenance unverified). The EVE topology, endpoint certificate, single data port, VLAN 10, and IPv4 address were the same as the [role](../aruba-1007-reference-20261109/README.md) and [direct CoA](../aruba-1007-coa-20261109/README.md) tests. KidCarmi/nac was not connected or changed.

## Bounded sequence

| Step | Observation | Status |
| --- | --- | --- |
| Service health | Corp guest bound `2001:db8:70:10::10:8083` and returned HTTP 200 locally. The endpoint used `2001:db8:70:10::50/64` on `ens3` and received HTTP 200 to the service while role `corp` was active. | **PASS** |
| Quarantine | Session-targeted CoA Request ID 163 at `17:53:50.561043` UTC, ACK at `17:53:50.602287`. Endpoint IPv6 HTTP probe returned `000` after 2 s connection timeout; IPv4 corp probe also returned `000`. | **PASS** for tested fresh flows |
| Switch dataplane | [Endpoint tap](aruba-ipv6-role-20261109/endpoint-tap.pcap) saw IPv6 SYN at `17:54:07.645184` and retransmission at `17:54:08.665553`; [corp tap](aruba-ipv6-role-20261109/corp-tap.pcap) had no captured IPv6 TCP packet. | **PASS** for endpoint-to-corp denial |
| Restore | CoA Request ID 211 at `17:54:33.122754`, ACK at `17:54:33.132168`; fresh endpoint IPv6 and IPv4 corp probes each returned HTTP 200. | **PASS** |

The existing local `quarantine` role policy contained an IPv4 remediation permit and IPv4 drop class; this run shows the role also blocked the tested IPv6 flow. It does **not** establish the exact internal policy match or prove that IPv6 remediation can be permitted. It does not test peer-initiated IPv6 traffic, routed IPv6 services, established IPv6 connections, DHCPv6, or NAC-driven authorization. Do not mark general IPv6 support or a complete access profile demonstrated from this one flow.

The test addresses were added temporarily to the endpoint and corp guest NICs, then removed. The disposable IPv6 HTTP service was stopped; the switch session was restored to `corp`. The [peer-initiated IPv4 test](../aruba-1007-peer-20261109/README.md) separately shows that this role does not prevent a peer-originated SYN from reaching the endpoint.

## Evidence and rerun

The [bounded capture archive](aruba-ipv6-role-20261109.tar.gz) SHA-256 is `16e4820d883dfe51b8a8d84c8eb25fa9a1c51279c702592abd9507cffc101c87`. It includes endpoint, corp and CoA taps plus capture logs. The [manifest](manifest.json) pins each file. [Secret-free capture helper](aruba_ipv6_capture_guest.py) and [rerun steps](rerun.md) are included. No private key, shared secret or vendor image is included.
