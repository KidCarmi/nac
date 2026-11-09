# Aruba CX 10.07 peer-initiated traffic — 2026-11-09

**Finding: the tested quarantine role prevents a peer-initiated TCP session, but still delivers the peer's SYN to the endpoint.** The endpoint replied with SYN-ACK, which was visible at its EVE tap but never reached the peer tap. The peer's fresh HTTP probe timed out. This demonstrates an **ingress-only restriction at the endpoint's switch port**, not complete packet isolation in both directions.

The test used the same isolated EVE topology and ArubaOS-CX Virtual.10.07.0004 image as the [reference role run](../aruba-1007-reference-20261109/README.md) and [direct CoA run](../aruba-1007-coa-20261109/README.md). Exact switch build SHA: `64b88cbfc38a48a03d7b8b6229137e37f59c9310`; image SHA-256: `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25` (image excluded, vendor provenance unverified). This is a reference RADIUS platform test; KidCarmi/nac was not connected or changed.

## Sequence and result

| Step | Expected | Actual | Status |
| --- | --- | --- | --- |
| Service health in `corp` role | Peer `10.70.10.11` reaches endpoint `10.70.10.50:8081` | Endpoint's disposable Python HTTP service returned HTTP 200 from the peer. | **PASS** |
| Quarantine CoA | Active endpoint session changes role without VLAN/IP change | Session-targeted role CoA returned ACK; switch role became `quarantine`. | **PASS** |
| Peer TCP reachability | Fresh peer TCP cannot complete | Peer curl returned HTTP `000` after 2 s connection timeout. | **PASS** for TCP session denial |
| Peer packet delivery | A policy requiring bidirectional peer isolation would stop inbound peer packets | [Endpoint tap](aruba-peer-inbound-20261109/endpoint-tap.pcap) saw peer SYN at `17:43:32.360707` UTC and endpoint SYN-ACK at `17:43:32.360965`; [peer tap](aruba-peer-inbound-20261109/peer-tap.pcap) saw the SYN but no SYN-ACK. SYN and SYN-ACK retransmissions corroborate. | **FAIL** for complete bidirectional packet isolation |
| Restore | Peer reaches endpoint again | Corp role CoA ACK; switch role `corp`; peer curl HTTP 200. | **PASS** |

The [packet decode](packet-observations.txt) gives the decisive lines. A peer-originated packet reached the endpoint, even though a two-way TCP connection did not complete. This test did not use an application-level UDP receiver, so it does not classify which other inbound protocols can deliver payloads. It also does not test IPv6, established flows, a shared phone/PC port or a NAC-driven role.

## Evidence and rerun

The [archive](aruba-peer-inbound-20261109.tar.gz) SHA-256 is `b0b5f975e9141de4f0346f5cbec30c334cb15abba95943919e811625557abca3`. [Manifest](manifest.json) pins its files. The EVE taps were captured concurrently with a 35 s timeout and 200-packet cap; the [secret-free capture helper](aruba_peer_capture_guest.py) is included. No private key, shared secret or vendor image is included.

To repeat, start from a `dot1x Success` endpoint in the `corp` role and run `python3 -m http.server 8081 --bind 10.70.10.50` on its single data NIC. From the same-subnet peer run `curl --connect-timeout 2 --max-time 3 http://10.70.10.50:8081/` and require HTTP 200. Start captures on endpoint tap `vunl0_62_0` and peer tap `vunl0_66_0`. Send the [reference role CoA](../aruba-1007-coa-20261109/rerun.md) with `Aruba-User-Role=quarantine`, verify the switch role, then repeat the peer curl. Decode both taps for the peer SYN and endpoint SYN-ACK. Restore `corp` by CoA and require peer HTTP 200. Keep the switch port and endpoint address unchanged.
