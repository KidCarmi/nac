"""Count UDP echo requests/replies per role window on one EVE-host clock."""
from collections import Counter
from datetime import datetime, timezone
import ipaddress
from pathlib import Path
import struct
import sys

ROOT = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent


def packets(path):
    data = path.read_bytes()
    if data[:4] != bytes.fromhex("d4c3b2a1"):
        raise ValueError(f"unexpected pcap format: {path}")
    off = 24
    while off + 16 <= len(data):
        sec, usec, captured, _ = struct.unpack_from("<IIII", data, off)
        off += 16
        packet = data[off:off + captured]
        off += captured
        if len(packet) != captured:
            raise ValueError("truncated pcap record")
        yield sec + usec / 1e6, packet


def udp4(packet):
    if len(packet) < 42 or struct.unpack_from("!H", packet, 12)[0] != 0x0800:
        return None
    ip = 14
    ihl = (packet[ip] & 15) * 4
    if packet[ip + 9] != 17 or len(packet) < ip + ihl + 8:
        return None
    src = str(ipaddress.IPv4Address(packet[ip + 12:ip + 16]))
    dst = str(ipaddress.IPv4Address(packet[ip + 16:ip + 20]))
    sport, dport = struct.unpack_from("!HH", packet, ip + ihl)
    return src, dst, sport, dport, packet[ip + ihl + 8:]


acks = []
for when, packet in packets(ROOT / "control.pcap"):
    u = udp4(packet)
    if u and u[2] == 3799 and u[4][:1] == b"\x2c":
        acks.append(when)
if len(acks) != 2:
    raise ValueError(f"expected two CoA ACKs, got {len(acks)}")
print("quarantine_ack", datetime.fromtimestamp(acks[0], timezone.utc).isoformat())
print("restore_ack", datetime.fromtimestamp(acks[1], timezone.utc).isoformat())

for tap in ("endpoint", "corp", "remediation"):
    counts = Counter()
    for when, packet in packets(ROOT / f"{tap}.pcap"):
        u = udp4(packet)
        if not u or u[2] != 9099 and u[3] != 9099:
            continue
        src, dst, sport, dport, body = u
        phase = "before" if when < acks[0] else "quarantine" if when < acks[1] else "restore"
        target = "corp" if "10.70.10.10" in (src, dst) else "remediation" if "10.70.20.10" in (src, dst) else "other"
        direction = "request" if src == "10.70.10.50" else "reply" if dst == "10.70.10.50" else "other"
        if body.startswith(b"aruba-udp-probe:") or body.startswith(b"corp:aruba-udp-probe:") or body.startswith(b"remediation:aruba-udp-probe:"):
            counts[(phase, target, direction)] += 1
    print(tap, dict(sorted(("/".join(k), v) for k, v in counts.items())))
