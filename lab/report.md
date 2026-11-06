# Wired NAC qualification report

**Did this vEOS build enforce access? Yes.** On vEOS-lab EOS 4.29.2F, the same single-NIC endpoint and Ethernet1 port could not reach the protected corp TCP service before authentication or with an invalid certificate. With valid EAP-TLS, the endpoint reached corp in VLAN 10; with a fixed RADIUS quarantine result and a fresh session, it reached quarantine in VLAN 20 while corp timed out. The endpoint TCP probes, EAPOL/RADIUS captures and switch session observations are in [evidence.md](evidence.md), [endpoint-eapol.pcap](endpoint-eapol.pcap) and [radius.pcap](radius.pcap).

**Did changing only our NAC rule change actual reachability? Yes.** The same certificate, endpoint, switch port/configuration and FreeRADIUS adapter matched NAC rule `97b99d3b-9251-481e-a957-156f42688a21` in both runs. Changing only that rule's `segment` from `corp` to `quarantine`, then clearing the old session and reauthenticating, changed the RADIUS VLAN from 10 to 20, the switch session from VLAN 10 to 20, and the endpoint's TCP access from corp to quarantine. See the rule match records in [nac-adapter.jsonl](nac-adapter.jsonl), VLAN attributes in [radius.pcap](radius.pcap), and the probe transcript in [evidence.md](evidence.md).

## Lab and exact versions

| Component | Observed state |
| --- | --- |
| Proxmox | `cv-lenovo-pve-01`, `192.168.1.84`, PVE `9.2.2` (`pve-manager/9.2.2/b9984c6d90a4bd80`), kernel `7.0.2-6-pve`; 12 logical CPUs, 31 GiB host RAM; existing `vmbr0` and VMs 100/101 unchanged |
| EVE VM | New dedicated VM 102 `eve-ng-nac-lab`; 6 vCPU, 12 GiB RAM, 80 GiB thin disk, one management NIC on existing `vmbr0`, `onboot=0`; management `192.168.1.125` |
| EVE guest | `eve-ng 6.2.0-4`, kernel `6.7.5-eveng-6-ksm+`; 77 GiB filesystem, 54 GiB available at evidence collection; 6.6 GiB RAM available; `/dev/kvm` read/write and a KVM QEMU launch test passed; `ip_forward=0` |
| vEOS | Authorized image supplied at `C:\Users\Orel\Downloads\veos-4.29.2F`; vEOS-lab, EOS `4.29.2F`, build `4.29.2F-30634808.4292F`, Aboot `8.0.1`; image SHA-256 `f6d90cd479ddcdd7a3751985b699a27c9ff587117fcc795c66e4a946b5bc66b8` (qcow2), `a1d2c3a751619feb3e3367a7e0c660ebc34b8c72e51b6a05498169d70d7953f4` (ISO); vendor files remain inside EVE, outside deliverables |
| Linux nodes | Ubuntu `22.04.5 LTS` endpoint, corp service, quarantine service and control node; each has one NIC on an isolated EVE bridge; endpoint has no management NIC |
| FreeRADIUS | `3.0.26` on control node `10.70.99.2`; disposable CA and EAP-TLS; switch RADIUS source `10.70.99.1` |
| NAC | `KidCarmi/nac` commit `4911d2d940797aeb41be3815af1cf23b25573dc1`; PostgreSQL `14.24`; lab composition root added because that commit lacks its documented `cmd/nacd`; NAC bound only to EVE control bridge `10.70.99.254:18080` |

The EVE [topology export](NAC-Wired-Qualification.unl) has four internal bridge networks: access, corp, quarantine and control. vEOS Ethernet1 faces the endpoint, Ethernet2 corp, Ethernet3 quarantine, Ethernet4 control. vEOS management is disconnected. The EVE guest has no IP on access or corp; its temporary quarantine IP is for service checks, and IP forwarding is disabled. Both service nodes returned HTTP 200 on TCP 8080 from their respective bridges before endpoint isolation tests. vEOS needed QEMU 8.2.1 with `-cpu Haswell`; the stock EVE QEMU 4.1/`-cpu host` launch paused with an internal error. This change is saved on the lab node only.

## Test matrix

| Test | Expected before run | Result | Observation |
| --- | --- | --- | --- |
| Corp and quarantine service health | Each reachable from its own VLAN | **PASS** | Both returned HTTP 200 before isolation tests |
| Fresh endpoint before auth | Corp blocked | **PASS** | Corp TCP timed out; no authorized session |
| Fixed FreeRADIUS, valid EAP-TLS corp | Corp reachable | **PASS** | EAP SUCCESS, switch Access-Accept VLAN 10, Et1 SUCCESS VLAN 10, corp TCP connected |
| Fixed FreeRADIUS, same cert quarantine | Quarantine reachable, corp blocked | **PASS** | Only RADIUS VLAN result changed; after fresh session and IP refresh, Et1 VLAN 20, quarantine TCP connected, corp timed out |
| Invalid client certificate | Corp blocked | **PASS** | EAP failure/Access-Reject, Et1 FAILED with no VLAN, corp TCP timed out |
| NAC rule corp | Corp reachable, quarantine blocked | **PASS** | Matched rule ID above, segment corp, RADIUS VLAN 10, Et1 VLAN 10, corp connected, quarantine timed out |
| NAC rule quarantine | Quarantine reachable, corp blocked | **PASS** | Same identity/port/adapter; only NAC rule segment changed, RADIUS VLAN 20, Et1 VLAN 20, quarantine connected, corp timed out |
| Simulated printer MAB | Printer reaches quarantine only | **BLOCKED** | vEOS-lab rejected `dot1x mac based authentication`: `% Unavailable command (not supported on this hardware platform)`; no printer access claim |
| Rejected EAP-TLS client attempting MAB fallback | Corp blocked; fallback must not grant access | **BLOCKED** | Invalid cert did leave Et1 FAILED, fallback NONE and corp blocked, but this virtual platform could not enable MAB, so an actual fallback attempt could not be exercised |
| New login with NAC decision service down | No corp or quarantine access | **PASS** | Adapter returned `NAC decision service unavailable`; Et1 FAILED; both TCP probes timed out |
| CoA on active session | Access changes without manual reauth | **PASS** | Correctly identified CoA got ACK; active Et1 moved VLAN 20 to 10; after endpoint IP refresh, corp connected and quarantine timed out |

The fixed-policy active-session `dot1x re-authenticate Ethernet1` transition from VLAN 10 to 20 produced `FAILED-DYN-VLAN`. Clearing the session and reauthenticating succeeded. This limits the baseline claim to fresh-session reassignment. A later CoA changed the active session without manually forcing reauthentication. The first CoA request lacked the observed NAS port identifier and returned NAK `Missing-Attribute`; a corrected request with the switch-originated `Calling-Station-Id`, `NAS-Port-Id` and `NAS-IP-Address` returned ACK. CoA success is backed by the TCP probes, not just the ACK.

## Identity and NAC integration

The upstream access endpoint accepts MAC, posture and arbitrary attributes from its caller. Its service loads the endpoint record by MAC and merges supplied values over stored values. The lab [FreeRADIUS adapter](nac_radius_adapter.py) therefore runs only after FreeRADIUS validates EAP-TLS. It reads the validated client certificate serial, issuer and common name from FreeRADIUS, matches an administrator-owned, root-protected certificate-to-endpoint map, cross-checks the switch-reported Calling-Station-Id, and then sends the mapped endpoint MAC to `POST /api/v1/access/evaluate` with the service token. It sends no client-provided posture or attributes. The NAC endpoint record stores the enrolled certificate serial, which the rule matches. The adapter fails closed on missing/mismatched certificate facts, NAC outage, deny or an unmapped segment. It maps only `corp -> VLAN 10` and `quarantine -> VLAN 20`. The [composition root](nacd_lab_main.go) wires the upstream services without changing their policy engine or API implementation. [Sanitized configurations](configs.sanitized.txt) include the map and placeholder secret references.

## Handoff and hardware work

The VM and EVE topology remain running. The NAC rule was returned to `corp`; the endpoint has a valid EAP-TLS session on Et1, VLAN 10, and a final corp TCP probe connected. vEOS CoA reception is saved. The NAC service is running; a guest reboot requires starting EVE nodes, restoring the EVE control bridge address, and starting the lab NAC process as in [rerun.md](rerun.md).

Physical hardware is still needed to test printer MAB and rejected EAP-TLS fallback because this vEOS-lab platform explicitly does not support its MAB command. On a supported physical Arista switch, repeat the same isolated one-port topology with a disposable printer MAC authorized only to quarantine, then verify both allowed and blocked TCP probes. These virtual results prove this specific image's observed EAP-TLS/VLAN/CoA behavior; they do not certify a particular production switch ASIC or firmware. No Proxmox host change or missing image blocks the completed virtual tests.

Deliverables contain no vendor image, private CA/client/server key files, RADIUS shared-secret value or NAC service-token value. Raw pcaps carry public certificate/session data and encrypted RADIUS key attributes. The workstation root SSH-key enrollment attempt and a Linux lab-node password-SSH configuration attempt were rejected by automatic approval review; the former was never applied and the latter was reverted on nodes where it had been applied. Console access was used for the lab nodes.
