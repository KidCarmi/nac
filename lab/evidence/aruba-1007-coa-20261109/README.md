# Aruba CX 10.07 direct role CoA — 2026-11-09

**Result: PASS for the tested IPv4 per-session role transition without readdressing.** On the isolated EVE topology, a valid EAP-TLS endpoint was already authenticated on port `1/1/1` in local role `corp`. A session-targeted RADIUS CoA carrying `Aruba-User-Role=quarantine` received CoA-ACK; the switch reported role `quarantine`. Fresh endpoint TCP to corp and a same-subnet peer timed out, while remediation continued to return HTTP 200. A second CoA carrying `Aruba-User-Role=corp` received CoA-ACK and restored HTTP 200 to all three. The endpoint retained `10.70.10.50/24` and VLAN 10. No operator reauthentication, endpoint reconnect, port bounce, address renewal or fixed FreeRADIUS policy edit was performed between these two CoAs.

This is a **reference RADIUS platform test without KidCarmi/nac**. It does not prove a NAC rule or NAC console action can drive the role. The earlier [EAP-TLS and fixed-policy reference run](../aruba-1007-reference-20261109/README.md) established the authenticated session and role policy. The rc10 NAC/vEOS baseline remains separate and untouched.

## Pinned environment and scope

| Item | Value |
| --- | --- |
| EVE | VM 102, EVE 6.2.0-4, isolated [topology](../aruba-1007-reference-20261109/Aruba-Role-ACL-1007.unl) SHA-256 `004c7fbf948fa3e9fe06367c94a889c88a73afb56c0b65719006e10f3970ae94` |
| Switch | ArubaOS-CX Virtual.10.07.0004, build SHA `64b88cbfc38a48a03d7b8b6229137e37f59c9310`, image SHA-256 `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25` (image excluded; third-party mirror provenance unverified) |
| Reference RADIUS | FreeRADIUS 3.0.26, fixed EAP-TLS identity `wired-endpoint-01`; disposable CA and shared secret excluded |
| Endpoint | Linux wired supplicant, one `ens3` data NIC, static `10.70.10.50/24`, no management NIC |
| CoA | UDP/3799 from `10.70.99.2` to switch `10.70.99.1`, management VRF, same disposable secret as reference RADIUS |
| Access policy | Local `quarantine` role retains VLAN 10; `q-policy` permits remediation `10.70.20.10` and drops other tested IPv4 ingress; `corp` role has no associated policy |

The [HPE 10.07 Security Guide](https://arubanetworking.hpe.com/techdocs/AOS-CX/10.07/PDF/5200-7884.pdf) documents `radius dyn-authorization enable`, a trusted CoA client, and session matching by User-Name, NAS identity, and port plus calling station. The run configured that client on the isolated `mgmt` VRF and sent those attributes plus Aruba VSA 14823/1.

## Results and synchronized evidence

| Gate | Expected | Observed | Result |
| --- | --- | --- | --- |
| Service health | Target services independently healthy | Corp node reached corp and peer HTTP 200; remediation node reached remediation HTTP 200. | **PASS** |
| Pre-CoA corp | Endpoint reaches corp and peer | Fresh endpoint probes returned HTTP 200 to corp, peer and remediation before the CoA. | **PASS** |
| CoA quarantine | ACK plus actual access restriction, same address | CoA Request ID 112 at `16:55:13.424052` UTC; ACK at `16:55:13.453828`; switch role `quarantine`. Last captured corp HTTP 200 at `16:55:13.239140`; the first post-ACK corp SYN at `16:55:13.736556` got no SYN-ACK and the endpoint probe timed out. Peer also timed out. Remediation returned HTTP 200 in every one of 72 probe samples. | **PASS** |
| CoA restore | ACK plus actual corp reachability, same address | CoA Request ID 6 at `16:57:12.229005` UTC; ACK at `16:57:12.260486`; switch role `corp`. First captured corp HTTP 200 after ACK at `16:57:12.789549`, 0.529 s after ACK; endpoint corp/peer/remediation HTTP 200. Remediation returned HTTP 200 in every one of 83 probe samples. | **PASS** |
| Reauthentication | Determine whether CoA caused EAP-TLS exchange | Both access-tap captures recorded zero EAPOL before reaching their 1,200-packet caps; both 58 s control-tap captures recorded zero UDP/1812 packets. The switch changed the active role directly in the observed interval. | **PASS**, observed window only |
| NAC integration, broader containment | NAC-driven CoA, peer-initiated traffic, established flows, IPv6, DHCP, shared port | Not run. | **BLOCKED / unverified** |

The endpoint probe clocks and EVE tap clock differ, so the **subsecond bounds above use packet timestamps from the same EVE host**, not subtraction across guest clocks. The quarantine capture shows denial on the first sampled corp SYN **0.283 s after ACK**; the actual enforcement instant lies between observations. The restore capture shows HTTP 200 by **0.529 s after ACK**. These are observations from this run, not a latency guarantee. The packet capture contains fresh TCP SYNs and responses (or their absence), so a displayed role or ACK alone does not decide the result.

## Evidence and rerun

- [Quarantine probes](aruba-coa-20261109-quarantine/endpoint-probes.jsonl), [control tap](aruba-coa-20261109-quarantine/switch-control-tap.pcap), [endpoint tap](aruba-coa-20261109-quarantine/endpoint-tap.pcap)
- [Restore probes](aruba-coa-20261109-restore/endpoint-probes.jsonl), [control tap](aruba-coa-20261109-restore/switch-control-tap.pcap), [endpoint tap](aruba-coa-20261109-restore/endpoint-tap.pcap)
- Each phase also has the opposite side's tap and bounded tcpdump logs. The [archive](aruba-coa-20261109.tar.gz) is SHA-256 `1146a16eb6009c7128aaa20f41699e751f693c1871d8acfc4e021b59f9e3a4ab`.
- [Secret-free helpers](scripts/) and [rerun instructions](rerun.md); [manifest](manifest.json) pins every file.

The CoA pcaps include internal addresses and the public EAP-TLS identity, but no private keys or RADIUS shared secret. Vendor images and test keys are excluded.
