# ASTRA targeted retest — pilot-aruba-rc4

**D2 liveness wording: PASS.** With a stored RADIUS accounting Start and no Stop, the rc4 API and console now say `unknown (accounting Start, no Stop)` and explain that Start does not prove current connection. The [before-action state](A14-rc4-d2-state.json) and [after-Restore state](A14-rc4-restore-state.json) retain the accounting session ID and the exact note. This replaces the false “receives no RADIUS accounting” wording observed on rc3.

**CoA regression smoke: PASS within the observed scope.** Through the rc4 console, Quarantine returned ACK at 2026-11-09 20:12:13.978 UTC. A fresh endpoint TCP probe then timed out to corp (`curl` HTTP code `000`) and got HTTP 200 from remediation. Console Restore returned ACK at 20:14:07.617 UTC. A fresh corp probe then got HTTP 200, and the [switch summary](switch-restored.txt) showed dot1x Success, role `corp` on port 1/1/1. These are post-action observations, not measured interruption times. The quarantine switch role and packet exchange were not captured again in this narrow rc4 run; the full independent rc3 CoA evidence remains in the [rc3 package](https://github.com/KidCarmi/nac/tree/148e196/lab/evidence/aruba-rc3-qualification-20261109).

## Exact pin and method

| Item | Value |
|---|---|
| Source | `pilot-aruba-rc4` commit `0148bf88985c5da9086b54aafd002fbfbfe71b75` |
| Independently built `nacd` SHA-256 | `dd2b61371585d317bf07684733996c69e265dfdb9e762e354eeca4f7821b49d7`; Go 1.25.0, Linux/amd64, CGO=0, `-trimpath -buildvcs=false`, candidate Version/Commit/BuildDate ldflags |
| Runtime | The exact binary replaced rc3 `nacd` atomically in the existing isolated Ubuntu guest, then `systemctl restart nacd`; `/version` returned `pilot-aruba-rc4`, `0148bf8`, build date 2026-11-09 19:57:53 UTC. This was an in-place replacement, not a clean rc4 installation. |
| Other components | Same FreeRADIUS 3.2.5 adapter and ArubaOS-CX Virtual.10.07.0004 topology/configuration as the [rc3 baseline](https://github.com/KidCarmi/nac/tree/148e196/lab/evidence/aruba-rc3-qualification-20261109). |
| Target session | `90c79ce7-24f8-4829-89f4-2dab6381fb72`, validated certificate serial `15866D96C9295CB16DF5C204580B6E8975DDC10D`, endpoint MAC `00:50:00:00:3e:00`, port 1/1/1, accounting ID `1794254508980`. |

## Evidence matrix

| Test | Expected | Actual | Result |
|---|---|---|---|
| D2 accepted accounting Start wording | Presence remains `unknown`; note acknowledges Start/no Stop and avoids claiming current connection | Both state snapshots and console showed `unknown`, basis `accounting Start, no Stop`, and accurate uncertainty text; UI CoA confirmation used the same wording | PASS |
| Console Quarantine smoke | ACK plus corp fresh TCP denial, remediation reachable | ACK at 20:12:13.978 UTC; endpoint `corp:000`, `quarantine:200` | PASS |
| Console Restore smoke | ACK plus corp fresh TCP recovery | ACK at 20:14:07.617 UTC; endpoint `corp:200`, `quarantine:200`; switch role `corp`, VLAN 10 | PASS |
| Full clean rc4 install, admission, policy, chaos, restart, Arista, Windows and physical switch regression | Repeat complete matrix on exact rc4 | Not run in this targeted replacement check | BLOCKED (not qualified on rc4) |

The endpoint probe results above were observed in the live serial tool session; this narrow run did not start a bounded continuous probe or packet capture before the actions. No timing or packet claim is made from them. The rc3 package has the complete captures for the previous binary and must not be relabeled as rc4 evidence.

## Rerun

1. Build and verify the exact source/binary pin above. Start from the isolated [rc3 topology and sanitized configuration](https://github.com/KidCarmi/nac/tree/148e196/lab/evidence/aruba-rc3-qualification-20261109); keep test keys and RADIUS secrets only on the lab guest.
2. Enable `aaa accounting port-access start-stop group radius` on the isolated switch. Authenticate a bound valid EAP-TLS endpoint and confirm the NAC session has `acct_session_id`, an accounting `Start` event and no `Stop`.
3. Open Sessions → session detail and the Quarantine confirmation. Require `unknown (accounting Start, no Stop)` and a note that Start does not prove current connection.
4. With bounded fresh TCP probes and a bounded CoA capture already running, use console Quarantine and Restore. Record CoA ACK/NAK, switch role (`show port-access clients interface 1/1/1`), endpoint probes and DHCP state for both actions. Treat the rc4 smoke here as a regression check, not a full qualification run.

No vendor image, private key, shared secret, admin credential or token is included.
