"""Replay one unique old Accounting Stop after checking duplicate capture frames."""
from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import socket
import struct

PCAP = Path('/root/nac-qualification/rc6-engine-after-manual-restart-20261106/radius.pcap')
TARGET_ID = b'1x00000007'
BRIDGE = 'vnet0_34'
action = os.environ.get('REPLAY_ACTION', '')
if action not in {'inspect', 'send'}:
    raise SystemExit('invalid REPLAY_ACTION')
if os.popen('sysctl -n net.ipv4.ip_forward').read().strip() != '0':
    raise SystemExit('EVE forwarding enabled')
data = PCAP.read_bytes()
if data[:4] == b'\xd4\xc3\xb2\xa1':
    endian = '<'
elif data[:4] == b'\xa1\xb2\xc3\xd4':
    endian = '>'
else:
    raise SystemExit('unexpected pcap format')
if struct.unpack_from(endian+'I', data, 20)[0] != 1:
    raise SystemExit('pcap is not Ethernet')
offset = 24
matches = []
while offset + 16 <= len(data):
    sec, usec, size, _ = struct.unpack_from(endian+'IIII', data, offset)
    offset += 16
    if offset + size > len(data):
        break
    frame = data[offset:offset+size]
    offset += size
    if len(frame) < 42:
        continue
    pos = 12
    ethertype = struct.unpack_from('!H', frame, pos)[0]
    pos = 14
    if ethertype == 0x8100:
        ethertype = struct.unpack_from('!H', frame, 16)[0]
        pos = 18
    if ethertype != 0x800 or frame[pos] >> 4 != 4:
        continue
    ihl = (frame[pos] & 15) * 4
    if frame[pos+9] != 17:
        continue
    if (frame[pos+12:pos+16] != socket.inet_aton('10.70.99.1') or
            frame[pos+16:pos+20] != socket.inet_aton('10.70.99.2')):
        continue
    udp = pos + ihl
    _, dport, length = struct.unpack_from('!HHH', frame, udp)
    if dport != 1813:
        continue
    radius = frame[udp+8:udp+length]
    if len(radius) < 20 or radius[0] != 4:
        continue
    attrs = {}
    i = 20
    while i + 2 <= len(radius):
        kind, sz = radius[i], radius[i+1]
        if sz < 2 or i + sz > len(radius):
            break
        attrs[kind] = radius[i+2:i+sz]
        i += sz
    if attrs.get(40) == b'\x00\x00\x00\x02' and attrs.get(44) == TARGET_ID:
        matches.append((sec, usec, radius, attrs, frame))
unique = {hashlib.sha256(item[4]).digest() for item in matches}
if not matches or len(unique) != 1:
    raise SystemExit(f'expected one unique old Stop, found {len(matches)} frames/{len(unique)} unique')
sec, usec, radius, attrs, frame = matches[0]
print('CAPTURED_DUPLICATES', len(matches))
print('OLD_STOP_AT_UTC', datetime.fromtimestamp(sec+usec/1e6, timezone.utc).isoformat())
print('OLD_ACCT_SESSION_ID', attrs[44].decode())
print('OLD_CLASS', attrs.get(25,b'').decode())
print('PAYLOAD_SHA256', hashlib.sha256(radius).hexdigest())
if action == 'send':
    with socket.socket(socket.AF_PACKET, socket.SOCK_RAW) as sock:
        sock.bind((BRIDGE, 0))
        n = sock.send(frame)
    if n != len(frame):
        raise RuntimeError('short send')
    print('REPLAY_SENT_AT_UTC', datetime.now(timezone.utc).isoformat(timespec='milliseconds'))
    print('REPLAY_BYTES', n)
