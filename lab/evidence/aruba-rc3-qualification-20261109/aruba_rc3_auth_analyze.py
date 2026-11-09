"""Summarize captured EAPOL and final RADIUS authorization without secrets."""

from pathlib import Path
import struct
import sys


def pcap_packets(path):
    data = Path(path).read_bytes()
    if data[:4] != b'\xd4\xc3\xb2\xa1':
        raise ValueError(f'unsupported pcap byte order: {path}')
    offset = 24
    while offset + 16 <= len(data):
        sec, usec, length, _ = struct.unpack_from('<IIII', data, offset)
        offset += 16
        packet = data[offset:offset + length]
        offset += length
        yield f'{sec}.{usec:06d}', packet


def radius(packet):
    if len(packet) < 42 or packet[12:14] != b'\x08\x00':
        return None
    ip_start = 14
    ihl = (packet[ip_start] & 0x0f) * 4
    if packet[ip_start + 9] != 17:
        return None
    udp_start = ip_start + ihl
    src, dst = struct.unpack_from('!HH', packet, udp_start)
    if not ({src, dst} & {1812, 1813}):
        return None
    payload = packet[udp_start + 8:]
    if len(payload) < 20:
        return None
    code, identifier, length = struct.unpack_from('!BBH', payload)
    attrs = []
    pos = 20
    while pos + 2 <= min(length, len(payload)):
        kind, size = payload[pos], payload[pos + 1]
        if size < 2 or pos + size > len(payload):
            break
        value = payload[pos + 2:pos + size]
        if kind == 26 and len(value) >= 6:
            vendor = struct.unpack_from('!I', value)[0]
            vendor_type, vendor_size = value[4:6]
            body = value[6:6 + max(0, vendor_size - 2)]
            safe = body.decode('utf-8', 'replace') if all(32 <= n < 127 for n in body) else body.hex()
            attrs.append(f'VSA vendor={vendor} type={vendor_type} value={safe}')
        elif kind in (64, 65, 81):
            attrs.append(f'RADIUS type={kind} value={value.hex()}')
        pos += size
    return code, identifier, attrs


directory = Path(sys.argv[1])
for name in ('auth-endpoint-eapol.pcap', 'auth-switch-radius.pcap'):
    path = directory / name
    print(name)
    count = 0
    for timestamp, packet in pcap_packets(path):
        if name.endswith('eapol.pcap'):
            if len(packet) >= 18 and packet[12:14] == b'\x88\x8e':
                count += 1
                print(f'  {timestamp} EAPOL src={packet[6:12].hex(":")} dst={packet[:6].hex(":")} length={len(packet)}')
        else:
            result = radius(packet)
            if result:
                count += 1
                code, identifier, attrs = result
                print(f'  {timestamp} RADIUS code={code} id={identifier} ' + '; '.join(attrs))
    print(f'count={count}')
