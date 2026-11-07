# Fresh-instance rc10 console qualification — 2026-11-07

**Candidate:** KidCarmi/nac `b39649f138e126ed6689a53b590855061042e341`; installed `nacd` SHA-256 `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e`. FreeRADIUS 3.2.5; Arista vEOS-lab 4.29.2F. The fresh control guest had an empty NAC database and policy version 1 (default deny). It is separate from the preserved rc9 and earlier rc10 overlays. The one-port Linux endpoint used systemd-networkd, one data NIC, EAP-TLS, and 120-second DHCP leases. No management NIC was present. The Windows DHCP result is a separate run and is not represented by these timings.

## Answers

| Observation | Result | Evidence |
| --- | --- | --- |
| Install candidate from documented archive | **PASS** | `install.txt`: exit 0, 45.8 s, services active, `/readyz` ready, binary hash. `initial-state.txt` shows zero switches and sessions. |
| Switch onboarding, trusted eAPI probe, identity binding, draft preview and publication in console | **PASS** | `nac-evidence.json`: switch, endpoint, issuer+serial binding, policy v2 and audit sequence. `ui-observations.md` records UI-only preview and probe states. |
| Corp and quarantine service health | **PASS** | `service-health-before.txt` and `service-health-after.txt`; both return HTTP 200 from their own isolated segment; EVE IP forwarding 0. |
| Fresh session before authorization | **PASS** | `endpoint-probe.jsonl` starts with no IPv4 and both fresh TCP probes blocked. |
| Unbound valid EAP-TLS identity | **PASS** | FreeRADIUS validated the client certificate; NAC policy v1 rejected it for no binding/no matched rule. `nac-evidence.json`, `radius-timeline.txt`, `eapol-timeline.txt`. |
| Preview prediction versus later authorization | **PASS** | Console preview: one recorded case, active `deny`, draft `allow corp`, zero insufficient/stale inputs. After publication and new authentication, policy v2 matched `bound-corp`, returned VLAN 10; `nac-evidence.json` and RADIUS capture agree. |
| Corp admission with ordinary DHCP | **PASS** | EAPOL SUCCESS, Access-Accept VLAN 10, eAPI SUCCESS VLAN 10, DHCP corp ACK, endpoint 10.70.10.80/24 with corp route/DNS; fresh corp TCP open, quarantine blocked. First corp TCP 13:46:40.844 UTC, about 42.8 s after 13:45:58 authorization (rounded authorization timestamp). |
| Console Quarantine CoA and automatic addressing | **PASS, with 69.687 s interruption to new service** | Action 13:47:32.413 UTC, CoA-ACK, switch-initiated EAP-TLS and VLAN 20, old corp TCP denied +1.734 s, new quarantine TCP open +69.687 s at 10.70.20.80. `analysis-quarantine.txt`, `nac-evidence.json`, packet timelines. No manual renew/reconnect. |
| Console Restore CoA and automatic addressing | **PASS, with 84.846 s interruption to new service** | Action 13:49:17.347 UTC, CoA-ACK, switch-initiated EAP-TLS and VLAN 10, old quarantine TCP denied +0.906 s, new corp TCP open +84.846 s at 10.70.10.80. `analysis-restore.txt`, packet timelines. No manual renew/reconnect. |
| Interactive Account password change | **PASS** | User entered/submitted the new disposable password in the Account page at 13:50:44 UTC; DB audit and password-change timestamp confirm. Pre-change token `/auth/me` and initial-password login both returned HTTP 401 after rotation (`password-revocation.txt`). The current browser session remained usable. |

The product correctly separates requested access, CoA state, switch-observed authorization, and endpoint reachability. The UI explicitly says endpoint reachability is **not observed**. The endpoint's fresh TCP probes determine the access result.

## DHCP behavior and limits

The 120-second DHCP servers did not send explicit T1/T2 options. The endpoint sent a unicast renewal around T1 and a broadcast rebind around T2; the new segment's BusyBox `udhcpd` did not reply to a request carrying the old segment address. It obtained the new subnet only after lease expiry, by Discover/Offer/Request/ACK. In this run, corp→quarantine took 69.687 s from CoA request to fresh quarantine TCP, and quarantine→corp took 84.846 s. The old service was denied within 1.734 s and 0.906 s respectively. All timings belong to this clean UI run. EVE packet-capture time is offset from endpoint time by roughly one second; delay calculations use NAC action and endpoint probe clocks, while packet timelines establish event order.

The console confirmation's phrases “about 20 s” and “at its next DHCP renewal” are misleading for this Linux/BusyBox combination. OPUS has accepted the wording issue for a later candidate. This run does not show a NAC enforcement defect: CoA, policy, switch VLAN, and access changes agree. Windows Server 2025 results on 120-second and one-hour leases are separately documented in the Windows package.

## Lab corrections and operator interventions

The fresh Ubuntu overlay initially lacked SSH host keys and a usable lab root login; ASTRA repaired the disposable guest OS before product installation. The initially staged disposable RADIUS CA differed from the endpoint's trusted CA, causing TLS `unknown CA` failures between 13:39 and 13:43 UTC. ASTRA verified the original EVE PKI chain, staged the matching CA/server certificate/key root-only, validated FreeRADIUS configuration and restarted FreeRADIUS. The subsequent valid certificate was accepted by EAP-TLS and rejected by NAC only for lack of binding. These are **lab setup corrections**, preserved in `endpoint-ca-diagnostic.txt`, `control-ca-diagnostic.txt`, and packet/event history; they are not counted as a clean first-try authentication or product defect.

The operator still had to apply the console-generated EOS RADIUS target on the switch CLI and save startup config, install a trusted eAPI CA for `SSL_CERT_FILE`, and use the endpoint supplicant to start a new login after policy publication. The user handled the browser's disposable self-signed HTTPS warning and entered/submitted the new Account credential, as browser-use policy requires. No operator action touched the endpoint between each CoA request and automatic DHCP recovery.

## Evidence interpretation

`nac-evidence.json` is a read-only allowlist from the fresh NAC database: 1 switch, 1 device, 1 issuer+serial binding, 2 policy versions, 7 decisions, 3 sessions, 2 observed CoA requests, 30 auth events and 13 audit events. It excludes RADIUS secrets, eAPI passwords, password hashes, bearer tokens and key material. `radius-timeline.txt` includes the Access-Accept VLAN attributes and the exact CoA request/ACK plus subsequent Access-Accept for each transition. `eapol-timeline.txt` and `dhcp-timeline.txt` show switch-initiated authentication and the lease progression. `endpoint-probe.jsonl` has 3,909 bounded rows; its `network_state` events show no IPv4 → 10.70.10.80 → 10.70.20.80 → 10.70.10.80, with route and DNS changes and continuous fresh TCP results.

The package manifest hashes every file; the credential scanner compared it with eight lab-only credential values and found zero plaintext matches (`credential-scan.txt`).
