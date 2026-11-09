# Aruba role policy: endpoint-initiated IPv4 UDP reference

**PASS for the tested endpoint-initiated IPv4 UDP datagrams.** This is a
fixed-policy Aruba/FreeRADIUS reference run with direct role CoA. It is **not
a KidCarmi/nac product pass**, nor proof of full bidirectional packet
isolation. The endpoint kept its ordinary DHCP address throughout.

## Setup

- 2026-11-09, 18:29–18:31 UTC; EVE-NG VM 102, isolated
  `Aruba-Role-ACL-1007.unl` topology.
- ArubaOS-CX `Virtual.10.07.0004`, build SHA
  `64b88cbfc38a48a03d7b8b6229137e37f59c9310`; port `1/1/1`,
  `wired-endpoint-01`, DHCP address `10.70.10.50` on one data NIC.
- `corp` and `quarantine` roles share VLAN 10. The quarantine role uses
  `q-policy`: permit IPv4 traffic to `10.70.20.10`, then drop other IPv4
  traffic. Sanitized role config and the topology export are in the earlier
  [reference package](../aruba-1007-reference-20261109/README.md).
- Disposable Python UDP echo services listened on port 9099 at corp
  `10.70.10.10` and remediation `10.70.20.10`. Each replied with its own
  name and the probe nonce. The probes accepted a response only from the
  intended IP with the exact nonce. Both services returned valid echoes from
  the corp service, remediation service and endpoint before the role change.
  Their after-restore replies are also visible in the captures. The
  temporary services were stopped after the run.
- FreeRADIUS 3.0.26 sent session-targeted role CoA using the reference
  `aruba_coa_send_control.py`; no NAC decision service participated.

## Evidence matrix

The four PCAPs share the EVE host clock. `analyze_aruba_udp_pcap.py .`
extracts CoA ACKs and request/reply counts by role window.

| Phase | Expected | Observed | Status |
|---|---|---|---|
| Preflight, corp | Both UDP services answer independently | Both services returned nonce-matched echoes from three sources; endpoint reached both | PASS |
| Quarantine | Corp UDP denied; remediation UDP permitted | CoA-ACK ID 75 at **18:30:04.731672 UTC**. Switch showed `quarantine`. Endpoint sent 3 corp datagrams; corp service tap saw **0**. Endpoint sent 3 remediation datagrams; remediation tap saw 3 requests and 3 replies, and endpoint saw all 3 replies. Endpoint TCP control: corp timed out, remediation HTTP 200 | PASS |
| Restore | Corp UDP works again | CoA-ACK ID 116 at **18:31:09.713927 UTC**. Corp service tap saw 3 endpoint requests and 3 replies; endpoint saw all 3. Remediation still returned 3/3. Switch was returned to `corp` and the same DHCP address remained | PASS |
| NAC product action | API/console-driven policy, identity-scoped override and failure handling | Not involved | BLOCKED pending pinned candidate |

The quarantined endpoint's corp datagrams were visible on its own tap but
absent at the corp service tap. This localizes the denial to the switch path
for these endpoint-initiated packets; it does not establish what happens to
peer-initiated UDP, IPv6 UDP, routed ingress, or already open UDP application
state. No service outage was used to infer isolation.

## Rerun

Use the isolated topology and saved role config from the linked reference
package. Copy `aruba_udp_echo.py` to the corp and remediation guests, then
start each as a bounded service with `python3 aruba_udp_echo.py corp` and
`python3 aruba_udp_echo.py remediation`. Copy `aruba_udp_probe.py` to the
endpoint and both service guests. Verify `echo_ok` for both targets from
each guest before changing the role.

Capture UDP/9099 on endpoint tap `vunl0_62_0`, corp tap `vunl0_63_0`, and
remediation tap `vunl0_64_0`, plus UDP/3799 on switch control tap
`vunl0_61_0`. Use `timeout` and `tcpdump -c` to bound every capture. Send
`python3 aruba_coa_send_control.py quarantine` from the FreeRADIUS control
guest, run at least three new-nonce probes from the endpoint, then send
`python3 aruba_coa_send_control.py corp` and repeat. The CoA helper reads its
disposable secret from a protected file that is not included. Run the
analyzer on the completed PCAPs; it requires only the Python standard
library.

## Files

- `endpoint.pcap`, `corp.pcap`, `remediation.pcap`, `control.pcap`: bounded
  packet captures. The control cap has CoA request/ACK IDs 75 and 116.
- `aruba_udp_echo.py`, `aruba_udp_probe.py`: bounded service and nonce probes.
- `analyze_aruba_udp_pcap.py`: reproducible PCAP count analysis.
- `sha256-manifest.json`: hashes and sizes.

No vendor image, private key, admin credential, shared secret or service
token is included.
