"""Read lab Ethernet pcaps into a concise EAPOL/RADIUS timeline without secrets."""
import argparse
from datetime import datetime, timezone
from pathlib import Path
import socket
import struct

CODES = {1:"Access-Request",2:"Access-Accept",3:"Access-Reject",11:"Access-Challenge",40:"Disconnect-Request",41:"Disconnect-ACK",42:"Disconnect-NAK",43:"CoA-Request",44:"CoA-ACK",45:"CoA-NAK"}
ATTRS = {1:"User-Name",4:"NAS-IP-Address",30:"Called-Station-Id",31:"Calling-Station-Id",44:"Acct-Session-Id",64:"Tunnel-Type",65:"Tunnel-Medium-Type",79:"EAP-Message",81:"Tunnel-Private-Group-Id",87:"NAS-Port-Id",101:"Error-Cause"}
EAP_CODES = {1:"Request",2:"Response",3:"Success",4:"Failure"}

def mac(data):
    return ':'.join(f'{byte:02x}' for byte in data)

def attributes(data):
    out = {}
    eap = bytearray()
    i = 0
    while i + 2 <= len(data):
        kind, length = data[i], data[i+1]
        if length < 2 or i + length > len(data):
            break
        raw = data[i+2:i+length]
        key = ATTRS.get(kind)
        if key:
            if kind == 4 and len(raw) == 4:
                value = socket.inet_ntoa(raw)
            elif kind in (64,65,101) and len(raw) == 4:
                value = str(struct.unpack('!I', raw)[0])
            elif kind == 79:
                eap.extend(raw)
                value = ''
            else:
                value = raw.decode('ascii','replace')
            out[key] = value
        i += length
    if len(eap) >= 4:
        value = EAP_CODES.get(eap[0],str(eap[0])) + ':' + str(eap[1])
        if len(eap) > 4 and eap[0] in (1,2):
            value += ':type=' + str(eap[4])
        out['EAP-Message'] = value
    return out

def packets(path):
    with Path(path).open('rb') as stream:
        header = stream.read(24)
        if header[:4] == b'\xd4\xc3\xb2\xa1':
            fmt = '<'
        elif header[:4] == b'\xa1\xb2\xc3\xd4':
            fmt = '>'
        else:
            raise ValueError('not a microsecond pcap')
        if struct.unpack(fmt+'I', header[20:24])[0] != 1:
            raise ValueError('expected Ethernet link type')
        while meta := stream.read(16):
            if len(meta) != 16:
                break
            sec, micros, incl, _orig = struct.unpack(fmt+'IIII',meta)
            frame = stream.read(incl)
            if len(frame) != incl:
                break
            yield datetime.fromtimestamp(sec+micros/1e6, timezone.utc).isoformat(timespec='milliseconds'),frame

def describe(frame):
    if len(frame) < 14:
        return None
    dest, src, ethertype = frame[:6],frame[6:12],struct.unpack('!H',frame[12:14])[0]
    offset = 14
    while ethertype in (0x8100,0x88a8) and len(frame) >= offset+4:
        ethertype = struct.unpack('!H',frame[offset+2:offset+4])[0]
        offset += 4
    if ethertype == 0x888e and len(frame) >= offset+4:
        typ = frame[offset+1]
        eapol_length = struct.unpack('!H',frame[offset+2:offset+4])[0]
        body = frame[offset+4:offset+4+eapol_length]
        label = {0:'EAP-Packet',1:'Start',2:'Logoff',3:'Key'}.get(typ,str(typ))
        if typ == 0 and len(body) >= 4:
            label += ' ' + EAP_CODES.get(body[0],str(body[0])) + ' id=' + str(body[1])
            if len(body) > 4:
                label += ' type=' + str(body[4])
        return f'EAPOL {mac(src)}>{mac(dest)} {label}'
    if ethertype != 0x0800 or len(frame) < offset+20:
        return None
    ip = frame[offset:]
    ihl = (ip[0]&15)*4
    if ip[9] != 17 or len(ip) < ihl+8:
        return None
    udp = ip[ihl:]
    sport,dport,ulen = struct.unpack('!HHH',udp[:6])
    if sport not in (1812,1813,3799) and dport not in (1812,1813,3799):
        return None
    data = udp[8:8+ulen-8]
    if len(data) < 20:
        return None
    code,ident,length = data[0],data[1],struct.unpack('!H',data[2:4])[0]
    attrs = attributes(data[20:length])
    picked = ' '.join(f'{key}={attrs[key]}' for key in ('User-Name','Calling-Station-Id','NAS-IP-Address','NAS-Port-Id','Tunnel-Type','Tunnel-Medium-Type','Tunnel-Private-Group-Id','Error-Cause','EAP-Message') if key in attrs)
    return f'RADIUS {socket.inet_ntoa(ip[12:16])}:{sport}>{socket.inet_ntoa(ip[16:20])}:{dport} {CODES.get(code,str(code))} id={ident} {picked}'

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('pcap')
    parser.add_argument('--from-time',default='')
    parser.add_argument('--to-time',default='')
    parser.add_argument('--contains',default='')
    args = parser.parse_args()
    for when, frame in packets(args.pcap):
        if args.from_time and when < args.from_time:
            continue
        if args.to_time and when > args.to_time:
            continue
        line = describe(frame)
        if line and (not args.contains or args.contains in line):
            print(when,line)
