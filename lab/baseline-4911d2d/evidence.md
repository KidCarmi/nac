# Wired NAC lab evidence, 2026-11-06 UTC

The TCP results below came from `nac-endpoint` `ens3`, the sole endpoint NIC. The two service nodes separately returned HTTP 200 on TCP 8080 from their own bridges before isolation tests. `ip_forward` on EVE was `0`; the EVE guest had no address on the access or corp bridges.

| Run | Endpoint IP and TCP probes | Switch `show dot1x hosts` | RADIUS and NAC |
| --- | --- | --- | --- |
| Fresh, no supplicant | `10.70.10.50/24`; corp `nc -vz -w3 10.70.10.10 8080` timed out | No authorized session | No accept |
| Fixed FreeRADIUS corp | `10.70.10.50/24`; corp connected; on-link quarantine probe timed out | Et1 EAPOL SUCCESS VLAN 10 | Switch `10.70.99.1` Access-Accept, VLAN 10; endpoint EAP-TLS SUCCESS |
| Fixed FreeRADIUS quarantine, fresh session | `10.70.20.50/24`; quarantine connected; on-link corp probe timed out | Et1 EAPOL SUCCESS VLAN 20 | Only fixed RADIUS VLAN result changed to 20; Access-Accept VLAN 20 |
| Invalid certificate, fresh session | `10.70.10.50/24`; corp timed out | Et1 EAPOL FAILED, fallback NONE, no VLAN | EAP failure; Access-Reject |
| NAC rule corp | `10.70.10.50/24`; corp connected; on-link quarantine timed out | Et1 EAPOL SUCCESS VLAN 10 | Rule `97b99d3b-9251-481e-a957-156f42688a21`; adapter `segment=corp`, VLAN 10 at 08:05:12-08:05:33; Access-Accept VLAN 10 |
| NAC rule quarantine | `10.70.20.50/24`; quarantine connected; on-link corp timed out | Et1 EAPOL SUCCESS VLAN 20 | Same rule ID and same certificate; only rule `segment` changed; adapter VLAN 20 at 08:06:46-08:06:55; Access-Accept VLAN 20 |
| New login while NAC down | Both corp and quarantine timed out | Et1 EAPOL FAILED, no VLAN | Adapter `NAC decision service unavailable` at 08:08:07; RADIUS rejected |
| CoA on active quarantine session | After CoA, `10.70.10.50/24`; corp connected and on-link quarantine timed out | Same active Et1 session moved VLAN 20 to 10 | Initial incomplete CoA got NAK `Missing-Attribute`; corrected CoA got ACK at 08:12:12; no manual reauthentication |
| Simulated printer MAB | No MAB access run | `dot1x mac based authentication` returned `% Unavailable command (not supported on this hardware platform)` | BLOCKED by this vEOS-lab platform |
| Invalid EAP-TLS with MAB fallback attempted | Corp timed out | Et1 EAPOL FAILED, fallback NONE | MAB command was unavailable, so fallback mechanism itself was BLOCKED; fail-closed behavior observed |

The main rule update used `PUT /api/v1/policies/97b99d3b-9251-481e-a957-156f42688a21` with unchanged name, description, priority, enabled state, conditions and action. The update response showed `segment: corp -> quarantine`. The certificate, endpoint MAC, Et1 switch configuration and adapter files were unchanged for the two NAC runs. The session was cleared and the same supplicant reauthenticated between runs; endpoint IP was refreshed for its VLAN.

The baseline attempt to change VLAN 10 to 20 with `dot1x re-authenticate Ethernet1` on the still active session produced `FAILED-DYN-VLAN` and no TCP pass. Clearing the session and reauthenticating succeeded. The separate CoA test later moved an active session successfully.

Evidence files: [endpoint EAPOL](endpoint-eapol.pcap), [switch-originated RADIUS and CoA](radius.pcap), [NAC adapter decisions](nac-adapter.jsonl), [valid supplicant log](wpa-nac.log), [invalid supplicant log](wpa-invalid-fallback.log). The packet captures include public certificate and session identifiers plus standard encrypted RADIUS key attributes; they contain no private key files or shared-secret values.
