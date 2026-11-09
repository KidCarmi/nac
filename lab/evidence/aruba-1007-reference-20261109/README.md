# Aruba CX 10.07 reference authentication and role qualification — 2026-11-09

**Did this exact simulator enforce a per-session role? Yes, for the tested IPv4 flows.** With one EAP-TLS endpoint on port `1/1/1`, one unchanged address (`10.70.10.50/24`) and no NAC service, fixed FreeRADIUS authorization changed only the local role. `quarantine` blocked fresh TCP to corp and a healthy same-subnet peer while permitting remediation; restoring `corp` made both reachable. The endpoint probes, not the RADIUS Accept or switch display alone, decide this result. See [packet captures](#captures) and [test transcript](test-results.md).

**Did a KidCarmi/nac rule change Aruba reachability? Not tested in this reference run.** The proven rc10 NAC/vEOS baseline remains separate. Aruba NAC integration and CoA are the next gates; no new product candidate was qualified here.

## Correction to the 2026-11-08 experiment

The earlier [compatibility report](../aruba-1007-compat-20261108/README.md) classified the role path as blocked because no port-access session formed. That was a configuration gap, not evidence of unsupported per-session enforcement: the port had MAC authentication but the global 802.1X authenticator was disabled, and the endpoint had no interface-specific supplicant running. An initial reference attempt with port-level 802.1X enabled but global 802.1X still disabled produced EAPOL-Start on **both** endpoint and switch tap interfaces, no EAP response and no RADIUS packet. Enabling the global authenticator made the switch create an in-progress client; restarting the wired supplicant completed EAP-TLS and RADIUS. The exact first missing step was the switch's EAP response after EAPOL-Start.

HPE's [10.07.0004 simulator release notes](https://arubanetworking.hpe.com/techdocs/AOS-CX/10.07/OVA/5200-8082.pdf) list **classifier policy** as configurable but nonfunctional. HPE's [port-access policy guide](https://arubanetworking.hpe.com/techdocs/AOS-CX/10.17/HTML/security_8100-8360/Content/Chp_Port_acc/por-acc-pol-fl-10.htm) distinguishes the role-associated mechanism from classifier policy. The 10.17 guide is terminology context; this lab's packet probes demonstrate the tested 10.07 role-associated behavior directly. The earlier inference that the release-note caveat blocks all role enforcement was wrong.

## Pinned environment

| Component | Value |
| --- | --- |
| Proxmox/EVE | Existing EVE VM 102, `eve-ng 6.2.0-4`, kernel `6.7.5-eveng-6-ksm+`; 19 GiB guest RAM after owner increase |
| Topology | [Isolated EVE export](Aruba-Role-ACL-1007.unl), SHA-256 `004c7fbf948fa3e9fe06367c94a889c88a73afb56c0b65719006e10f3970ae94` |
| Switch | `ArubaOS-CX Virtual.10.07.0004`, Build SHA `64b88cbfc38a48a03d7b8b6229137e37f59c9310` |
| Image payload | SHA-256 `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25`; image excluded |
| Image provenance | Third-party mirror supplied by owner; HPE signature/entitlement not verified. This is an exact-image compatibility test, not a vendor-provenance qualification. |
| FreeRADIUS | 3.0.26 in isolated control guest, fixed role result and disposable CA; no nacd |
| Authenticated identity | `wired-endpoint-01`, verified by EAP-TLS client certificate against the disposable CA |
| Endpoint | Linux, `wpa_supplicant` wired driver on one `ens3` data NIC, static `10.70.10.50/24`; no management NIC |
| NAC | rc10 baseline `b39649f138e126ed6689a53b590855061042e341` untouched and **not connected** to this topology |

The new topology is isolated from the existing rc10 topology and Proxmox bridges. EVE access bridge `vnet0_61` connects endpoint tap `vunl0_62_0` to switch port `1/1/1` tap `vunl0_61_1`; control bridge `vnet0_64` connects switch management tap `vunl0_61_0` to FreeRADIUS tap `vunl0_65_0`. Both bridges had `group_fwd_mask=0xffff`; `ip_forward=0`. The switch port was operationally up, and the earlier port ACL experiment already proved ordinary data frames traversed this mapping.

## Result matrix

| Gate | Expected | Observed | Status |
| --- | --- | --- | --- |
| Reference authentication before global enable | EAPOL-Start leads to switch EAP response | Start visible on both access taps; no response, no RADIUS, no client. Global authenticator absent. | **FAIL** — lab config, corrected |
| Fixed FreeRADIUS EAP-TLS | Valid certificate authenticates and creates client | EAPOL exchange; switch-originated Access-Request/Challenge/Accept; `wired-endpoint-01` `dot1x Success`; endpoint `Authorized`. | **PASS** |
| Services healthy before filtering | Corp and remediation answer | Endpoint HTTP 200 to both; separate corp node reached same-subnet peer HTTP 200. | **PASS** |
| Per-session quarantine role | Same IP, corp and peer denied, remediation permitted | FreeRADIUS Access-Accept Aruba VSA 14823/1=`quarantine`; switch session role `quarantine` Applied; endpoint corp `000` timeout, peer `000` timeout, remediation `200`. | **PASS**, tested IPv4 flows |
| Restore corp role | Same IP, corp and peer reachable | Only fixed FreeRADIUS role changed to `corp`; explicit switch reauthentication; Access-Accept VSA=`corp`; switch session role `corp`; endpoint corp/peer/remediation each HTTP 200. | **PASS** |
| NAC-driven role and CoA | NAC rule or action changes actual reachability | Not run in this reference test. | **BLOCKED** pending adapter and CoA qualification |
| Other capabilities | Shared phone/PC, IPv6, established-flow cutting, DHCP | Not exercised here. | **BLOCKED** |

The switch accepted and applied `q-policy`, but the HTTP probes are the enforcement evidence. No endpoint IP, route, certificate, port, or switch role configuration changed between quarantine and restore. The fixed RADIUS role changed, followed by an **operator-triggered** `port-access reauthenticate interface 1/1/1`; this is not a CoA or automatic transition.

## Captures

Raw pcaps are in the four `aruba-reference-20261109-*` directories, one file per VM tap. Every capture used `timeout 40`, a 200-packet limit, and only EAPOL or RADIUS BPF filters. [Pre-global](aruba-reference-20261109-pre-global/), [baseline authentication](aruba-reference-20261109-baseline/), [quarantine](aruba-reference-20261109-quarantine/), [restore](aruba-reference-20261109-restore/). The original compressed archive is [pcaps.tar.gz](pcaps.tar.gz). [Manifest](manifest.json) pins every file.

- Pre-global: EAPOL-Start at `16:00:08.132` on both access taps, RADIUS captures empty.
- Baseline: EAPOL begins `16:01:49.664`; final Access-Accept `16:01:49.691` from `10.70.99.2:1812` to switch `10.70.99.1`.
- Quarantine: final Access-Accept `16:03:57.370` with Aruba VSA 14823/1 `quarantine`.
- Restore: final Access-Accept `16:06:23.481` with Aruba VSA 14823/1 `corp`.

The captures contain disposable test EAP-TLS exchanges and internal lab addresses. They contain no private key, shared secret, or vendor image. [Sanitized configuration](sanitized-config.md) and [rerun commands](rerun.md) are included. The earlier failed MAB attempt and static ACL controls remain in the 2026-11-08 package.
