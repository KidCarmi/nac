# Bounded established-flow rerun

Start from the [authenticated reference lab](../aruba-1007-reference-20261109/rerun.md), with the endpoint in `corp` and the direct [CoA path](../aruba-1007-coa-20261109/rerun.md) working. Use fresh disposable credentials and the same isolated links.

1. On the corp guest start `python3 aruba_flow_server.py > server.jsonl 2>&1` and on the endpoint start `python3 aruba_flow_client.py > client.jsonl 2>&1`. The client opens **one** TCP connection to port 8082 and exchanges numbered messages for at most 95 s. Require several `echo_ok` records before changing the role.
2. Start `aruba_flow_capture_guest.py` on EVE to capture endpoint and corp data taps plus both CoA taps for at most 105 s or 1,000 packets per tap.
3. Send `Aruba-User-Role=quarantine` by the reference CoA sender. Do not reconnect or reauthenticate. Observe whether the original socket continues receiving echoes; also check fresh corp TCP access.
4. After at least 10 s, send `Aruba-User-Role=corp` by CoA. Probe a **fresh** TCP connection separately and continue watching the original socket. Compare its 4-tuple and sequence numbers on both taps; check whether queued data reaches the service after restoration.
5. Classify interruption and termination separately. A receive timeout while quarantined does not prove a TCP reset or destruction of the established connection.
