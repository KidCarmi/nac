# ASTRA independent qualification — `pilot-aruba-rc2` (partial, candidate FAIL)

**Did this candidate enforce a role on the virtual Aruba port? Yes, in A1–A3.** The [packet captures and endpoint probes](evidence/) show certificate admission to `corp`, then NAC CoA to `quarantine` and back. The switch role, RADIUS packets and fresh TCP outcomes agree. This was an **API-driven** product run; the console action remains unverified because the task browser is at the disposable certificate warning.

**Is this candidate fit for the pilot? No.** The independent [reload chaos run](reload-chaos.log) found a persistent missed reload under burst updates. `pilot-aruba-rc2` is **FAIL** pending OPUS's replacement. The manual reload that repaired this lab instance is recorded below and is not a pass.

## Pin and lab

| Item | Value |
|---|---|
| Product commit | `9f0dba137dd4b284abaaab090d32bf3fe3a21ada` |
| `nacd` SHA-256, independently built and installed | `333feaa70191e4e07c83cf089e4058d46b057e8e4b2eb2ff0aa1da08366a23f0` |
| Installed adapter aggregate SHA-256 | `3c45063efb451ff86dac23d695e9c57c307b86ef8054b790a759cbb63e80db32` |
| `sites-available/nac` SHA-256 | `d992840840bad3647c7bbb71afae59eaf4bd0981303b9465d7469c0306b75d1e` |
| OS / RADIUS / database | Ubuntu 24.04.5 LTS / FreeRADIUS 3.2.5 / PostgreSQL 16.15 ([install report](install-report.txt)) |
| Switch | ArubaOS-CX Virtual.10.07.0004, isolated EVE-NG 6.2.0-4 VM 102; image SHA-256 `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25`; third-party image provenance remains unverified |
| Switch/session | 10.70.99.1, port 1/1/1, endpoint MAC 00:50:00:00:3e:00, session `ff97df77-c43e-464d-a0be-a03236264215` |
| Candidate control | Clean copy-on-write guest on EVE isolated control bridge, 10.70.99.5; rc10 baseline guest unchanged |
| Endpoint | One `ens3` data NIC, systemd-networkd DHCP, 10.70.10.50/24, default route 10.70.10.1, DNS 10.70.10.10 |

The switch used the existing local `corp` and `quarantine` roles and its quarantine ingress policy. The old reference FreeRADIUS was stopped during new logins. Corp service `10.70.10.10:8080` and remediation service `10.70.20.10:8080` each returned HTTP 200 when probed independently from its own segment. The endpoint retained one data interface and no management bypass.

## Results

| Check | Expected | Actual | Status |
|---|---|---|---|
| Clean install | Documented install boots exact candidate | Installer completed; `nacd`, FreeRADIUS and reload path active; running `/version` says `9f0dba1`; installed hashes match pin | PASS |
| D1 reload burst | Client file and running FreeRADIUS converge after each update | C2, C3 and cleanup did not converge in 12 s; cleanup still mismatched after >60 s | **FAIL** |
| Mapped source input | Reject before client-file write | Both `::ffff:` forms returned HTTP 422 | PASS |
| FreeRADIUS deliberately stopped | Status reports not loaded | NAC reported `no`; restart and explicit reload recovered | PASS |
| Fresh unauthenticated port | Corp blocked | No switch client; healthy corp and remediation both timed out from endpoint | PASS |
| Valid certificate, unbound | Policy default deny | EAP-TLS identity was validated, NAC default-denied, switch `Fail`, Access-Reject and corp TCP timeout. `wpa_supplicant` logged EAP success despite final RADIUS reject; switch and TCP decide access | PASS |
| Bound certificate, corp | Access-Accept role corp; corp TCP works | Bound the **session's validated issuer and serial** to the endpoint; published policy v2; Access-Accept carried Aruba vendor 14823/type 1 `corp`, no Tunnel attributes; switch `Success corp`; fresh corp HTTP 200 | PASS |
| Product Quarantine (API) | CoA role quarantine; corp blocked; remediation works; same DHCP IP | CoA-ACK ID 0x86; switch `quarantine`; 0 corp HTTP 200 after ACK, remediation continued; no EAPOL or RADIUS reauth; same IP | PASS |
| Product Restore (API) | CoA role corp; corp works; same DHCP IP | CoA-ACK ID 0x6f; switch `corp`; fresh corp HTTP 200 resumed; no EAPOL or RADIUS reauth; same IP | PASS |
| Console Quarantine/Restore | Same outcomes by operator UI | Browser remains at self-signed certificate warning; no UI action counted | BLOCKED |
| A5–A10 persistence, NAK, timeout, stale session, flow limits, Arista regression | As in [OPUS's plan](https://github.com/KidCarmi/nac/blob/9f0dba137dd4b284abaaab090d32bf3fe3a21ada/docs/pilot/aruba-role-path.md) | Not run on this failed candidate; replacement retest pending | BLOCKED |
| Windows / physical switch | Full pilot behavior | No authorized physical Aruba; this run used Linux. Windows 10/11 Aruba role behavior remains untested | BLOCKED |

## D1 replacement defect, exact trigger

Run `scripts/chaos-freeradius-reload.sh` on the clean candidate with `CHAOS_BURST=8`, `CHAOS_SETTLE_SECONDS=12`, `CHAOS_OUTAGE=1`. It reported **17 PASS, 3 FAIL**. At 18:47:33 UTC, the current client file SHA-256 was `63384afa88a65616f310b9a23a250bc8fc4609875b0a4269facabb0d7a0c6bf3`, while `freeradius-reload.status` said `result=ok`, `clients_sha256=7e96f284f86cbcb4d75eb394cce202326d8535a86679c7ab6bd5a412bbdbe279`, `at=18:47:21Z`. FreeRADIUS was active but had not confirmed the current file. At 18:48:34 UTC, >60 s after the last write, the hashes still differed and `/api/v1/radius/health` said `freeradius_load=no`. **Diagnostic manual intervention:** `systemctl start nac-freeradius-reload.service` at 18:48:53 UTC made the recorded hash equal the file hash and restored `yes`.

The runner's old “loaded” check used whole-second service/file timestamps, so its C2/C3/cleanup ground-truth passes were false positives when a write and restart shared a second. The exact hash mismatch is the decisive evidence. OPUS has accepted the defect and is preparing `pilot-rc13` / `pilot-aruba-rc3` plus a hash-aware runner. C5's `systemctl kill` printed an auxiliary-process error; its PASS proves recovery afterward, not that every intended kill occurred.

## Product CoA timing (EVE host clock)

| Transition | CoA ACK | First fresh packet/outcome after ACK | Other evidence |
|---|---|---|---|
| Corp → quarantine | 18:58:49.235421 UTC | Corp SYN at **+0.374 s**, no corp HTTP 200 for rest of 70 s capture; remediation HTTP 200 at **+0.378 s** | 30 corp successes before ACK, 0 after; remediation 30 before, 42 after. Endpoint probe log: corp 30 success then 42 timeouts; remediation 72/72 success |
| Quarantine → corp | 19:00:23.540160 UTC | Corp HTTP 200 at **+0.113 s** | 0 corp successes before ACK, 68 after. Endpoint probe log: corp 18 timeouts then 68 successes; remediation 86/86 success |

The endpoint kept `10.70.10.50/24`, default route and DNS. DHCP renewal traffic occurred near the transitions; the switch kept VLAN 10. There were zero EAPOL and zero RADIUS authentication packets in either CoA capture window. The ACK proves switch acceptance; switch role and fresh TCP prove the observed traffic change. NAC correctly reports `observed.freshness=unavailable` and `endpoint_reachability=not_observed` for AOS-CX. Guest wall clocks differed, so the timing above uses only EVE-host packet timestamps. An established connection was **not** measured on this candidate.

## Rerun and artifacts

1. Use the exact commit above and `make pilot-bundle VERSION=pilot-aruba-rc2` with Go 1.25.0, Linux/amd64, CGO off. Verify `nacd` and adapter hashes against [install-report.txt](install-report.txt). The source archive was generated with LF line endings; Windows `core.autocrlf=true` changes adapter bytes and must be disabled for this build.
2. Prepare a separate overlay from the authorized Ubuntu base and attach only its data/control NIC to the Aruba topology's isolated `vnet0_64` bridge. The [topology export](Aruba-Role-ACL-1007.unl) identifies the switch, endpoint and service links. Install with `deployments/pilot/install.sh --test-pki` on the clean guest. Keep generated test keys on the guest.
3. Register the switch with platform `aruba-aos-cx`, source/NAS IP `10.70.99.1`, server IP `10.70.99.5`, roles `corp`/`quarantine`, access VLAN 10, port `1/1/1`. Wait for FreeRADIUS load `yes`; compare its recorded hash to the client file. Point only the isolated switch at 10.70.99.5 using its root-only shared secret; stop the reference FreeRADIUS.
4. Stop the old supplicant and clear the one test port session. Probe both healthy services. Start the disposable EAP-TLS client, confirm default deny, bind the validated session certificate, publish `identity.bound=true` and `mac.matches_device=true` → `corp`, then reauthenticate. Capture EAPOL, RADIUS and endpoint probes.
5. Start bounded fresh TCP probes and packet captures **before** NAC Quarantine and Restore. Analyze the pcaps with [aruba_rc2_coa_analyze.py](aruba_rc2_coa_analyze.py) pointed at [evidence/](evidence/). Inspect [product-state.json](product-state.json), [switch transcript](evidence/switch-after-restore.txt), and [reload-chaos.log](reload-chaos.log).

No private keys, shared secrets, vendor images or admin tokens are included. The archive and files are SHA-256 listed in [sha256-manifest.json](sha256-manifest.json).
