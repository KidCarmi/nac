"""Print only nonsecret tunnel attribute TLVs from captured CoA-Requests."""
import argparse
import struct
from pcap_timeline import packets

parser = argparse.ArgumentParser()
parser.add_argument('pcap')
args = parser.parse_args()
for at, frame in packets(args.pcap):
    if len(frame) < 42 or frame[12:14] != b'\x08\x00':
        continue
    ip = frame[14:]
    if ip[9] != 17:
        continue
    ihl = (ip[0] & 15) * 4
    udp = ip[ihl:]
    if len(udp) < 28:
        continue
    sport, dport, length = struct.unpack('!HHH', udp[:6])
    radius = udp[8:8+length-8]
    if dport != 3799 or radius[0] != 43:
        continue
    attrs = []
    data = radius[20:struct.unpack('!H', radius[2:4])[0]]
    i = 0
    while i + 2 <= len(data):
        typ, size = data[i], data[i+1]
        if size < 2 or i + size > len(data):
            break
        if typ in (64, 65, 81):
            attrs.append(f'{typ}:{data[i:i+size].hex()}')
        i += size
    print(at, 'id', radius[1], 'tunnel_tlvs', ','.join(attrs) or 'none')
