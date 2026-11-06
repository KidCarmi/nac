# Pilot-rc1 operator actions (UTC, 2026-11-06)

Exact candidate: `fe7c9810848910aa4dc4536a5074d3ebaef72c7e`, nacd SHA-256 `062e948ac922ed703ed3743a75a2bc85241bf59597b963282dae2cef3e764204`. Timestamps combine UI, NAC events and packet captures. No credentials are included.

| Time | Action and observed result |
| --- | --- |
| 11:13:07.592 | Clean documented install completed. |
| 11:16:02 | UI registered switch `nac-veos-rc1` on `10.70.99.1`, Ethernet1, corp10/quarantine20. Saved EOS config from prior isolated candidate was reused, with the same disposable lab secret. |
| 11:16:16 | First fresh invalid-CA certificate rejected; UI showed no validated certificate; both TCP targets blocked. |
| 11:16:51 | Valid EAP-TLS before binding: v1 default deny, decision `ed982e78-d023-4f2c-a86e-380e936f06d3`; validated CN `wired-endpoint-01`, issuer `/CN=NAC Lab Disposable CA`, serial `2E1B47DC86A00B7F7250500B4A21F454B2EFD819`. |
| 11:17:24–11:17:57 | UI created device `03055a24-7ae7-462b-9f0e-9f2c6aef81f5` with MAC `00:50:00:00:02:00`; bound validated issuer+serial to it (`658e9567-67bc-45c3-9d60-f43cfd1aa06c`). |
| 11:17:57 | UI draft `bound-eap-corp`, priority100, bound identity and MAC match, allow corp; preview predicted deny→allow corp, 0 stale/insufficient. |
| 11:25:49 | User accepted browser-native publish confirmation; UI activated v2. Earlier apparent publish stall was a browser confirmation blocking automation; NAC received no request until confirmation. |
| 11:26:24 | Valid certificate: v2 decision `98a95814-32b7-4c94-90ec-eb5bb3812d89`, VLAN10 Access-Accept, Et1 SUCCESS; corp TCP OPEN, quarantine BLOCKED. |
| 11:27–11:28 | Operator stopped supplicant/cleared Et1 for fresh invalid test; Access-Reject and both TCP blocked. Valid client restarted and corp reopened. |
| 11:29:08 | UI changed only same rule's segment to quarantine, preview predicted corp→quarantine and published v3. Existing corp access remained until reauth. |
| 11:29:39 | UI Re-authenticate sent CoA id73/ACK200, switch reauthenticated, v3 decision `4d182348-d118-4fc6-a007-bc44163076a4`, VLAN20; quarantine TCP OPEN/corp BLOCKED after convergence. |
| 11:30:51 | UI restored v2 and Re-authenticated via CoA id122/ACK200, VLAN10; corp reopened. |
| 11:31:38 | Continuous fresh TCP probes and a held corp socket started. |
| 11:32:16 | User accepted native confirmation for UI Quarantine. Product CoA id255/ACK200, switch reauthenticated to VLAN20. Corp fresh TCP blocked by 11:32:18, quarantine opened at 11:32:37. Held old corp socket's subsequent HTTP GET timed out. No manual endpoint reconnect/reauth. |
| 11:33:11 | UI Restore CoA id78/ACK200, switch reauthenticated to VLAN10. Quarantine blocked at 11:33:13, corp opened at 11:33:33. No manual endpoint reconnect/reauth. |
| 11:34:35–11:36:43 | UI Quarantine CoA id115/ACK200. Operator replaced valid supplicant with invalid-CA client on same port/MAC: Access-Reject, both blocked. Valid client restarted, VLAN20/quarantine returned. UI Restore CoA id215/ACK200, corp returned. |
| 11:38 | Stopped only nacd; existing VLAN10 session and fresh corp TCP persisted. Operator explicitly stopped supplicant and cleared Et1 for a new login; during outage Access-Reject and both TCP blocked. Restored nacd; automatic supplicant retry (no further operator reconnect/reauth) got VLAN10 and corp access. |
| 11:43–11:45 | Restarted only candidate nodes11–15. Waited for EOS saved config and both services to become ready, then fresh unauth blocked and valid cert admitted corp. UI activated saved v3; user accepted Re-authenticate confirmation, CoA id22/ACK200, VLAN20, quarantine OPEN/corp BLOCKED. UI restored v2; operator explicitly ran endpoint `wpa_cli -i ens3 reauthenticate`; after transient EAPOL failure a later Access-Accept VLAN10 restored corp. This manual action is part of the restart test only. |
| 11:48:13–11:48:27 | Separate timed invalid-cert diagnosis: agent started fresh invalid cert, then saw unknown-CA reason in UI Overview at 11:48:27.659 (14.6 s from attempt start, 9.7 s after event). Valid client restarted; lab left in corp. |

During the dedicated product Quarantine/Restore CoA interval the endpoint and switch were not manually reconnected, cleared or reauthenticated. Browser-native confirmation dialogs required the user to click OK because the task browser could not dismiss them; this was an automation limitation, and each click is recorded above.
