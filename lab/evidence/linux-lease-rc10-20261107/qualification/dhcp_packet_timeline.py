"""Print DHCPv4 lease and renewal events from a bounded Ethernet pcap."""

import argparse
import socket
import struct

from pcap_timeline import packets


KINDS = {1: 'Discover', 2: 'Offer', 3: 'Request', 4: 'Decline',
         5: 'ACK', 6: 'NAK', 7: 'Release', 8: 'Inform'}


def address(raw):
    value = socket.inet_ntoa(raw)
    return '' if value == '0.0.0.0' else value


def describe(frame):
    if len(frame) < 14:
        return None
    offset = 14
    ethertype = struct.unpack('!H', frame[12:14])[0]
    while ethertype in (0x8100, 0x88a8) and len(frame) >= offset + 4:
        ethertype = struct.unpack('!H', frame[offset + 2:offset + 4])[0]
        offset += 4
    if ethertype != 0x0800 or len(frame) < offset + 20:
        return None
    ip = frame[offset:]
    ihl = (ip[0] & 15) * 4
    if len(ip) < ihl + 8 or ip[9] != 17:
        return None
    src, dst = address(ip[12:16]) or '0.0.0.0', address(ip[16:20]) or '0.0.0.0'
    udp = ip[ihl:]
    sport, dport, length = struct.unpack('!HHH', udp[:6])
    if {sport, dport} != {67, 68}:
        return None
    body = udp[8:8 + length - 8]
    if len(body) < 240 or body[236:240] != b'\x63\x82\x53\x63':
        return None
    options = {}
    index = 240
    while index < len(body):
        kind = body[index]
        index += 1
        if kind == 255:
            break
        if kind == 0:
            continue
        if index >= len(body) or index + 1 + body[index] > len(body):
            break
        size = body[index]
        index += 1
        options[kind] = body[index:index + size]
        index += size
    msg = KINDS.get(options.get(53, b'\x00')[0], '?')
    fields = [f'{src}:{sport}>{dst}:{dport}', msg,
              'xid=' + hex(struct.unpack('!I', body[4:8])[0])]
    for name, raw in [('ciaddr', body[12:16]), ('yiaddr', body[16:20])]:
        if value := address(raw):
            fields.append(name + '=' + value)
    for name, kind in [('requested', 50), ('server', 54)]:
        if len(options.get(kind, b'')) == 4:
            fields.append(name + '=' + address(options[kind]))
    for name, kind in [('lease', 51), ('T1', 58), ('T2', 59)]:
        if len(options.get(kind, b'')) == 4:
            fields.append(name + '=' + str(struct.unpack('!I', options[kind])[0]))
    return ' '.join(fields)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('pcap')
    parser.add_argument('--from-time', default='')
    parser.add_argument('--to-time', default='')
    args = parser.parse_args()
    for when, frame in packets(args.pcap):
        if args.from_time and when < args.from_time:
            continue
        if args.to_time and when > args.to_time:
            continue
        if line := describe(frame):
            print(when, line)
