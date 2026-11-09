"""Compare CoA and TCP/DHCP packets using one EVE-host clock."""
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
import ipaddress
import struct
import sys

ROOT = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent
UI = len(sys.argv) > 2 and sys.argv[2] == 'ui'


def packets(path):
    raw = path.read_bytes()
    if raw[:4] != bytes.fromhex("d4c3b2a1"):
        raise ValueError(f"unexpected pcap magic: {path}")
    if struct.unpack_from("<I", raw, 20)[0] != 1:
        raise ValueError(f"not Ethernet: {path}")
    offset = 24
    while offset + 16 <= len(raw):
        sec, usec, captured, _ = struct.unpack_from("<IIII", raw, offset)
        offset += 16
        data = raw[offset:offset + captured]
        offset += captured
        if len(data) != captured:
            raise ValueError("truncated record")
        yield sec + usec / 1e6, data


def ip4(data):
    if len(data) < 34:
        return None
    ethertype = struct.unpack_from("!H", data, 12)[0]
    offset = 14
    while ethertype == 0x8100 and len(data) >= offset + 4:
        ethertype = struct.unpack_from("!H", data, offset + 2)[0]
        offset += 4
    if ethertype != 0x0800 or len(data) < offset + 20:
        return None
    ihl = (data[offset] & 15) * 4
    if len(data) < offset + ihl:
        return None
    src = str(ipaddress.IPv4Address(data[offset + 12:offset + 16]))
    dst = str(ipaddress.IPv4Address(data[offset + 16:offset + 20]))
    return src, dst, data[offset + 9], data[offset + ihl:]


def udp(payload):
    if len(payload) < 8:
        return None
    source, dest = struct.unpack_from("!HH", payload)
    return source, dest, payload[8:]


def tcp(payload):
    if len(payload) < 20:
        return None
    source, dest = struct.unpack_from("!HH", payload)
    offset = (payload[12] >> 4) * 4
    return source, dest, payload[13], payload[offset:]


def stamp(epoch):
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat()


for phase, endpoint_name, control_name in (
    (("ui-" if UI else "") + "quarantine", ("ui-" if UI else "coa-") + "quarantine-endpoint.pcap", ("ui-" if UI else "coa-") + "quarantine-control.pcap"),
    (("ui-" if UI else "") + "restore", ("ui-" if UI else "coa-") + "restore-endpoint.pcap", ("ui-" if UI else "coa-") + "restore-control.pcap"),
):
    if not (ROOT / endpoint_name).exists() or not (ROOT / control_name).exists():
        continue
    ack = None
    auth_packets = 0
    for when, data in packets(ROOT / control_name):
        p = ip4(data)
        if p and p[2] == 17:
            u = udp(p[3])
            if u and (u[0] == 1812 or u[1] == 1812):
                auth_packets += 1
            if u and u[0] == 3799 and u[2][:1] == b"\x2c":
                ack = when
    if ack is None:
        raise ValueError(f"no CoA ACK in {control_name}")
    successes = {"corp": [], "remediation": [], "peer": []}
    syns = {"corp": [], "remediation": [], "peer": []}
    dhcp = []
    eapol_packets = 0
    totals = Counter()
    for when, data in packets(ROOT / endpoint_name):
        if data[12:14] == b"\x88\x8e":
            eapol_packets += 1
        p = ip4(data)
        if not p:
            continue
        src, dst, proto, payload = p
        if proto == 17:
            u = udp(payload)
            if u and {u[0], u[1]} == {67, 68}:
                dhcp.append((when, src, dst, u[0], u[1]))
        elif proto == 6:
            t = tcp(payload)
            if not t:
                continue
            port_src, port_dst, flags, body = t
            for name, address in (("corp", "10.70.10.10"),
                                  ("remediation", "10.70.20.10"),
                                  ("peer", "10.70.10.11")):
                if src == "10.70.10.50" and dst == address and port_dst == 8080 and flags & 2 and not flags & 16:
                    syns[name].append(when)
                if src == address and dst == "10.70.10.50" and port_src == 8080 and b" 200 " in body[:32]:
                    successes[name].append(when)
                if src == address and dst == "10.70.10.50" and port_src == 8080:
                    totals[name] += 1
    print(phase, "coa_ack", stamp(ack), "radius_auth_packets", auth_packets,
          "endpoint_eapol_packets", eapol_packets)
    for name in successes:
        before = [v for v in successes[name] if v < ack]
        after = [v for v in successes[name] if v >= ack]
        first_syn = next((v for v in syns[name] if v >= ack), None)
        print(" ", name, "http200_before", len(before), "http200_after", len(after),
              "server_packets", totals[name],
              "first_syn_after_ack_s", round(first_syn - ack, 3) if first_syn else None,
              "first_http200_after_ack_s", round(after[0] - ack, 3) if after else None)
    for when, src, dst, sport, dport in dhcp:
        print(" ", "dhcp", stamp(when), f"{src}:{sport}", "->", f"{dst}:{dport}")
