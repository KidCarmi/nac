# Aruba CX 10.07 established TCP flow across role CoA — 2026-11-09

**Result: the quarantine role stopped data on an already established TCP connection, but did not terminate the socket.** After restoring `corp`, the **same TCP 4-tuple** resumed and data buffered during quarantine reached the server. This passes a narrow requirement to interrupt service access while quarantined; it **fails** a stronger requirement to destroy established connection state or prevent its queued data from arriving after restoration.

This was an isolated reference RADIUS test on ArubaOS-CX Virtual.10.07.0004, build SHA `64b88cbfc38a48a03d7b8b6229137e37f59c9310`, image SHA-256 `e60ef7d9679f664d1dafcaecb96f7443ec64e5b2abca26c41fdec26c57fafc25` (vendor image excluded, provenance unverified). It used the same EVE topology, single-NIC EAP-TLS endpoint, port `1/1/1`, VLAN 10 and static endpoint IP `10.70.10.50/24` as the [direct CoA test](../aruba-1007-coa-20261109/README.md). No KidCarmi/nac product or rc10 baseline was changed.

## Test and result matrix

| Gate | Expected | Observed | Status |
| --- | --- | --- | --- |
| Existing flow healthy | One endpoint-to-corp connection exchanges data before CoA | Client opened `10.70.10.50:37056 → 10.70.10.10:8082` and received matching echoes for messages 0–58 at ~0.5 s intervals. | **PASS** |
| Quarantine CoA | Active role changes without endpoint intervention | RADIUS CoA Request ID 80 at `17:47:36.371646` UTC; ACK at `17:47:36.413931`. No reauth/reconnect/port bounce. | **PASS** |
| Existing-flow data while quarantined | Corp stops receiving bytes | Client logged 36 consecutive one-second receive timeouts, beginning with message 59. Endpoint tap saw TCP retransmission from the same 4-tuple; corp tap had **zero** packets from that flow between the last pre-CoA exchange at `17:47:35.913330` and `17:48:30.293582`. | **PASS** for interruption |
| Restore CoA | Role returns to corp; new access works | CoA Request ID 193 at `17:48:05.466597`; ACK at `17:48:05.470745`. Switch role `corp`; a separate fresh corp HTTP probe returned 200. | **PASS** |
| Established-flow teardown | Existing socket cannot resume or deliver queued data after restore | The original `10.70.10.50:37056 → 10.70.10.10:8082` connection resumed at `17:48:30.293582`, **24.823 s after restore ACK**. The server echoed message 59 and a coalesced batch containing messages 60–94 that the client had attempted during quarantine. No new TCP handshake occurred for this flow. | **FAIL** for termination/queued-data purge |

All subsecond and 24.823 s packet timings use **EVE host timestamps** from [control](aruba-established-flow-20261109/server-tap.pcap), [endpoint](aruba-established-flow-20261109/endpoint-tap.pcap) and [corp](aruba-established-flow-20261109/corp-tap.pcap) taps. The endpoint and corp guest clocks are useful for sequence order but were not subtracted from EVE timestamps. The client logged `echo_mismatch` after restoration because old replies were queued and consumed in order by a simple one-request/one-response loop; this is evidence of data replay, not a new authorization failure. The server raised `ConnectionResetError` only after the 95 s client monitor exited with unread replies.

## Capability implication

For ADR-0009, do **not** mark a strong `cut_established_flows` requirement demonstrated by this role/CoA combination. The policy stopped packets during quarantine, but the transport state survived and later delivered queued data. The product may offer this strategy only when the required meaning is “no service data while restricted,” or when an additional tested teardown action provides the stronger guarantee. This test did not assess application-layer session invalidation, IPv6, DHCP, shared ports or NAC-driven role changes.

## Evidence and rerun

- [Client log](aruba-established-flow-20261109/client.jsonl), [server log](aruba-established-flow-20261109/server.jsonl), [endpoint tap](aruba-established-flow-20261109/endpoint-tap.pcap), [corp tap](aruba-established-flow-20261109/corp-tap.pcap), [CoA tap](aruba-established-flow-20261109/server-tap.pcap)
- [Packet observations](packet-observations.txt), [bounded archive](aruba-established-flow-20261109.tar.gz) SHA-256 `abaebc8b02a1378ea5612ef9d573cde1a39df850eb203e22ad51506225ce7aae`, [manifest](manifest.json)
- [Secret-free echo server](scripts/aruba_flow_server.py), [single-socket client](scripts/aruba_flow_client.py), [bounded capture helper](scripts/aruba_flow_capture_guest.py), [rerun steps](rerun.md)

The archive has no private key, shared secret or vendor image. Captures are bounded to 105 s or 1,000 packets per tap.
