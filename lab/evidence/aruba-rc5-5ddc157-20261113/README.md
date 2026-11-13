# ASTRA bounded rc5 verification — 2026-11-13 UTC

**Exact candidate:** `5ddc15732a7a49bc65df9a0338351cb9dbec55b0` (`pilot-aruba-rc5`); installed `nacd` SHA-256 `43efe22aa63cf54cb4aaf5216cb58bfee65efb61dc56c8aff69499843d95f6dc`; unchanged FreeRADIUS adapter aggregate SHA-256 `3c45063efb451ff86dac23d695e9c57c307b86ef8054b790a759cbb63e80db32`. The running `/version` returned `pilot-aruba-rc5`, commit `5ddc157`; the installed `/usr/local/bin/nacd` hash matched. Go 1.25.0, linux/amd64, CGO off, `-trimpath`. FreeRADIUS 3.2.5; ArubaOS-CX Virtual 10.07.0004; EVE-NG 6.2.0-4 on Proxmox VE 9.2.2 VM 102.

This is the narrow [PR #5 assignment](https://github.com/KidCarmi/nac/pull/5#issuecomment-6848120081), not a fresh qualification of all pilot behavior or release clearance. The [rc4 clean run](../aruba-rc4-clean-20261110/README.md) and [Arista rc4 run](../arista-rc4-regression-20261110/README.md) remain separate and unchanged.

## Candidate installation and topology

The exact commit was checked out in a detached worktree and built on EVE from a Git bundle (bundle SHA-256 `aecc7e328538060ac9fe212005417816ae9450983dcde7762a151da25b893aea`). `nacd` alone was swapped into a **new 12 GB child copy-on-write overlay** backed by the stopped rc4 control disk. The adapter, database, PKI, FreeRADIUS and switch configurations came from that preserved rc4 disk. The initial `/readyz` check in the swap script raced service startup and exited nonzero; `nacd` then reached `active`/`ready` without repair. This is a recorded harness timing issue, not an installation failure. No rc4 disk was written by rc5.

The isolated [topology export](Aruba-Role-ACL-1007.unl) hashes to `004c7fbf948fa3e9fe06367c94a889c88a73afb56c0b65719006e10f3970ae94`. EVE nodes 61–65 were used. The endpoint had one data NIC `ens3`, DHCP address `10.70.10.50/24`, no management bypass; corp and remediation services at `10.70.10.10:8080` and `10.70.20.10:8080` independently returned HTTP 200 from their own segments before the transition tests. The switch used port 1/1/1 with VLAN 10 fixed, local corp/quarantine roles. The [rc4 sanitized config](../aruba-rc4-clean-20261110/sanitized-switch-config.txt) is the unchanged switch baseline.

The virtual AOS-CX did **not** authenticate automatically after its cold boot. Before manual recovery the client was absent from switch state and both endpoint TCP probes timed out. At about 08:58 UTC, the operator ran the documented **port-scoped** authenticator disable/enable under interface 1/1/1; EAP-TLS then succeeded and corp HTTP returned 200. This is a manual prerequisite. Automatic cold-start recovery remains **FAIL**. The sequence and impact are in the [rc4 handoff](../aruba-rc4-clean-20261110/README.md); no full performance or cold-boot campaign was repeated here.

## Exact-candidate matrix

| PR #5 row | Result | Direct evidence and limit |
|---|---|---|
| P1, pinned install | **PASS** | `/version` commit `5ddc157`; installed binary SHA-256 above; rc4-backed child overlay and unchanged adapter. One readiness-check race in the install harness, with no operator repair. |
| 1.2, switch onboarding | **PASS** | Temporary UI switch `rc5-onboarding-probe` showed `Configured: pending` at 08:55:40Z, then `yes` and `Loaded by FreeRADIUS: yes` after reload at 08:55:41Z. It was deleted after the run. The real switch returned to `Configured: yes` after D1 cleanup. |
| 1.3, policy hint, preview, publish | **PASS** | Policy page names “Apply active policy on a role switch.” A rule-name-only draft preview predicted zero changed decisions; UI published version 5 at 08:56:09Z. Original version 2 was restored at 09:06:30Z, with the override semantics shown in the confirmation. |
| 1.6, Aruba Apply active policy | **PASS** | Session button read “Apply active policy”; no Re-authenticate button. Confirmation said the switch does not re-authenticate. At 09:02:00.760439Z CoA ID `0xc5` targeted `wired-endpoint-01`, port 1/1/1, with Aruba-User-Role `corp`; ACK at 09:02:00.783940Z. No Tunnel attributes or EAPOL in the action capture. Switch stayed dot1x Authenticated, role corp Applied, VLAN 10; endpoint address unchanged. UI displayed `acked`, not traffic verification. API/history still calls the action `reauthenticate` by contract. |
| 3.1, UI Quarantine | **PASS for enforcement** | UI action at 09:03:03.541970Z sent Aruba-User-Role `quarantine`, CoA ID `0x2c`; ACK at 09:03:03.588060Z. Switch showed quarantine Applied, dot1x still Authenticated, VLAN 10; endpoint kept `10.70.10.50/24`. Fresh corp TCP timed out; remediation returned HTTP 200. The bounded diagnostic probe has 59 steady-state samples from 09:04:26–09:05:13Z. Its attempted transition logger exited early, so **transition time is NOT RUN**, and the ACK is not claimed as traffic proof. |
| 3.2, UI Restore | **PASS** | UI action at 09:05:51.742865Z sent Aruba-User-Role `corp`, CoA ID `0xf4`; ACK at 09:05:51.776250Z. Same-clock endpoint pcap first showed a corp SYN/ACK at 09:05:51.858845Z, 0.116 s after request; subsequent fresh HTTP returned 200. Continuous 60-row probe log spans 09:05:23–09:06:09Z and changes from corp timeout to HTTP 200. Remediation returned 200 in both roles. No EAPOL, VLAN change, IP renewal or manual endpoint action in this restore. |
| 2.1, D1 C1–C6 real systemd | **PASS** | [Full log](rc5-d1-chaos.log): 20 PASS, 0 FAIL, 09:09:51–09:10:21Z. Burst onboarding/rotation converged; IPv4-mapped IPv6 was rejected; after FreeRADIUS kill the current list loaded; deliberately stopped FreeRADIUS stayed stopped and NAC reported `no`; restart/cleanup restored `yes`, with zero chaos clients left. `systemctl kill` emitted an auxiliary-process warning, but the FreeRADIUS main start timestamp advanced and ground truth loaded. |
| 5.2, Arista console label/action | **BLOCKED** | The existing isolated vEOS topology was booted on a disposable rc5 child overlay, but its control guest exposed no SSH service. Automatic approval review rejected logging in as root over serial and starting SSH as an access expansion. No rc5 binary was installed there and no Arista UI action was run. Nodes 31–35 were stopped and both original baseline and rc4 COW restored. Required prerequisite: an authorized, already enabled management path to the isolated Arista control guest, or explicit approval for the serial/SSH access change. |
| Virtual AOS-CX cold-start automatic recovery | **FAIL** | No switch client or corp access after cold boot. A manual interface 1/1/1 authenticator disable/enable was required; subsequent success does not count as automatic recovery. |
| Windows, physical switch, shared-port, IPv6, UDP, DHCP-renewal behavior | **BLOCKED / NOT RUN** | No one-NIC Windows EAP-TLS guest or authorized physical switch; this bounded rc5 run used unchanged single-host, fixed-VLAN role policy and fresh IPv4 TCP. These are not inferred from the CoA ACK or rc4 results. |

The role CoAs contain Aruba vendor attribute 14823/1 and **no Tunnel-\*** attribute. Captures of the Apply, Quarantine and Restore windows each have zero endpoint EAPOL frames. Packet checksum warnings in the EVE tap are virtual NIC checksum offload artifacts; response packets and endpoint HTTP determine results.

## Evidence and rerun

- [Sanitized packet timeline](packet-summary.md) derives the role CoA, EAPOL and TCP facts from four EVE taps per action, each capped at 180 seconds or 5,000 packets. The raw capture archive stays in the local qualification workspace at `D:\AI\nac-wired-lab\qualification\rc5-role-captures.tar.gz` and on EVE at `/mnt/nac-rc5/rc5-role-captures.tar.gz`; its SHA-256 is `32e00cafeb8b81fcd6932203216caf3dcb20fe7e3a58c65385003fec37ffd109`. It is excluded from this GitHub package because it was not fully sanitized.
- [Quarantine probes](rc5-quarantine-probes.jsonl) and [Restore probes](rc5-restore-probes.jsonl) use fresh TCP connections. The quarantine file begins only after the role transition; it is not timing evidence. The restore file covers the transition.
- [D1 log](rc5-d1-chaos.log) records each test and the FreeRADIUS/systemd ground truth. The [manifest](sha256-manifest.json) hashes every delivered file.

To repeat in this isolated lab, first verify the exported topology hash and service health. Start only nodes 61–65 using the preserved EVE runner, launch a fresh rc5 COW child from the stopped rc4 disk, verify `/version` and `sha256sum /usr/local/bin/nacd`, and record the automatic boot observation. If the virtual authenticator is stuck, document the failure before the port-scoped reset:

```text
configure terminal
interface 1/1/1
aaa authentication port-access dot1x authenticator
disable
enable
end
show port-access clients interface 1/1/1 detail
```

This reset affects **only port 1/1/1** and drops any clients on it; verify dot1x Authenticated, the applied local role, fixed VLAN 10, endpoint DHCP address and a fresh TCP probe. From the UI, register a disposable switch, inspect configured/loaded, preview a rule-name-only draft, publish, open the active session, apply policy, Quarantine, Restore, then restore policy v2 and delete the disposable switch. Capture CoA/EAPOL/TCP on the same EVE clock for every action. Run the candidate's documented `scripts/chaos-freeradius-reload.sh` only on the isolated control guest with lab credentials supplied at runtime, and verify no `chaos-*` switches remain. Stop nodes 61–65 and the rc5 child after collection. Do not reuse the rc5 result as rc4 evidence.

**Final lab state:** Aruba nodes 61–65 and Arista nodes 31–35 stopped; rc5 control child stopped; Arista original disk SHA-256 `8d51b4fc45307ede0a0e39eb516df2815550fbba76a04e4b0a5e806f1d660c1d` restored; rc4 COW restored at its original path; temporary EVE Arista control address detached; IP forwarding still 0. The user-authorized additional 40 GB VM 102 disk is ext4 mounted at `/mnt/nac-rc5` with 33 GB free at last check. No vendor image, credential, private key or shared secret is included.
