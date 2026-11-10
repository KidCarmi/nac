# ASTRA clean qualification: pilot-aruba-rc4

**Candidate:** `0148bf88985c5da9086b54aafd002fbfbfe71b75`; installed `nacd` SHA-256 `dd2b61371585d317bf07684733996c69e265dfdb9e762e354eeca4f7821b49d7`; installed FreeRADIUS adapter aggregate SHA-256 `3c45063efb451ff86dac23d695e9c57c307b86ef8054b790a759cbb63e80db32`. This is a new Ubuntu 24.04.5 guest installed with the candidate's documented installer in 39 s, not the prior rc3 guest with a swapped binary. FreeRADIUS 3.2.5, PostgreSQL 16.15, ArubaOS-CX Virtual 10.07.0004. All times below are UTC on 2026-11-10. Agent-operated lab measurements.

## Direct answers

- **Did the candidate change endpoint access? PASS.** UI Quarantine and Restore returned CoA-ACKs. The switch showed the requested local role; fresh corp TCP stopped under quarantine and returned under corp. Remediation returned HTTP 200 under quarantine. The endpoint stayed on VLAN 10 at DHCP address `10.70.10.50`; no EAP/RADIUS exchange accompanied a role CoA. The second pair after restart/repair is in `postrestart-capture2.tar.gz` and the matching probe logs.
- **Did a rule-only policy change change access? PASS.** UI preview predicted quarantine, UI published policy v3, and the role CoA applied it without a new EAP exchange. Corp TCP then failed and remediation returned 200. A separately recorded manual port cycle produced a fresh EAP-TLS/RADIUS decision for v3. UI restored v2; corp TCP returned, and a separate fresh login confirmed the v2 corp decision. See `packet-captures-continued.tar.gz` and `rule-only-*.jsonl`.
- **Did the full EVE restart recover automatically? FAIL (lab).** The NAC services, saved policy and binary did recover. Endpoint access needed four explicit lab repairs described below. Product Quarantine/Restore passed again afterward. This is not evidence of an rc4 product enforcement defect, and the full-restart acceptance row remains failed until the lab boots without these interventions.

## Matrix

| Test | Result | Observation and evidence |
|---|---|---|
| Exact candidate rebuild, clean install, installed pins | PASS | `install-report.txt`, `installed-hashes.txt`, `preflight.json`; 39 s install |
| Corp and remediation service health | PASS | Both returned HTTP 200 from their respective guests (`corp-service-raw.txt`, `remediation-service-raw.txt`) |
| UI switch onboarding, endpoint binding, fresh admission | PASS | FreeRADIUS-validated issuer+serial bound to a NAC endpoint; policy v2 `rc4-bound-corp`; Access-Accept role corp, switch Success, fresh corp HTTP 200 |
| Fresh unauthenticated and invalid certificate | PASS | Corp blocked before admission; untrusted certificate caused FreeRADIUS rejection, switch Fail and corp blocked (`packet-captures.tar.gz`) |
| UI preview, rule-only v3 publication, v2 restoration | PASS | Preview and actual v3 quarantine decision agreed; the rule-only change drove corp denial and remediation success; manual reauth separately verified RADIUS v3; v2 restoration gave corp HTTP 200 (`packet-captures-continued.tar.gz`, probe logs) |
| UI/API role CoA | PASS for observed UI path | Session-targeted Aruba-User-Role CoA ACK; role and endpoint TCP agree. No VLAN/IP transition. UI labels the result as ACK, not traffic proof. API negative cases were not independently run in this package |
| D1 real-systemd reload chaos C1-C6 | PASS | 20 PASS, 0 FAIL, including real SIGKILL of FreeRADIUS main PID and systemd restart; deliberate stop reported `no`, not false `yes` (`reload-chaos.log`, `systemd-crash-excerpt.txt`) |
| D2 connection wording | PASS | UI reported `unknown (accounting Start, no Stop)`, with Acct-Session-Id and age; switch observation `unavailable`, endpoint reachability `not_observed` |
| nacd decision outage and recovery | PASS | Existing authorized session kept corp HTTP 200; a new login during outage got Access-Reject/switch Fail/corp blocked; after nacd start and manual port cycle, fresh Access-Accept/switch Success/corp HTTP 200. No cached authorization on new login (`outage-capture.tar.gz`, `nac-outage.log`) |
| Full EVE restart, automatic endpoint recovery | FAIL (lab) | Saved NAC state and services survived, but virtual authenticator, DHCP, checksum offload and return route needed manual repair; details below (`postrestart-capture.tar.gz`, first probe logs) |
| Postrepair UI Quarantine/Restore | PASS | Two UI pairs ACKed; second pair has endpoint/control taps on the same EVE clock and continuous fresh TCP (`postrestart-capture2.tar.gz`, second probe logs) |
| Interactive password change on this clean rc4 guest | BLOCKED | Browser credential-change handoff was not performed on this guest. Earlier rc10 Account-page evidence does not qualify rc4 |
| Arista regression with this binary | NOT RUN | Separate isolated vEOS topology still needs candidate `dd2b6137…` wired into its RADIUS control node; prior vEOS results are historical, not an rc4 pass |
| Physical switch and Windows 10/11 | BLOCKED | No authorized physical Aruba/Arista switch or Windows 10/11 guest in this run |

## Outage and CoA clarification

At 07:22:40 `nacd` was stopped while FreeRADIUS remained running. The existing corp session still returned HTTP 200 at 07:22:45: authorization already installed in the switch persisted, not an NAC cache. A manually initiated new EAP-TLS login got FreeRADIUS Access-Reject at 07:23:12.976337; the switch showed Fail and corp TCP failed. The outage log marked this `outage-reject`, explicitly not a NAC policy decision. After `nacd` returned at 07:23:36, a **manual port cycle** led to a new Access-Accept at 07:23:59.817278, switch Success/corp, and endpoint HTTP 200 at 07:24:06. The product has no authorization cache for new logins.

The second postrestart UI Quarantine requested role `quarantine` with CoA ID `0x18` at 07:47:20.130736; the switch ACKed at 07:47:20.168604. The first subsequent fresh corp SYN at 07:47:20.197680 got no SYN/ACK; the preceding corp HTTP 200 ended at 07:47:19.700675. This bounds observed corp denial to **<0.50 s from request**, with the first blocked probe **0.067 s** after request. Remediation continued HTTP 200. The UI Restore requested role `corp` with ID `0x25` at 07:49:53.244821; ACK at 07:49:53.286117. Fresh corp SYN/SYN-ACK occurred at 07:49:53.854268/.855429, **0.611 s from request**. The switch then showed role corp. Endpoint DHCP address did not change. The probe guest clock differed from the EVE packet-capture clock by roughly one second, so packet timing uses only EVE-host taps. The role CoAs did not trigger EAP/RADIUS reauthentication. No manual endpoint renewal or reconnect occurred during this second pair.

Corp role did **not** block the remediation service; the probe shows remediation HTTP 200 in both roles. The demonstrated isolation claim is quarantine denying corp and same-subnet peer while allowing remediation. It is not a claim that corp cannot reach remediation.

## Full restart and manual intervention

The EVE restart runner stopped only nodes 61-65 in the pinned `Aruba-Role-ACL-1007.unl` topology, then restarted them. The separate rc4 guest was gracefully shut down and relaunched from its copy-on-write overlay. The candidate hash, nacd/FreeRADIUS/PostgreSQL services, policy v2, session record and switch saved 802.1X/RADIUS configuration survived.

1. Endpoint boot was delayed about 2 minutes by `systemd-networkd-wait-online`; it then had no IPv4 address. Starting `wpa_supplicant` produced no EAP exchange. A manual port shutdown/no shutdown and endpoint reauthenticate command did not repair it. Restarting the supplicant produced EAPOL Start but the virtual switch did not respond. Manually disabling and re-enabling the 802.1X authenticator under interface 1/1/1 produced EAP/RADIUS at 07:33:22 and a valid v2 corp decision `c7dbd70c-f575-48dc-87e4-d292cd75f082`. The EAPOL/control tap before that is in `postrestart-capture.tar.gz`.
2. The corp BusyBox `udhcpd` had been started as an ephemeral process, not a boot service. It was started manually. `networkctl renew ens3` was invoked manually (first try did not acquire); DHCP DORA around 07:37:04 assigned `10.70.10.50/24`, default route `10.70.10.1`, DNS `10.70.10.10`.
3. TX checksum offload reverted to on for endpoint, corp and remediation virtual NICs after reboot. Server taps showed SYN checksum mismatch. `ethtool -K ens3 tx off` was run manually on each affected guest. Corp HTTP 200 returned at 07:42:20.
4. First UI Quarantine at 07:43:05.954601 was ACKed and switched to role quarantine; corp was blocked but remediation also timed out. The remediation tap saw SYNs, and its guest had no route to `10.70.10.50`. A **manual** `ip route add 10.70.10.0/24 via 10.70.20.1 dev ens3` at 07:44:28 restored remediation HTTP 200 at 07:44:42. The first postrestart quarantine is a failed automatic-lab-recovery observation, not a passing transition. UI Restore at 07:45:17.276052 ACKed and corp HTTP 200 returned.

After all four repairs, a second UI Quarantine/Restore pair passed without endpoint intervention. The next lab revision must make DHCP, route and offload settings persistent, and prove that the virtual AOS-CX authenticator responds after reboot without an operator reset. Keep this isolated topology unchanged until that diagnosis is complete.

## Reproduce and inspect

Use only the authorized isolated EVE topology. The sanitized `Aruba-Role-ACL-1007.unl` export and relevant switch lines are here. The source runner `aruba_rc4_restart_eve_guest.py` verifies the topology SHA-256 and node identities before touching nodes 61-65; it does not restart unrelated VMs. The bounded probe fetcher is `aruba_fetch_rc4_probe_guest.py`. Raw capture tarballs contain endpoint/control PCAPs; use `tcpdump -tttt -nn -vv -r <pcap>` and filter CoA IDs, EAPOL and fresh TCP. Probe JSONL has UTC timestamps and independent corp/remediation/peer results. The D1 helper's real-systemd log is supplied in full. The guest commands used for failed recovery are above; rerunning them makes the run diagnostic, not automatic.

No keys, vendor images, shared secrets or browser credentials are included. Lab IPs, test identity and certificate serial are present for correlation. `sha256-manifest.json` checks the delivered files.
