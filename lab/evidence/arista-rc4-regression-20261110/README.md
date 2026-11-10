# ASTRA: same-binary vEOS regression for pilot-aruba-rc4

**Can this vEOS build enforce access? PASS.** Authorized vEOS-lab 4.29.2F on Ethernet1 admitted the EAP-TLS endpoint to VLAN 10, rejected the invalid certificate, moved the same endpoint to VLAN 20 and back after NAC CoA, and changed actual TCP reachability. The independently healthy service checks and the [bounded packet/probe archive](arista-rc4-complete-20261110.tar.gz) support the result.

**Did changing only a NAC rule change reachability? PASS, after documented reauthentication and manual DHCP recovery.** The one draft rule changed from `corp` to `quarantine`, policy v13 was published, and NAC's Re-authenticate action caused CoA and a new Access-Accept for VLAN 20. Corp TCP stopped; quarantine TCP opened once the Linux guest was manually reconnected and took a `10.70.20.80/24` lease. Policy v2 restoration plus another NAC reauthentication returned VLAN 10 and corp TCP. The rule conditions, certificate, MAC, switch port, switch mapping, adapter and binary were unchanged.

**Automatic DHCP movement: FAIL for this Linux guest.** Its link remained up across VLAN CoA, and it kept the old lease with neither service reachable until a clearly recorded `networkctl down ens3; networkctl up ens3`. The manual recovery does not count as automatic. No Windows endpoint or physical switch was available, so neither is qualified here. This regression was run through the NAC API, not the Arista console UI; the separate [clean rc4 Aruba UI package](../aruba-rc4-clean-20261110/README.md) covers the operator UI on that platform. **The candidate is not release-cleared.**

## Pins and layout

| Item | Exact run value |
|---|---|
| Candidate | `pilot-aruba-rc4`, commit `0148bf88985c5da9086b54aafd002fbfbfe71b75` |
| Running nacd SHA-256 | `dd2b61371585d317bf07684733996c69e265dfdb9e762e354eeca4f7821b49d7`, independently checked after the second start |
| FreeRADIUS adapter aggregate SHA-256 | `3c45063efb451ff86dac23d695e9c57c307b86ef8054b790a759cbb63e80db32` |
| RADIUS / database / control OS | FreeRADIUS `3.2.5+dfsg-3~ubuntu24.04.3`, PostgreSQL 16.15, Ubuntu 24.04.5 LTS |
| Switch / EVE / hypervisor | vEOS-lab 4.29.2F, EVE-NG 6.2.0-4 / QEMU 8.2.1 on Proxmox VE 9.2.2 VM 102; 19 GiB EVE RAM |
| Topology | [NAC-Pilot-DHCP.unl](NAC-Pilot-DHCP.unl), SHA-256 `8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935`; isolated nodes 31–35 |
| Endpoint | One data NIC `ens3`, `00:50:00:00:02:00`, valid certificate CN `wired-endpoint-01`; no management NIC |
| Segments | Corp VLAN 10, `10.70.10.0/24`, service `10.70.10.10:8080`; quarantine VLAN 20, `10.70.20.0/24`, service `10.70.20.10:8080`; isolated control `10.70.99.0/24` |

The control guest was an **upgraded copy-on-write overlay** of the preserved rc9 node, using the rc4 bundle and existing disposable CA/database. This is a same-binary switch regression, not a clean Arista product-install result. The clean documented installation was separately tested in the Aruba package. The original Arista control disk SHA-256 `8d51b4fc45307ede0a0e39eb516df2815550fbba76a04e4b0a5e806f1d660c1d` was restored byte for byte; its rc4 COW is retained as `virtioa-rc4-20261110.qcow2`. Nodes 31–35 are stopped. The temporary EVE transfer namespace, veth and server were removed. No Proxmox bridge, host setting or unrelated VM was changed.

## Result matrix

| Check | Verdict | Direct observation |
|---|---|---|
| Independently healthy corp and quarantine services | PASS | HTTP 200 from temporary namespaces attached to each *own* isolated EVE bridge; EVE forwarding `0` |
| Fresh unauthenticated endpoint | PASS | No IPv4 lease, no vEOS dot1x host, both endpoint TCP probes blocked |
| Valid EAP-TLS, active policy v2 | PASS for enforcement | EAPOL and Access-Accept with Tunnel-Private-Group-ID 10; vEOS `SUCCESS VLAN 10`; endpoint corp TCP open, quarantine blocked after **manual** DHCP renewal |
| Invalid certificate, fresh session | PASS | FreeRADIUS Access-Reject, vEOS `FAILED`, corp TCP timeout even while the endpoint retained its prior corp address |
| Rule-only v13 corp → quarantine | PASS for enforcement | Preview predicted quarantine; CoA `0x62` ACKed; switch-initiated EAP-TLS and Access-Accept VLAN 20; vEOS `SUCCESS VLAN 20`; corp blocked, quarantine open after **manual** reconnect |
| Policy v2 restore | PASS for enforcement | CoA `0x89` ACKed; Access-Accept VLAN 10; quarantine blocked, corp open after **manual** reconnect |
| NAC API Quarantine override under v2 | PASS for enforcement | CoA `0x34` ACKed; new RADIUS VLAN 20; vEOS VLAN 20; corp blocked, quarantine open after **manual** reconnect. NAC decision cites operator override above matched corp rule |
| NAC API Restore | PASS for enforcement | CoA `0xc3` ACKed; new RADIUS VLAN 10; vEOS VLAN 10; quarantine blocked, corp open after **manual** reconnect |
| Automatic DHCP recovery across VLAN CoA | FAIL on this Linux guest | No link-state change or prompt subnet renewal; old lease became unusable. Four transitions required manual `networkctl down/up` |
| Restart all five Arista lab nodes | FAIL for unattended lab recovery; PASS after repair | Saved policy and pinned nacd resumed, EAP-TLS succeeded after switch target correction, but endpoint needed manual DHCP reconnect; corp guest disk was full until journal/cache cleanup |
| Arista console UI Quarantine/Restore | NOT RUN | API action pair was exercised; clean Aruba UI run is separate evidence |
| Established TCP connection survival | NOT RUN | This run measured fresh TCP connections only |
| Windows wired EAP-TLS / physical switch | BLOCKED | No Windows one-NIC EAP-TLS guest or authorized physical switch in this lab |

## Action timing on one capture clock

The times below are from EVE taps, UTC 2026-11-10. “Denied” means the first *fresh* old-segment SYN with no SYN-ACK; it bounds the observation, not the exact instant of switch enforcement. “New HTTP” followed a manual endpoint reconnect and must not be read as automatic recovery. Endpoint JSONL has 110 samples per action and the guest address at each sample. The [analyzer](arista_rc4_tcp_timeline_guest.py) reconstructs these values from PCAPs.

| Action | CoA request / ACK | Last old HTTP 200 | First unanswered old SYN | First new HTTP 200 | Manual reconnect start |
|---|---|---:|---:|---:|---|
| Rule v13 quarantine | `08:31:32.095767` / `.099170` ID `0x62` | 0.218 s before | 0.473 s after | 43.937 s after | `08:32:07` |
| Policy v2 restore | `08:33:42.713748` / `.716231` ID `0x89` | 0.061 s before | 0.631 s after | 43.289 s after | `08:34:18` |
| Operator Quarantine | `08:44:31.010589` / `.015704` ID `0x34` | 0.056 s after | 0.749 s after | 42.515 s after | `08:45:06` |
| Operator Restore | `08:46:11.318649` / `.321147` ID `0xc3` | 0.060 s after | 0.754 s after | 41.620 s after | `08:46:46` |

The two operator actions allowed one fresh connection immediately after the CoA request before the switch changed authorization. Therefore the first blocked endpoint-log sample at about 0.18 s is **not** an enforcement-time measurement. The packet clock is authoritative for the table.

## Lab corrections and failure attribution

1. The first valid attempt at 08:19 timed out because the saved vEOS config still targeted an older rc10 control address `10.70.99.4`. Its control tap had no RADIUS traffic. The helper changed only this isolated switch's live RADIUS target to the rc4 guest `10.70.99.2` without saving it; the repeat succeeded at 08:21:21. The original switch startup config remains unchanged.
2. On initial authentication, `systemd-networkd` did not start a lease when 802.1X authorization changed without carrier change. A manual `networkctl renew ens3` obtained corp `10.70.10.80/24`. After a later failed-certificate run, a manual down/up was required to regain corp addressing. During VLAN transitions, the stale lease persisted until each separately recorded manual down/up. Captures show DHCP NACK followed by Discover/Offer/Request/ACK on reconnection. These are Linux client/switch-link and lab addressing limits, not evidence of a NAC policy error.
3. After the five-node restart, the isolated corp service guest was 100% full; a scoped `journalctl --rotate --vacuum-size=16M` and `apt-get clean` recovered 220 MB. DHCP and HTTP then remained healthy. The control guest, FreeRADIUS and PostgreSQL were active and the running nacd hash still matched rc4.
4. The switch session remained connected under eAPI; NAC's endpoint reachability field remained `not_observed`. That wording correctly avoids claiming DHCP or TCP success from a CoA ACK or VLAN observation.

## Reproduce

Use the authorized vEOS image and [sanitized configuration](configs.sanitized.md). The [topology runner](arista_rc4_eve_guest.py) verifies the export SHA and node names before starting or stopping nodes 31–35. The [overlay helper](arista_rc4_overlay_guest.py) only runs while those nodes are stopped and checks the original disk hash. The preserved rc4 COW can be reactivated, then restored after testing. The [upgrade script](arista_rc4_overlay_upgrade.sh) describes how the separate lab-only overlay was constructed; for a clean product install, use OPUS's documented installer instead.

On EVE, with the helper files staged under `/root`, rerun the scoped steps:

```sh
python3 /root/arista_rc4_overlay_guest.py reactivate
ARISTA_RC4_ACTION=start python3 /root/arista_rc4_eve_guest.py
SWITCH_ACTION=rc4_control python3 /root/dhcp_switch_guest.py
DHCP_ENDPOINT_ACTION=valid_start python3 /root/dhcp_endpoint_guest.py
# Record automatic DHCP state first; if needed, label this intervention:
DHCP_ENDPOINT_ACTION=manual_reconnect python3 /root/dhcp_endpoint_guest.py
# On the control guest, use the local authenticated API helpers:
python3 /root/nac_policy_api_control.py read
python3 /root/nac_policy_api_control.py draft-quarantine
python3 /root/nac_policy_api_control.py preview
python3 /root/nac_policy_api_control.py publish
RC10_ACTION=reauth python3 /root/rc10_product_action_guest.py
DHCP_ENDPOINT_ACTION=probe python3 /root/dhcp_endpoint_guest.py
python3 /root/nac_policy_api_control.py restore-v2
RC10_ACTION=reauth python3 /root/rc10_product_action_guest.py
RC10_ACTION=quarantine python3 /root/rc10_product_action_guest.py
RC10_ACTION=restore python3 /root/rc10_product_action_guest.py
ARISTA_RC4_ACTION=stop python3 /root/arista_rc4_eve_guest.py
python3 /root/arista_rc4_overlay_guest.py restore
```

Run `ARISTA_RC4_PHASE=<phase> python3 /root/arista_rc4_capture_guest.py` before each transition, and the bounded endpoint [probe](arista_rc4_probe_endpoint.py) as documented in the helper comments. To inspect this package, `tar -xzf arista-rc4-complete-20261110.tar.gz`, then `python3 arista_rc4_analyze.py arista-rc4-20261110` and `tcpdump -tttt -nn -vvv -r arista-rc4-20261110/<phase>-control.pcap`. Every capture has a 120-second timeout and 10,000-packet cap. No keys, shared secrets, browser credentials or vendor disk images are delivered. [Manifest](sha256-manifest.json) verifies the files.
