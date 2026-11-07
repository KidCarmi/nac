# rc10 targeted timing: saved switch config and same-VLAN CoA

Candidate: `b39649f138e126ed6689a53b590855061042e341`; running `nacd` SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`; FreeRADIUS 3.2.5; vEOS-lab 4.29.2F; Windows Server 2025 Standard Evaluation Desktop Experience build 26100. All times UTC on 2026-11-07. The endpoint had one DHCP data NIC, EAP-TLS certificate `host/wired-endpoint-01`, address `10.70.10.80/24` for same-VLAN tests, and no management NIC. Both isolated TCP services returned HTTP 200 from their own segments; EVE forwarding was 0. EVE had 11 GiB free before the run. Each of four packet captures was bounded to two 8 MiB files; the Windows probe was bounded to 8 MiB and 20 minutes. Fresh TCP probe resolution was approximately one second. Physical Arista confirmation remains unavailable.

## Saved configuration reboot: PASS

Before the switch reboot, the running EOS configuration pointed at rc10 RADIUS `10.70.99.4` and trusted `nac-eapi` TLS, while startup still pointed at the old `10.70.99.2` and default TLS. `write memory` made running and startup agree on rc10 RADIUS, TLS and dot1x accounting. Only isolated EVE switch node 31 was stopped and restarted; no Proxmox bridge/host setting or unrelated VM was changed. After boot, running and startup still matched, eAPI observed `SUCCESS VLAN 10`, and Windows freshly authenticated into corp. No switch configuration was reapplied. See `switch-save.txt`, `switch-reboot.txt`, `switch-after-reboot.txt`, `session-after-reboot.txt`, and bounded capture `persistence/radius.pcap0`.

The disposable rc10 Account password had been changed during the previous qualification. To use the documented product API in this new run, I used the documented **local password recovery** on the isolated control guest; that revoked prior lab tokens. This operator intervention is recorded in `operator-recovery.txt`. It did not touch the switch or endpoint.

| Product action | Old-segment first denial | New-segment first success | Addressing and authorization |
|---|---:|---:|---|
| Quarantine CoA 10:55:36.224 | corp +1.871 s | quarantine +29.961 s | CoA-ACK 200; Access-Accept VLAN 20; Windows DHCP `10.70.20.80/24` at 10:56:05; eAPI observed VLAN 20. |
| Restore CoA 10:58:19.737 | quarantine +1.938 s | corp +57.180 s | CoA-ACK 200; Access-Accept VLAN 10; Windows DHCP `10.70.10.80/24` at 10:59:16; eAPI observed VLAN 10. |

The endpoint performed no manual renewal or reconnect. `probe-summary.txt`, `windows-probe-persistence.jsonl`, `radius-timeline.txt`, `eapol-timeline.txt`, and `persistence/*.pcap0` preserve the timestamps. Application access is established by fresh TCP, separately from the switch observation.

## Same-VLAN comparison: reproducible virtual-platform interruption

Windows stayed on the same certificate, port, target, VLAN 10, and DHCP address. The idle control had **0 failed samples in 41**. Each product `Re-authenticate` sent CoA VLAN 10, received ACK 200, and returned Access-Accept VLAN 10. Three switch-local `dot1x re-authenticate Ethernet 1` commands also returned Access-Accept VLAN 10 and EAP Success; none pushed VLAN attributes as an operator action. No endpoint renewal/reconnect/port bounce was used. `samevlan-summary.jsonl` has exact samples; `samevlan/radius.pcap0` and `samevlan/endpoint.pcap0` show CoA, EAPOL and authorization.

| Trigger | Repetitions | First corp TCP failure after action | First corp TCP recovery after action | Failed samples |
|---|---:|---|---|---|
| NAC same-VLAN CoA | 3 | 1.897 / 2.146 / 2.116 s | 21.246 / 50.566 / 50.250 s | 13 / 30 / 32 |
| Switch-local reauthentication | 3 | none observed | uninterrupted | 0 / 0 / 0 (72 / 71 / 53 samples) |

The Windows address remained `10.70.10.80/24` in the product and switch-local trials. No carrier loss was observed in the Windows network-state samples, which run about every three seconds; a shorter link event cannot be excluded. Product trials 2 and 3 included EAP retries, so their longer gaps cannot be attributed solely to forwarding without the packet timeline.

## Packet-level control: VLAN-bearing CoA reproduces independently of NAC

From the same isolated control IP, `radclient` sent the same test identity/MAC/NAS/port attributes to vEOS. A CoA **without** tunnel attributes received CoA-NAK Error-Cause **404 Invalid-Request** and had 0 failed TCP samples. This build did not accept that as a reauthentication command. Three otherwise identical lab CoAs **with VLAN 10 tunnel attributes** received CoA-ACK 200 and reproduced the interruption. The captured tunnel attribute TLVs exactly match all three product CoAs: `64:40060000000d`, `65:410600000006`, `81:51043130` (tag 0 integers, untagged group id). See `coa-control-summary.jsonl`, `coa-control/radius.pcap0`, and `coa-control/endpoint.pcap0`.

| Lab-only packet | Response | First corp TCP failure | First corp TCP recovery |
|---|---|---|---|
| CoA without VLAN attributes | NAK 404 | none observed | uninterrupted |
| CoA with VLAN 10, repeat 1 | ACK 200 | +1.805 s | +21.343 s |
| CoA with VLAN 10, repeat 2 | ACK 200 | +1.758 s | +50.050 s |
| CoA with VLAN 10, repeat 3 | ACK 200 | +2.160 s | +57.659 s |

In repeat 3, Windows temporarily lost its address before reacquiring the same corp address; repeats 1 and 2 retained the address while fresh TCP failed. This shows both endpoint address behavior and switch forwarding can contribute to the interruption. The causal boundary established here is **vEOS-lab 4.29.2F handling of an ACKed, VLAN-bearing CoA**; the same gap occurs without a NAC product action. Omitting VLAN attributes is not a validated product fix because vEOS NAKed that packet. A physical Arista switch is needed to determine hardware behavior. OPUS received the first finding and this controlled correction; the pinned rc10 product was not changed.

## Outstanding timing scope

The PR-requested one-hour DHCP lease runs on both Linux and Windows remain unexecuted. This report contains the retained 120-second baseline and the new same-VLAN/reboot results only. It does not establish the one-hour lease operating envelope. An established application connection was not measured because the current simple HTTP target closes requests; fresh connections decide access in these runs.
