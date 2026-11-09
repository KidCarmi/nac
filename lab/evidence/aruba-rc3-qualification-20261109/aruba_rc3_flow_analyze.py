"""Compare established TCP flow payloads with CoA ACKs on one EVE clock."""
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
import ipaddress
import struct
import sys

root = Path(sys.argv[1])

def packets(name):
    raw = (root / name).read_bytes()
    assert raw[:4] == bytes.fromhex("d4c3b2a1"), name
    pos = 24
    while pos + 16 <= len(raw):
        sec, usec, cap, _ = struct.unpack_from("<IIII", raw, pos)
        pos += 16
        frame = raw[pos:pos+cap]
        pos += cap
        if len(frame) != cap:
            raise ValueError("truncated pcap")
        yield sec + usec / 1e6, frame

def ip4(frame):
    if len(frame) < 34 or frame[12:14] != b"\x08\x00":
        return None
    ip = frame[14:]
    n = (ip[0] & 15) * 4
    return str(ipaddress.IPv4Address(ip[12:16])), str(ipaddress.IPv4Address(ip[16:20])), ip[9], ip[n:]

acks = []
for when, frame in packets("flow-control.pcap"):
    ip = ip4(frame)
    if not ip or ip[2] != 17 or len(ip[3]) < 9:
        continue
    udp = ip[3]
    src, dst = struct.unpack_from("!HH", udp)
    if src == 3799 and udp[8] == 44:
        acks.append(when)
assert len(acks) == 2, f"expected 2 CoA ACKs, found {len(acks)}"
print("CoA ACKs:", *(datetime.fromtimestamp(t, timezone.utc).isoformat() for t in acks))

counts = {"before_quarantine": Counter(), "quarantined": Counter(), "after_restore": Counter()}
first = {}
for when, frame in packets("flow-endpoint.pcap"):
    ip = ip4(frame)
    if not ip or ip[2] != 6 or len(ip[3]) < 20:
        continue
    src, dst, _, tcp = ip
    sport, dport = struct.unpack_from("!HH", tcp)
    if {sport, dport} != {43402, 8082}:
        continue
    offset = (tcp[12] >> 4) * 4
    flags = tcp[13]
    payload = tcp[offset:]
    phase = "before_quarantine" if when < acks[0] else "quarantined" if when < acks[1] else "after_restore"
    key = "server_payload" if src == "10.70.10.10" and payload else "client_payload" if src == "10.70.10.50" and payload else "other"
    counts[phase][key] += 1
    if flags & 4:
        counts[phase]["rst"] += 1
    if flags & 1:
        counts[phase]["fin"] += 1
    first.setdefault((phase,key), when)
for phase, c in counts.items():
    print(phase, dict(c))
    for key in ("client_payload", "server_payload"):
        t = first.get((phase,key))
        if t is not None:
            print(" ", key, "first", datetime.fromtimestamp(t, timezone.utc).isoformat(),
                  "after_quarantine_s", round(t-acks[0],3), "after_restore_s", round(t-acks[1],3))
