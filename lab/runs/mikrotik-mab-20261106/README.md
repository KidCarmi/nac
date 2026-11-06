# MikroTik CHR MAB qualification — 2026-11-06 UTC

**MAB port enforcement: PASS on MikroTik CHR 7.24.6.** A fixed FreeRADIUS authorization for printer MAC `00:50:00:00:02:00` on `ether1` produced Access-Accept/VLAN 20; the CHR session showed `auth-info=mac-auth`, `vlan-id=20`, and fresh endpoint TCP was corp BLOCKED/quarantine OPEN. An unregistered MAC was rejected and both TCP targets were blocked.

**Rejected EAP-TLS fallback: FAIL for an allowlisted MAC.** While the invalid-certificate supplicant kept responding, EAP-TLS was rejected and fresh TCP was blocked. After the operator **stopped** that supplicant, the same endpoint using the printer MAC received MAB Access-Accept/VLAN 20 and reached quarantine. The unregistered MAC remained blocked after the same sequence. This exposes the inherent spoofability of MAC-based admission on a dual-mode port. No corp access was observed. A brief MAB accept at 12:31:48 before the EAP exchange was not covered by continuous TCP probes, so transient access in that interval is unmeasured. The post-rejection MAB success is a distinct, measured result; it was triggered after the manual supplicant stop, not by the reject packet alone.

**KidCarmi/nac MAB: BLOCKED/unqualified.** This test used an exact fixed rule in the cloned FreeRADIUS control node because the current NAC adapter rejects non-EAP requests. The NAC product did not decide the MAC authorization. This result does not change the exact Arista `fe7c981` qualification in [the parent report](../../qualification-report.md).

## Inventory and isolation

| Item | Observed value |
| --- | --- |
| Platform | Existing Proxmox VM 102, EVE-NG 6.2.0-4, nested KVM. Existing Proxmox bridges/other VMs unchanged. |
| Vendor image | Official MikroTik CHR long-term 7.24.6, 1 vCPU/1 GiB; source ZIP SHA-256 `692672e6cac9cfaa4f37c6334d07760d49e074b50befc61f0904270553f17126`, installed qcow2 SHA-256 `8069d35c3454915c4bc3a3fb037dcf40d835b9c34779d9a771c90f5c3228a4de`. Vendor disk excluded. CHR free tier has a 1 Mbps per-interface cap ([official CHR documentation](https://help.mikrotik.com/docs/spaces/ROS/pages/18350234/Cloud%20Hosted%20Router%20CHR)). |
| Topology | [NAC-MAB-MikroTik.unl](NAC-MAB-MikroTik.unl), SHA-256 `527f9a4ab2070facd443c3d16e6d3af042769c08282b079d9d8125d6e84bef3a`, EVE lab UUID `29079d7d-fa67-4b16-9951-f8dfe9dd830d`; nodes 21 CHR, 22 one-NIC endpoint, 23 corp target, 24 quarantine target, 25 cloned control. Isolated access, corp, quarantine and control bridges. |
| Control | Cloned exact `fe7c981` control overlay; FreeRADIUS `3.2.5+dfsg-3~ubuntu24.04.3`. The original Arista control disk and policy files were not modified. Cloned overlay SHA-256 `13e1a1f663a707123ad3b8a50a0b8b4868ea857032ef62321cfe3f2930cb98ce`; contains secrets and is excluded. |
| Endpoint | One data interface `ens3`, no management interface or default route. Static source addresses `10.70.10.50/24` and `10.70.20.50/24` on that interface for fresh TCP probes. Changing the MAC bounced the Linux link and removed the secondary quarantine address; it was explicitly restored before the final allowed-MAC probe. |

Before admission tests, independent source-side HTTP probes returned **200** from corp `10.70.10.10:8080` on the corp bridge and quarantine `10.70.20.10:8080` on the quarantine bridge. EVE IP forwarding was **0**. A stale host-only address on the stopped baseline quarantine bridge initially sent the service-health probe to the wrong bridge; the health helper temporarily removed that address, verified both services, then restored it. This was a host route issue, not a CHR enforcement result.

## Evidence matrix

| Case and expected result | Actual endpoint TCP and switch/RADIUS evidence | Status |
| --- | --- | --- |
| No fixed MAB allow, printer MAC: both blocked | RADIUS Access-Request with `User-Name=005000000200`, `Calling-Station-Id=00-50-00-00-02-00`, `NAS-Port-Id=ether1` rejected; fresh corp/quarantine blocked. | PASS |
| Fixed printer MAB: quarantine open, corp blocked | 12:30:26.852 Request id2; .853 Accept id2 with Tunnel-Type VLAN, Medium IEEE-802, Group 20. CHR `auth-info=mac-auth`, VLAN20, authorized, dynamic VLAN20/ether1. Fresh corp blocked/quarantine open. | PASS |
| Invalid certificate, responding supplicant: both blocked | Repeated EAP-TLS Access-Reject/EAPOL Failure, including id9 at 12:32:02.151; fresh corp/quarantine blocked while supplicant ran. CHR had no active authorized session at observation. A prior MAB Accept id3 at 12:31:48.902 preceded EAP; its reachability window was not measured. | PASS for observed interval; transient window BLOCKED |
| Rejected client stops EAP while using *allowlisted printer MAC*: expected blocked | Operator stopped supplicant. At 12:33:16.411 MAC Request/Accept id55 assigned VLAN20; CHR `auth-info=mac-auth`, VLAN20; fresh corp blocked/quarantine open. | **FAIL** |
| Unregistered MAC `00:50:00:00:02:01`: both blocked after a fresh session | MAC Request id56 at 12:34:25.395 rejected at :26.405; fresh both blocked. Invalid EAP id62 rejected at 12:35:08.459. After operator stopped supplicant, MAB Request id78 at 12:35:46.555 rejected at :47.569; fresh both blocked. A [later retest](final-retest.md) confirmed both addresses on `ens3`, no active CHR session, and both TCP targets blocked **after an explicit session reset**. Before reset, the previous printer session still permitted quarantine even with the endpoint MAC changed. | PASS for the fresh session |
| Final address-corrected allowed-MAC repeat: quarantine open, corp blocked | With CHR `auth-info=mac-auth`, VLAN20, first probe was both blocked because Linux retained only corp source address after MAC change. Restoring both source addresses on `ens3` yielded fresh corp blocked/quarantine open. | PASS after addressing correction |
| NAC product decides MAB | Product adapter/site had no MAB decision path; fixed FreeRADIUS rule was required in the isolated clone. | BLOCKED |

[RADIUS packet capture](radius.pcap), [RADIUS timeline](radius-timeline.txt), [endpoint EAPOL capture](eapol.pcap), and [EAPOL timeline](eapol-timeline.txt) preserve packet chronology. The [final console retest](final-retest.md) records the later address correction and fresh-session check; it happened after the pcaps stopped. `radius.pcap` SHA-256 `f7d70ee39973fb6215ecf7d9f6f3fe1360524cd44346adf2f5a827ee4ae9f556`; `eapol.pcap` SHA-256 `536d282e137e2febc886336e035bcfbd1ef649e5eebea1b60640cba28bea942`. Known plaintext lab credential scan found **0 matches** in both captures.

## Sanitized configuration

```routeros
/ip address add address=10.70.99.1/24 interface=ether4
/interface bridge add name=nac-bridge protocol-mode=rstp vlan-filtering=no
/interface bridge port add bridge=nac-bridge interface=ether1 edge=yes pvid=1
/interface bridge port add bridge=nac-bridge interface=ether2 edge=yes pvid=10
/interface bridge port add bridge=nac-bridge interface=ether3 edge=yes pvid=20
/interface bridge vlan add bridge=nac-bridge vlan-ids=10 untagged=ether2
/interface bridge vlan add bridge=nac-bridge vlan-ids=20 untagged=ether3
/interface bridge set nac-bridge vlan-filtering=yes
/radius add address=10.70.99.2 secret=[REDACTED] service=dot1x
/interface dot1x server add interface=ether1 auth-types=mac-auth mac-auth-mode=mac-as-username radius-mac-format=XXXXXXXXXXXX accounting=no retrans-timeout=3s auth-timeout=20s
# Later dual-mode test:
/interface dot1x server set [find interface=ether1] auth-types=dot1x,mac-auth retrans-timeout=5s
```

The isolated FreeRADIUS `authorize` rule matched **all** of: `User-Name=005000000200`, `Calling-Station-Id=00-50-00-00-02-00`, `NAS-Port-Id=ether1`, and absence of `EAP-Message`; it returned `Auth-Type := Accept` and VLAN20. [Exact rule installer](mab_radius_patch_guest.py) includes rollback on failed `freeradius -XC`. The RADIUS shared secret and CHR admin password are root-only on EVE and absent from this export. RouterOS wired Dot1X/MAC auth and dynamic VLAN behavior are documented by [MikroTik](https://help.mikrotik.com/docs/spaces/ROS/pages/328090/Dot1X).

## Rerun

Use the existing EVE guest transport `D:\AI\nac-wired-lab\eve_guest.py` from the Windows host and Python environment `D:\AI\.venv-culvcenter\Scripts\python.exe`. Run the commands below from PowerShell after copying these scripts to a local directory. The topology and cloned control/image must already be present on EVE; no keys, secrets or vendor disk are in this export. The original Arista lab is stopped during the MikroTik test.

```powershell
$py = 'D:\AI\.venv-culvcenter\Scripts\python.exe'
$eve = 'D:\AI\nac-wired-lab\eve_guest.py'
$run = 'D:\AI\nac-lab-evidence-repo\lab\runs\mikrotik-mab-20261106'
& $py $eve 'MAB_TRANSITION_ACTION=inspect python3 -' --script "$run\mab_transition_guest.py"
& $py $eve 'TRANSITION_ACTION=stop_candidate python3 -' --script 'D:\AI\nac-lab-evidence-repo\lab\candidate_transition_guest.py' # only if Arista nodes are running
& $py $eve 'MAB_TRANSITION_ACTION=start_mikrotik python3 -' --script "$run\mab_transition_guest.py"
& $py $eve 'python3 -' --script "$run\mab_service_health_guest.py"
& $py $eve 'MAB_ROUTEROS_ACTION=mab_status python3 -' --script "$run\mab_routeros_guest.py"
& $py $eve 'ENDPOINT_CONSOLE_PORT=32790 ENDPOINT_ACTION=probe python3 -' --script 'D:\AI\nac-lab-evidence-repo\lab\candidate_endpoint_guest.py'
```

For a fresh MAB session, stop the endpoint supplicant, set the endpoint MAC with `ENDPOINT_ACTION=mac_printer` or `mac_unregistered`, restore **both** static probe addresses with `ENDPOINT_ACTION=fresh`, then run `MAB_ROUTEROS_ACTION=reset_mab`. Wait for `mab_status` to show the session result, and run `ENDPOINT_ACTION=probe`. For invalid EAP-TLS, use `ENDPOINT_ACTION=invalid_start` on the same port, reset the CHR session, observe repeated EAP rejects and fresh TCP; then use `ENDPOINT_ACTION=stop`, wait for MAC fallback and probe again. Capture RADIUS on `vnet0_24` and EAPOL on `vnet0_21`. The fixed rule is confined to the cloned control; see [rule installer](mab_radius_patch_guest.py). If rebuilding from scratch, run [image preparation](prepare_mikrotik_guest.py), [topology builder](make_mikrotik_topology.py), [control clone](clone_mab_control_guest.py), then the transition and CHR configuration helpers; the authorized image download and existing root-only EVE credentials are prerequisites.

To restore the prior lab, run `MAB_TRANSITION_ACTION=stop_mikrotik`, then `MAB_TRANSITION_ACTION=restore_arista` using the transition helper. It verifies topology hashes, node IDs, bridge addresses and disabled EVE forwarding before moving the isolated control address. The CHR image and cloned control overlay remain inside EVE for a repeat.

## Next product and hardware step

OPUS needs a **separate, explicit MAB policy path** if printer admission is desired. A MAC is an easily copied identifier, so a policy must limit it to the intended switch/port and lowest-privilege segment, expose that weaker assurance in the decision/evidence contract, and define whether a rejected EAP identity is permitted to try MAB. This lab shows why a generic EAP-to-MAB fallback cannot be treated as certificate identity. A physical Arista with supported MAB is still needed to qualify the original Arista fallback behavior; this MikroTik CHR result does not supply that hardware proof.
