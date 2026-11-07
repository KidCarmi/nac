# NAC pilot: qualification status

What has been proven, on which exact commit, and where the evidence is. ASTRA
runs the lab qualification in EVE-NG. The build session runs the automated
tests. A row stays **pending** until a lab result names the commit it ran on.

## Candidates

| Candidate | Commit | nacd SHA-256 (`make pilot-bundle VERSION=<name>`) | Status |
|---|---|---|---|
| `pilot-rc1` | `fe7c9810848910aa4dc4536a5074d3ebaef72c7e` | `062e948ac922ed703ed3743a75a2bc85241bf59597b963282dae2cef3e764204` | **PASS** (static addressing, no eAPI/accounting), 2026-11-06 |
| `pilot-rc2` | `c121a952cf86c78f6a29b5950d62101c7f9bab6b` | `babe6ff523acadd0d382ca6df5dfcad5f96189b1c1b68e297027625c2f7fa6cf` | **Defects found**: quarantine lost on accounting Stop; eAPI TLS handshake with stock EOS (fixed in later candidates) |
| `pilot-rc3` | `74e5a1e338e386ad1dcbe7630dde17b9e754a9c2` | `f746d5d0908ee50644e15afce64a50e413b169aab5114a5f8d6e11488abcffc0` | **Superseded before qualification**: Codex found a quarantine bypass (failed attempt between accounting Stop and the valid login) |
| `pilot-rc4` | `76a53fd52da3e9f9e2f74b08e7b8f6064c38e7e2` | `5162ad2699f90ee62203d6df0f5ad2d52dbd5b40484ae776838799a4f3f86a40` | **Superseded before qualification**: Codex found that a replacement session could briefly exist without its inherited quarantine |
| `pilot-rc5` | `23349b7d871c9db323d09309b428b9c80046164f` | `8821f28f0df89efef135c8d38465ea1b9b10ed21f973d3de4b6f152388a0457a` | **Superseded before qualification**: Codex found that concurrent exchanges could bypass quarantine on an insert conflict |
| `pilot-rc6` | `cf8920656b44dd628334cf2934c4ba12c22a28d4` | `fa39d5be994661385a369067f56b61f529a8b446e406e6d71763b2bcccd6b4bd` | **Lab run completed** by ASTRA (results in ASTRA's local handoff). Succeeded by rc7, which adds only the installer restart fix; nacd source is identical |
| `pilot-rc7` | `e7d41626c88eef8c708b9e0dd15fa295d455193c` | `16740a48aa8a869fa50e667eaadd6ee2df3a8ef45df48ef05c4557818fba8270` | **PASS** on the API workflows below (2026-11-06), plus **one defect**: the switch Verified badge read as current after its session ended. Console UI actions not yet qualified |
| `pilot-rc8` | `62f3b08f5ee302b7a7a7de7fb0a4fce75cf52491` | `fab898e64423c32230bd49a7164be07c8c0308464e27eced218b189e29746fe2` | **PASS** on everything ASTRA could run (2026-11-07; installed in place over rc7, clean-instance install BLOCKED), including the Verified fix and a full isolated restart. Console UI actions **BLOCKED** by a stale native `window.confirm()` from an rc7 tab. One unattributed operational finding (same-VLAN re-authentication pause) |
| `pilot-rc9` | `3e92642110f97d3f42a43652d33479e82cfbcc35` | `d2d93e3ddc40a7eadcddfd3275f3d8a7aa5610ca07b43eb219bb9d59267a3749` | **PASS** on the console UI retest (2026-11-07), plus the core, restart and override runs. Server code is identical to rc8. Installed in place over rc8 for the UI run, and **clean-instance install PASS** (2026-11-07, separate guest). On the clean instance, console switch onboarding, binding, policy and CoA passed with a real vEOS switch, and verified eAPI TLS passed. Password change and host reset passed through the API and the host command. The console Account page was in the user handoff on rc9 and passed on `pilot-rc10` |
| `pilot-rc10` | `b39649f138e126ed6689a53b590855061042e341` | `35699eac162cc2a7315efced101bee3ec18ed87ab1cb108f06f9bb1a7b48195e` | **PASS, qualified candidate** (ASTRA closeout, 2026-11-07; no product defect): clean install, real vEOS switch, CoA and rule-only policy, Linux and Windows Server 2025 DHCP, invalid certificate, nacd outage, verified eAPI TLS, console Account page, full lab restart (switch config reapplied manually). Blocked: physical Arista unplug |

Evidence lives on branch `astra/lab-evidence`:
- `fe7c981` run: commit `1e0d2a3`, `lab/runs/candidate-fe7c981-20261106/`
- MikroTik MAB run: commit `43980c6`, `lab/runs/mikrotik-mab-20261106/`
- `pilot-rc10` closeout: **published**, sanitized: `astra/lab-evidence` @ `bb40c44`, `lab/evidence/pilot-closeout-20261107/` (`report.md`, `rerun.md`; `sha256-manifest.json` verifies 143 files, checked here): https://github.com/KidCarmi/nac/tree/bb40c446d8ce522187005e80a244253d85ea775e/lab/evidence/pilot-closeout-20261107
- `pilot-rc9` run: **published**, sanitized: `astra/lab-evidence` @ `50633684`, `lab/evidence/dhcp-qualification-rc9-20261107/` (`report.md`; `sha256-manifest.json` verifies 214 files): https://github.com/KidCarmi/nac/tree/50633684b9f0452f2e549cf82f92eabc2afde9a2/lab/evidence/dhcp-qualification-rc9-20261107
- `pilot-rc8` run: ASTRA's local handoff (`deliverables/dhcp-qualification-rc8-20261107/report.md`, 103 files verified)
- `pilot-rc7` run: ASTRA's local handoff (`deliverables/dhcp-qualification-rc7-20261106/report.md`, observer outage under `runs/candidate-e7d4162-dhcp-rc7-observer-outage-20261106/`). The rc7 and rc8 packages are in ASTRA's local handoff only.

## Lab results by capability

Platform: vEOS-lab 4.29.2F, FreeRADIUS `3.2.5+dfsg-3~ubuntu24.04.3`, Ubuntu
24.04. Every access verdict uses a fresh endpoint TCP connection.

| Capability | Result | Commit | Notes |
|---|---|---|---|
| Documented clean install | PASS | `fe7c981` | About 63 s; installed nacd hash matched |
| Switch registration, generated EOS config | PASS | `fe7c981` | Generated config matched the saved switch config |
| Unauthenticated endpoint blocked | PASS | `fe7c981` | |
| Valid bound EAP-TLS → corp VLAN 10 | PASS | `fe7c981` | Before binding: default deny |
| Invalid certificate rejected, both segments blocked | PASS | `fe7c981` | Also when it replaced an active quarantine on the same port and MAC |
| Preview → publish → Re-authenticate CoA → VLAN 20 | PASS | `fe7c981` | Preview prediction matched the later decision |
| Quarantine / Restore CoA without endpoint action | PASS | `fe7c981` | ACK and switch-initiated re-authentication; restore 21.3 s to permitted TCP |
| nacd outage: existing session kept, new login rejected, automatic recovery | PASS | `fe7c981` | Recovery 30.2 s after nacd restart; no authorization cache |
| Full restart of all nodes | PASS | `fe7c981` | The final restore used an operator `wpa_cli reauthenticate` (disclosed) |
| Clean install and in-place upgrade rc6 → rc7 | PASS | `e7d4162` | About 63 s on clean Ubuntu 24.04.5. After the upgrade, the installed and running nacd hashes matched and settings were preserved |
| Unauthenticated block; valid EAP-TLS → corp VLAN 10 with DHCP and TCP; invalid certificate rejected | PASS | `e7d4162` | One-NIC Linux DHCP endpoint |
| Quarantine / Restore (API) with accounting on | PASS | `e7d4162` | CoA-ACK, switch-initiated EAP-TLS, RADIUS VLAN 20 / 10, accounting Stop/Start, endpoint TCP. This is the rc2 defect scenario. The failed-attempt variant was not reported |
| DHCP addressing across corp ⇄ quarantine | PASS | `e7d4162` | No manual renewal, no port bounce. Old segment denied within 0.1–2 s. New segment reachable 87–117 s after the CoA, at the next DHCP renewal: carrier stays up, so the old lease is kept until then. See known limitations |
| Rule-only publish (v6) and restore (v2) after a full EVE restart | PASS | `e7d4162` | The preview predicted the policy result |
| nacd outage: existing session kept, new login rejected, automatic recovery | PASS | `e7d4162` | |
| Accounting Stop replay for an older session | PASS | `e7d4162` | Ignored for the newer session |
| eAPI observation through an eAPI outage | PASS | `e7d4162` | Unreachable, then stale/unknown after 5 min 20 s, then fresh/connected on recovery. Session freshness and liveness were correct |
| Switch **Verified** status during an eAPI outage | **defect** → fixed in `pilot-rc8` | `e7d4162` | Verified=yes still named an ended session while a newer one was active and reachability was "no". rc8 marks Verified as a historical record and states whether its session is still active (`TestVerifiedIsMarkedAsHistoricalRecord`) |
| `pilot-rc8`: clean source build and Linux tests, in-place install over rc7, EAP-TLS corp admission, invalid-certificate reject | PASS | `62f3b08` | A clean-instance install of exact rc8 was **BLOCKED** in the lab, not passed |
| `pilot-rc8`: natural one-NIC DHCP transitions with API CoA | PASS | `62f3b08` | After a full isolated restart: old segment denied in 0.703 / 0.685 s; new segment open 112.665 / 84.216 s after the CoA, at natural DHCP renewal. No manual renewal or reconnect |
| `pilot-rc8`: rule-only publish (v7, v8 after restart) and v2 restore; override precedence | PASS | `62f3b08` | |
| `pilot-rc8`: Verified as a historical record under an eAPI outage (API) | PASS | `62f3b08` | The console label was not checked (UI blocked) |
| `pilot-rc8`: decision-service outage and recovery; full isolated restart | PASS | `62f3b08` | |
| `pilot-rc10`: independent rebuild, clean documented install on a separate Ubuntu overlay, switch/endpoint/policy registration | PASS | `b39649f` | Rebuilt nacd identical to `35699eac…` |
| `pilot-rc10`: bound EAP-TLS → corp VLAN 10; Quarantine CoA → v2 with override, VLAN 20; Restore → corp | PASS | `b39649f` | Real vEOS 4.29.2F |
| `pilot-rc10`: one-NIC Linux DHCP, 120 s lease | PASS | `b39649f` | Quarantine CoA: corp cut 1.964 s, quarantine open 24.397 s. Restore: quarantine cut 1.386 s, corp open 110.933 s. Rule-only v3: corp cut 1.065 s, quarantine open 72.114 s. v4: quarantine cut 1.601 s, corp open 79.464 s. All natural DHCP. Captures: no T1/T2 options, unicast renewal to the old server at about 60 s, broadcast rebind at about 105 s |
| `pilot-rc10`: rule-only publish with preview | PASS | `b39649f` | The preview predicted the later decision |
| `pilot-rc10`: invalid EAP-TLS fresh login | PASS | `b39649f` | RADIUS Reject, EOS FAILED, corp blocked |
| `pilot-rc10`: nacd outage | PASS | `b39649f` | Existing session stayed corp, new authentication rejected and blocked, automatic retry accepted after recovery |
| `pilot-rc10`: verified eAPI TLS and wrong-SAN rejection | PASS | `b39649f` | With verification on, real EOS probed and observed. `endpoint_reachability` stayed `not_observed` while the switch reported fresh SUCCESS VLAN 10 |
| Same-VLAN re-authentication | **operational finding** (unattributed) | `62f3b08` | Two sampled ~20 s interruptions of fresh quarantine TCP while carrier, address and Et1 SUCCESS VLAN 20 remained. See known limitations |
| Clean-instance install of exact rc9 | PASS | `3e92642` | Separate Ubuntu 24.04.5 guest (2 vCPU, 2 GB RAM, 12 GB disk, no prior `/etc/nac`, PostgreSQL or FreeRADIUS, NAT only). Checksum-verified bundle, installed following `installation.md` with `install.sh --test-pki` from the direct Ubuntu archive. Installer exit 0: FreeRADIUS 3.2.5, PostgreSQL 16.15, adapter hash `602c3fc5…`, nacd and the reload path active, `/readyz` ready, `/version` pilot-rc9/3e92642, `/ui/` HTTP 200. About 44 s from bundle extraction to both services active (agent-operated, excluding OS bootstrap). After a reboot, all services came back automatically and the console answered. Switch onboarding was not run on this instance |
| Clean rc9 instance with a real vEOS switch: console switch onboarding, EAP-TLS certificate binding, draft preview, publish v2, corp admission, console Quarantine CoA and Restore | PASS | `3e92642` (clean instance) | Endpoint DHCP lease 120 s, no manual renewal or reconnect. Quarantine at 07:59:25Z: corp denied +1.436 s, quarantine TCP open +75.925 s. Restore at 08:01:45Z: quarantine denied +2.201 s, corp TCP open +116.100 s. EAPOL, RADIUS and DHCP captures and probes preserved |
| Verified eAPI TLS on vEOS-lab 4.29.2F | PASS | `3e92642` (clean instance) | `SSL_CERT_FILE=/etc/nac/eapi-ca.pem`, skip-verify false, TLS 1.2 / ECDHE. UI Probe, Refresh and Verify reported SUCCESS VLAN 10. Same CA with a wrong IP SAN (10.70.99.9) was rejected with an x509 hostname mismatch. EOS requires importing the certificate and key from `flash:` into `certificate:` and `sslkey:` (installation §7a) |
| Console UI: sign-in, switch and session status, policy and CoA actions | PASS | `3e92642` | In-page confirmation for Restore, Publish and Re-authenticate. Quarantine and Restore CoA on the active EAP-TLS session. Draft edit of only `bound-eap-corp.segment`, preview predicted quarantine, publish v12, re-authenticate, then restore v2 and re-authenticate. Pcaps show CoA-ACK, switch-initiated EAP-TLS and Access-Accept VLAN 20 / 10. NAC decisions v12 quarantine and v2 corp; eAPI fresh with matching VLANs |
| DHCP across UI-driven transitions | PASS | `3e92642` | The same one-NIC endpoint naturally leased 10.70.20.80 (quarantine open, corp blocked), then 10.70.10.80 after the UI restore (corp open, quarantine blocked). No manual renewal, reconnect or port bounce. DHCP ACKs 06:44:55 q, 06:47:56 corp, 06:50:56 q, 06:56:55 corp UTC. The continuous UI-run probe file is empty because the endpoint disk filled (a lab issue); one-shot TCP and pcaps cover these transitions, and the earlier rc9 core runs have continuous probe timing |
| Password change and host reset (API and host command) | PASS | `3e92642` | On the clean rc9 instance: the initial admin login and `/auth/me` returned 200. A wrong current password returned 403 and the old token stayed valid. The correct change returned 200, after which the old token and the old password were rejected (401) and the new ones worked. `nac-reset-password` exited 0, revoked the current token, changed the password, and login recovered (200). Services stayed ready |
| Password change in the console (Account page) | PASS | `b39649f` | Changed interactively in the rc10 console; the old password then returned HTTP 401. Revocation of a separately kept pre-change token was **not measured** (no token kept); it is covered by the integration tests and the rc9 API run |
| Windows Server 2025 endpoint (Desktop Experience, machine EAP-TLS, one NIC) | PASS | `b39649f` | Two natural corp → quarantine → corp cycles, no manual renewal, reconnect or port bounce. Old segment denied 1.7–2.3 s; new segment reachable 33.4–63.9 s (Q1 33.4, R1 61.4, Q2 63.9, console R2 34.4). 120 s lease, no T1/T2 options, renewals at about 60 s, T2 not measured. Server Core lacks dot3svc |
| Full isolated lab restart (EVE, control node, Windows) | PASS with manual switch reapplication | `b39649f` | The vEOS saved config held an earlier RADIUS target and the default TLS profile, so the rc10 target and the trusted eAPI profile were reapplied (lab setup, not product recovery). Then admission and console Quarantine/Restore passed: old denied 2.378 / 2.345 s, new open 62.6 / 35.5 s |
| Physical Arista unplug/reconnect | BLOCKED | — | No authorized isolated physical Arista port |
| MAB / fallback | BLOCKED on Arista | — | vEOS rejects MAB. Separate MikroTik run: a spoofed allowlisted MAC can pass switch fallback after an EAP-TLS reject (non-product rule). The product rejects non-EAP requests; see known limitations |

## Automated tests (every commit)

- `go test -race ./...`: unit tests, including the RADIUS codec, policy
  engine, versions and preview, the eAPI parser on the vEOS capture, and
  password change and revocation.
- `go test -tags=integration ./test/integration/`: the full HTTP workflow on
  PostgreSQL against a fake vEOS, every review-finding regression, and the
  account lifecycle including restart and host reset.
- `scripts/e2e-freeradius.sh`: `eapol_test`/`radclient` → FreeRADIUS 3.2.5 →
  nacd → PostgreSQL. Runs in the build environment, not in CI.
- `internal/server/openapi_coverage_test.go`: every registered route is in
  the API specification and vice versa.
- `scripts/ui-walkthrough.js` (Playwright, run before each candidate on a
  fresh nacd) drives the operator walkthrough in the console:
  - Account password change
  - switch onboarding
  - unbound deny, device record and identity binding
  - policy preview and publish (in-page confirmation), then corp admission
  - the three-fact session view
  - quarantine CoA, then quarantine at the next authentication
  - policy restore

  It also checks that no native dialog appears. The switch's RADIUS traffic
  is simulated through the adapter API, so this is a build regression check.
  It does not qualify real switch onboarding or endpoint access.

## Manual interventions recorded during lab runs

- `3e92642`: an rc8 native browser prompt left open in the lab was cancelled before the UI run. No product intervention was needed.
- `e7d4162`: none reported. The DHCP transitions needed no manual renewal or
  port bounce.
- `fe7c981`: after the full restart, the final policy restore was completed
  with an endpoint `wpa_cli reauthenticate`. Policy restore applies at the
  next authentication; the product path is the session's **Re-authenticate**.
- `fe7c981`: in the outage test, the operator stopped the supplicant and
  cleared the port to force a new session.
- `fe7c981`: browser-native confirmation dialogs needed a human click in the
  automation.
