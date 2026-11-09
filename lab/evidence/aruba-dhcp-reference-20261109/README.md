# Aruba role CoA with one DHCP endpoint: platform reference

**Platform result: PASS for the measured corp → quarantine → corp reference
transition and DHCP renewal. Product result: BLOCKED until OPUS pins and
installs the NAC-driven Aruba candidate.** This run used direct `radclient`
CoA, not the NAC API or console. It extends the earlier static-address Aruba
[reference package](../aruba-1007-reference-20261109/README.md) and
[direct-CoA package](../aruba-1007-coa-20261109/README.md).

## Exact setup

| Component | Version/configuration |
|---|---|
| EVE | VM 102 on Proxmox; EVE-NG 6.2.0-4; isolated `Aruba-Role-ACL-1007.unl` topology |
| Switch | ArubaOS-CX `Virtual.10.07.0004`, build SHA `64b88cbfc38a48a03d7b8b6229137e37f59c9310`; image SHA-256 `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25` (third-party provenance not verified) |
| Access | Port `1/1/1`, 802.1X identity `wired-endpoint-01`, MAC `00:50:00:00:3e:00`; corp role and quarantine role both on VLAN 10; quarantine local `q-policy` permits remediation then restricts corp/peer traffic |
| RADIUS | FreeRADIUS 3.0.26; fixed EAP-TLS authorization and direct role CoA; no NAC decision service in this run |
| Endpoint | Ubuntu 22.04.5, `wpa_supplicant` wired EAP-TLS, only `ens3` data NIC, ordinary `systemd-networkd` DHCP |
| DHCP | BusyBox `udhcpd` 1.30.1 on isolated corp service `10.70.10.10`; single-address scope `10.70.10.50`; lease 120 s, T1 60 s, T2 105 s; router `10.70.10.1`, DNS option `10.70.10.10` |
| Services | Corp `10.70.10.10:8080`, remediation `10.70.20.10:8080`, peer `10.70.10.11:8080`; both principal services answered HTTP 200 locally before the run and from the endpoint while in corp |

The endpoint's original static netplan file was saved as
`/etc/netplan/*.yaml.pre-aruba-dhcp`. The DHCP configuration has mode 0600.
The corp DHCP server runs as transient `aruba-lab-dhcp.service`. One manual
`networkctl renew ens3` at 18:16:04 UTC established a fresh 120-second lease
**before** the quarantine action. There was no endpoint renewal, reconnect,
port bounce or operator-triggered reauthentication after either CoA.

## Evidence matrix

Times below come from the EVE-host packet captures, so the CoA and TCP offsets
share one clock. Guest console clocks differed by about 1–2 seconds and are
not used to calculate these offsets. TCP probes opened fresh connections;
their JSONL and the PCAPs are both included.

| Observation | Expected | Actual | Status |
|---|---|---|---|
| Preflight | Corp and remediation services healthy | Both returned HTTP 200 locally; endpoint in corp reached both | PASS |
| DHCP before CoA | One data NIC with lease, route and DNS | `ens3` got `10.70.10.50/24`, default route via `10.70.10.1`, DNS option `10.70.10.10`; 120/60/105 s lease/T1/T2 | PASS |
| Quarantine CoA | Active role changes, corp denied, remediation reachable, same address/VLAN | CoA-ACK ID 98 at **18:16:21.479854 UTC**; switch showed `quarantine`; first post-ACK corp SYN at +0.797 s had no HTTP 200, with 0 corp HTTP 200 in the subsequent capture; remediation HTTP 200 continued. Endpoint kept `10.70.10.50` | PASS |
| DHCP under quarantine | Lease remains usable without endpoint action | T1 unicast request at 18:17:06.512662 had no reply in the endpoint capture. T2 broadcast request at 18:17:50.651159 got a server reply at 18:17:50.652738; server journal logged ACK. The endpoint still held `10.70.10.50` after the original expiry and was still serving remediation probes | PASS for this client/server/role |
| Restore CoA | Corp reachability returns without new address | CoA-ACK ID 149 at **18:20:23.567176 UTC**; switch showed `corp`; first post-ACK corp SYN +0.529 s, HTTP 200 +0.533 s. Same DHCP address, route and DNS remained | PASS |
| Reauthentication/link behavior | Role CoA without EAPOL or new Access-Request | 0 endpoint EAPOL frames and 0 RADIUS authentication packets in both capture windows. Endpoint interface stayed UP in sampled states | PASS for captured windows |
| NAC-driven workflow | Product decision, override persistence, API/UI action, failure semantics | Not connected in this run | BLOCKED |

The quarantine cap used a 160-byte snap length, so its DHCP option bytes were
truncated. The 120/60/105 values come from the endpoint lease record, and the
T2 ACK is corroborated by the server journal and the address remaining usable.
The restore cap used full snap length. These packets establish observed DHCP
renewal; they do not prove every DHCP client or lease duration will behave the
same way. The corp access result covers fresh TCP connections, not full
bidirectional packet isolation or transport teardown.

## Rerun

Use the isolated topology and sanitized role configuration from the linked
reference package. On the corp service, create a `udhcpd` configuration with
the single-address scope and options listed above, including **`opt lease
120`** (plain `lease 120` leaves the default 10-day lease), and start it as a
bounded transient service. On the endpoint, save the static netplan file, set
`dhcp4: true` on `ens3`, and apply it. Confirm the 120/60/105 lease, route and
DNS before testing. Verify both HTTP services locally and from the corp role.

Use `aruba_probe_endpoint.py` on the endpoint to open repeated fresh TCP
connections to the three services. The reference CoA helper is
`aruba_coa_send_control.py`; it reads the disposable shared secret from a
protected local file, which is not included. Capture endpoint tap
`vunl0_62_0` for EAPOL, DHCP and TCP, and control tap `vunl0_61_0` for UDP
1812/3799 with bounded `tcpdump` durations and packet counts. Send
`quarantine`, observe through at least one T2 rebind, then send `corp` with
the probe still running. Use `analyze_aruba_dhcp_pcap.py .` to calculate CoA
and TCP offsets from the common EVE-host clock.

The endpoint is left on DHCP with corp role; the transient DHCP server is
running. A component restart requires starting the DHCP service again. The
saved static netplan file permits rollback. No Proxmox bridge, unrelated VM,
rc10 overlay or vendor image was changed.

## Files

- `control-tap.pcap`, `control-restore.pcap`: CoA request/ACK and absence of
  new RADIUS authentication in the captured windows.
- `endpoint-tap.pcap`, `endpoint-restore.pcap`: fresh TCP, DHCP and EAPOL.
- `endpoint-probes-quarantine.jsonl`, `endpoint-probes-restore.jsonl`: endpoint
  TCP outcomes; guest timestamps are for ordering, not CoA offset arithmetic.
- `analyze_aruba_dhcp_pcap.py`: portable, dependency-free PCAP timing summary.
- `aruba_probe_endpoint.py`, `aruba_coa_send_control.py`: reference runners.
- `sha256-manifest.json`: integrity and file sizes.

The package contains no vendor image, private key, admin credential, RADIUS
shared secret or service token.
