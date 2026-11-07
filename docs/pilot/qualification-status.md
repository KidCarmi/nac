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
| `pilot-rc8` | the commit that adds this row (commit and nacd hash in the PR; a commit cannot contain its own hash) | — | rc7 plus the Verified badge fix (marked historical, with the state of its session) and these docs. To retest: the Verified badge, console UI actions |

Evidence lives on branch `astra/lab-evidence`:
- `fe7c981` run: commit `1e0d2a3`, `lab/runs/candidate-fe7c981-20261106/`
- MikroTik MAB run: commit `43980c6`, `lab/runs/mikrotik-mab-20261106/`
- `pilot-rc7` run: ASTRA's local handoff (`deliverables/dhcp-qualification-rc7-20261106/report.md`, observer outage under `runs/candidate-e7d4162-dhcp-rc7-observer-outage-20261106/`). Packet captures remain local; not on the evidence branch.

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
| Console UI: onboarding, policy and CoA actions | **pending** | `e7d4162` | The API passes above are not claimed as UI passes |
| Password change and host reset | **not reported** | — | Unit and integration tested |
| Windows endpoint | untested | — | |
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

## Manual interventions recorded during lab runs

- `e7d4162`: none reported. The DHCP transitions needed no manual renewal or
  port bounce.
- `fe7c981`: after the full restart, the final policy restore was completed
  with an endpoint `wpa_cli reauthenticate`. Policy restore applies at the
  next authentication; the product path is the session's **Re-authenticate**.
- `fe7c981`: in the outage test, the operator stopped the supplicant and
  cleared the port to force a new session.
- `fe7c981`: browser-native confirmation dialogs needed a human click in the
  automation.
