# ASTRA rc10 short DHCP timer follow-up, 2026-11-07

**Answer:** Short T1/T2 on a one-hour lease worked for corp→quarantine, but **did not restore corp within 60 seconds** in the near-T1 reverse transition. This is a FAIL against the proposed recovery target, not evidence of a NAC decision defect. The first reverse DHCP rebind was captured on the endpoint port but not on either service bridge. The prior [one-hour default-timer comparison](https://github.com/KidCarmi/nac/tree/8db6fe899809c2a20db741d08f72e8e3711680e1/lab/evidence/dhcp-server-comparison-rc10-20261107) remains a distinct run.

## Fixed environment and treatment

NAC rc10 commit `b39649f138e126ed6689a53b590855061042e341`, installed nacd SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`, FreeRADIUS 3.2.5, vEOS-lab 4.29.2F, one-data-NIC Ubuntu 22.04.5 systemd-networkd endpoint, same EAP-TLS certificate and Ethernet1, policy v4 rule `bound-eap-corp`. Both original service guests ran Ubuntu dnsmasq-base 2.91-0ubuntu0.22.04.2, package SHA-256 `c999f2c92633d753edcdd67fb039f5866e9413500b2c764993cbb6bb26393309`, in authoritative mode. Compared with the earlier dnsmasq run, the only intended change was explicit DHCP options 58 and 59: T1=30s and T2=45s; lease remained 3,600s. The real [endpoint DHCP capture](primary/endpoint.pcap0) confirms all three options in both segments. The pre-run `networkctl down/up` at 17:00:40 UTC cleared the previous lease before authentication; **no endpoint action occurred during either primary CoA transition**.

The endpoint first authenticated by EAP-TLS into corp. It initially waited through DHCP retransmissions before receiving a corp offer/ACK at 17:01:55.322 UTC; this was setup latency, not a VLAN-change result. A corp renewal ACK at 17:02:24.360 carried lease/T1/T2 = 3600/28/43 seconds. Fresh corp TCP worked before Quarantine and quarantine TCP was blocked. Both services returned HTTP 200 independently from their own segments before and after the run, and EVE `ip_forward=0`.

## Primary results

| Test | Expected | Observed | Status |
| --- | --- | --- | --- |
| Fresh unauthenticated corp TCP | Blocked | Fresh corp TCP timed out before EAP-TLS | PASS |
| Corp admission | EAP-TLS, VLAN10, corp DHCP and TCP | Access-Accept VLAN10, corp ACK, corp TCP success | PASS |
| Quarantine before next T1 | Old corp TCP denied within 5s; quarantine TCP within proposed 60s without endpoint action | Product CoA 17:02:45.123847832 UTC; corp denial **+0.769s**; quarantine TCP success **+25.040s**. At 17:03:07.138, quarantine dnsmasq NAKed old-subnet rebind, then ACKed 10.70.20.52 at 17:03:10.163. | PASS in this direction |
| Restore 2.5s before quarantine T1 | Old quarantine TCP denied within 5s; corp TCP within proposed 60s without endpoint action | Product CoA 17:03:37.638858345 UTC; quarantine denial **+1.217s**. RADIUS CoA-ACK, Access-Accept VLAN10 and endpoint EAPOL Success followed. The endpoint broadcast a rebind at 17:03:55.024, but no corp DHCP reply or new address followed. Corp TCP had **no success through +165.191s** (17:06:22.830 UTC). | **FAIL** proposed 60s recovery target |
| Separate manual reconnect diagnostic | Determine whether service/topology can restore corp | At 17:08:38.107 UTC, `networkctl down/up ens3` caused corp dnsmasq to NAK the stale quarantine address and ACK 10.70.10.52 at 17:08:45.143; fresh corp TCP succeeded **+6.163s** after the manual action. | PASS diagnostic; does not count as automatic recovery |

The Quarantine action followed a renewal ACK by 20.764s; it was before the next T1, but is **not** a fresh-lease-at-second-zero measurement. Restore occurred 27.476s after the quarantine ACK, 2.524s before T1. The probe samples use fresh TCP connections with a sequential 0.8s timeout. Endpoint and EVE packet clocks can differ by about one second, so subsecond cross-machine ordering is not asserted. The first probe ended 17:04:08.995 and the next began 17:04:22.586, leaving a 13.6s probe gap. The continuous DHCP packet capture through 17:06:22 shows no corp ACK during that gap; the second probe still shows the quarantine address and no corp TCP. The automatic Restore result is explicitly censored at +165.191s; the later manual diagnostic is separate.

## Isolation of the failure

The [product CoA records](product-actions.txt) show both requests `observed` with one attempt and policy v4 decisions.

The [RADIUS timeline](primary/radius-timeline.txt) records CoA-Request VLAN10 at 17:03:38.463, CoA-ACK at 17:03:38.466 and Access-Accept VLAN10 at 17:03:38.725. The [EAPOL timeline](primary/eapol-timeline.txt) records Success at 17:03:38.729. The vEOS session later displayed Et1 EAPOL SUCCESS VLAN10. Yet the 17:03:55.024 broadcast DHCPREQUEST appears in the [endpoint capture](primary/endpoint.pcap0) and in neither [corp bridge capture](primary/corp-dhcp.pcap0) nor [quarantine bridge capture](primary/quarantine-dhcp.pcap0). This establishes that the request did not reach either DHCP guest through the observed EVE path at that instant; the trace does not pinpoint the internal vEOS forwarding cause. No later DHCP packet appeared in the primary capture. Both services remained healthy, and the [diagnostic capture](diagnostic/endpoint.pcap0) shows the same endpoint obtaining a corp lease after the manual reconnect. Thus the first T2 packet was lost in the access path, and short T2 alone was insufficient for this run.

## Disposition

The original BusyBox udhcpd services and unmodified 120-second scopes are active on both guests; dnsmasq and its short-timer config were stopped/restored. The endpoint supplicant and captures were stopped and the switch test session cleared. The two segment TCP services still return HTTP 200 and EVE forwarding remains off. A repeat with a full-length continuous probe and a second early-lease transition would strengthen consistency. Windows Server 2025, Windows 10/11 and physical-switch behavior with explicit short T1/T2 were not tested in this follow-up and remain BLOCKED here. A product or switch remedy cannot be selected as proven from this single timer configuration.

See [sanitized config](configs.sanitized.md), [rerun steps](rerun.md), [primary probe](primary/endpoint-probe-combined.jsonl), [diagnostic probe](diagnostic/endpoint-probe.jsonl), [Quarantine analysis](analysis-quarantine.txt), [Restore analysis](analysis-restore.txt), [diagnostic analysis](analysis-diagnostic.txt), [DHCP timeline](primary/dhcp-timeline.txt), [topology](NAC-Pilot-DHCP.unl), and [SHA-256 manifest](sha256-manifest.json).
