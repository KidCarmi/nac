# One-hour DHCP lease qualification — pinned rc10

Candidate `b39649f138e126ed6689a53b590855061042e341`; live nacd SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`; FreeRADIUS 3.2.5; vEOS-lab 4.29.2F. Windows endpoint: Server 2025 Standard Evaluation Desktop Experience, build 26100, one DHCP data NIC, authenticated EAP-TLS certificate `wired-endpoint-01`. EVE topology SHA-256 `8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935`; no host forwarding, both isolated TCP services independently returned HTTP 200. EVE had 11 GiB free before captures. Evidence is UTC, 2026-11-07.

Corp and quarantine BusyBox udhcpd scopes both advertise 3,600-second leases. The captured quarantine ACK at 11:24:41.842 UTC sent option 51=3600, but no explicit options 58/59. RFC 2131 defaults imply T1=1,800 s and T2=3,150 s; actual client renewal behavior is checked against the packet capture. These defaults are derived values, not server-sent options.

## Windows transition shortly after a fresh corp lease — PASS, API-driven

The Windows corp lease was obtained 11:22:59 UTC and expired 12:22:59 UTC. NAC Quarantine was requested 11:24:08.239 UTC, with 3,530.761 seconds remaining. Fresh corp TCP was first denied +1.652 s; fresh quarantine TCP succeeded +34.429 s from 10.70.20.80. Windows had no IPv4 address briefly at +30.431 s, then obtained its quarantine lease at 11:24:41 UTC; DNS and route became 10.70.20.10. NAC Restore was requested 11:25:14.260 UTC, with 3,566.740 seconds remaining on the quarantine lease. Fresh quarantine TCP was first denied +1.750 s; corp TCP succeeded +33.392 s from 10.70.10.80. Windows obtained the corp lease at 11:25:47 UTC, with corp DNS and route 10.70.10.10. No endpoint renewal, reconnect, or port bounce was performed. Product actions were through the documented API, so this row does not by itself close the all-console workflow gap.

The switch showed EAPOL SUCCESS in the assigned VLAN; the RADIUS capture contains CoA-ACK and subsequent Access-Accepts for both changes. The endpoint's fresh TCP samples decide reachability. See `plan.txt`, `windows-probe-snapshot.jsonl`, the final probe, and bounded pcaps.

## Near-T1 Windows transition — PASS, API-driven

After the automatic corp reacquisition at 11:25:47 UTC, the derived T1 was 11:55:47 UTC. Actual NAC Quarantine occurred at 11:55:45.418 UTC, 1,801.582 seconds before lease expiry. Fresh corp TCP was first denied +2.090 s; quarantine TCP first succeeded +35.593 s from 10.70.20.80. Quarantine DHCP ACK was at 11:56:20 UTC. NAC Restore occurred at 11:57:20.436 UTC with 3,539.564 seconds remaining on the quarantine lease. Fresh quarantine TCP was first denied +1.727 s; corp TCP first succeeded +32.632 s from 10.70.10.80. Corp DHCP ACK was at 11:57:52 UTC. The timer service triggered the documented NAC API; no endpoint renewal, reconnect, or port bounce was performed. Derived T1/T2 are not server-sent options.

The separate single established corp TCP connection began at 11:31:24.302 UTC. It received a reply 0.180 s after Quarantine and failed at 11:55:48.188 UTC, 2.770 s after the NAC request. It never reconnected. This result applies to the near-T1 Quarantine only; the fresh TCP probe supplied reachability for all transitions.

## Windows console quarantine and restore — PASS, UI-driven

Using the existing rc10 console, the operator clicked Quarantine on the active Windows session and confirmed the in-page dialog. NAC recorded the action at 12:00:37.521 UTC with 3,434.479 seconds remaining on the corp lease. Fresh corp TCP was first denied +2.411 s; quarantine TCP first succeeded +32.660 s from 10.70.20.80. Quarantine DHCP lease obtained 12:01:09 UTC, DNS and route 10.70.20.10. The operator then clicked Restore on the new active quarantine session and confirmed at 12:01:28.379 UTC, with 3,580.621 seconds remaining. Fresh quarantine TCP was first denied +1.559 s; corp TCP first succeeded +61.855 s from 10.70.10.80. Corp DHCP lease obtained 12:02:30 UTC, DNS and route 10.70.10.10. There was no endpoint intervention. The restore took longer because the packet capture shows Access-Accept for VLAN 10 only at 12:01:57.396 UTC, about 28 seconds after CoA-ACK. Do not treat the ACK as application recovery.

For all six actions, `coa-actions.txt` identifies the exact NAC request and decision, `radius-timeline.txt` shows CoA-ACK and the subsequent Access-Accept VLAN, `eapol-timeline.txt` shows EAPOL, and `windows-lease-analysis-final.txt` derives old-denial/new-reachability and lease/address/DNS/route state from `windows-probe-final.jsonl`. Bounded pcaps preserve packet-level evidence. The console correctly showed switch observation separately from endpoint reachability (`not_observed`). This UI run used the existing instance; it does not satisfy the separate clean-instance all-console workflow.

## Lab handoff

The endpoint probe and captures were fetched and stopped, and Windows was shut down from its own OS after its first ACPI powerdown request did not stop QEMU within 90 seconds. The Linux comparison then started on the same port. A separate fresh Ubuntu rc10 control overlay remains prepared for the all-console run.
