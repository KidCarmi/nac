# NAC pilot: qualification status

What has been proven, on which exact commit, and where the evidence is. ASTRA
runs the lab qualification in EVE-NG. The build session runs the automated
tests. A row stays **pending** until a lab result names the commit it ran on.

## Candidates

| Candidate | Commit | nacd SHA-256 (`make pilot-bundle VERSION=<name>`) | Status |
|---|---|---|---|
| `pilot-rc1` | `fe7c9810848910aa4dc4536a5074d3ebaef72c7e` | `062e948ac922ed703ed3743a75a2bc85241bf59597b963282dae2cef3e764204` | **PASS** (static addressing, no eAPI/accounting), 2026-11-06 |
| `pilot-rc2` | `c121a952cf86c78f6a29b5950d62101c7f9bab6b` | `babe6ff523acadd0d382ca6df5dfcad5f96189b1c1b68e297027625c2f7fa6cf` | In qualification: DHCP transitions, eAPI observation, accounting |
| `pilot-rc3` | `74e5a1e338e386ad1dcbe7630dde17b9e754a9c2` | `f746d5d0908ee50644e15afce64a50e413b169aab5114a5f8d6e11488abcffc0` | **Superseded before qualification**: Codex found a quarantine bypass (failed attempt between accounting Stop and the valid login) |
| `pilot-rc4` | `76a53fd52da3e9f9e2f74b08e7b8f6064c38e7e2` | `5162ad2699f90ee62203d6df0f5ad2d52dbd5b40484ae776838799a4f3f86a40` | **Superseded before qualification**: Codex found that a replacement session could briefly exist without its inherited quarantine |
| `pilot-rc5` | `23349b7d871c9db323d09309b428b9c80046164f` | `8821f28f0df89efef135c8d38465ea1b9b10ed21f973d3de4b6f152388a0457a` | **Superseded before qualification**: Codex found that concurrent exchanges could bypass quarantine on an insert conflict |
| `pilot-rc6` | named in the release notes / PR (a commit cannot contain its own hash) | — | rc5 plus full session re-resolution after an insert conflict. In qualification: DHCP, eAPI, accounting, rc2 defect retests, password change |

Evidence lives on branch `astra/lab-evidence`:
- `fe7c981` run: commit `1e0d2a3`, `lab/runs/candidate-fe7c981-20261106/`
- MikroTik MAB run: commit `43980c6`, `lab/runs/mikrotik-mab-20261106/`

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
| DHCP addressing across corp ⇄ quarantine | **pending** | `pilot-rc2` | `fe7c981` used static addresses |
| eAPI session observation (fresh / stale / absent, Verify with observation) | **defect found** → fix in `pilot-rc6` | `pilot-rc2` | Probe failed with a TLS handshake failure: stock EOS 4.29.2F eAPI offers only RSA key exchange, and Go 1.22+ does not offer it. With a lab-only `GODEBUG=tlsrsakex=1`, Probe answered `vEOS-lab EOS 4.29.2F`. rc3 adds `NAC_EAPI_TLS_ALLOW_RSA_KEX` (eAPI client only), a Probe error that names the fix, and the EOS SSL-profile alternative (to be validated) |
| RADIUS accounting (Start/Stop, Class echo, session end) | **defect found** → fix in `pilot-rc6` | `pilot-rc2` | With accounting on, a Quarantine CoA was ACKed and vEOS re-authenticated, but its accounting Stop ended the session first. The replacement session had no override, so the endpoint got corp VLAN 10 and the CoA stayed "acked". rc6 carries the override to the replacement session of the same identity and port (also when a failed attempt came in between, created atomically with the session) and correlate the CoA with it (`TestQuarantineSurvivesAccountingStopAndSessionReplacement`). Silently ignored accounting is also visible |
| Password change and host reset | **pending** | `pilot-rc6` | Unit and integration tested |
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

- `fe7c981`: after the full restart, the final policy restore was completed
  with an endpoint `wpa_cli reauthenticate`. Policy restore applies at the
  next authentication; the product path is the session's **Re-authenticate**.
- `fe7c981`: in the outage test, the operator stopped the supplicant and
  cleared the port to force a new session.
- `fe7c981`: browser-native confirmation dialogs needed a human click in the
  automation.
