"""Extract sanitized RADIUS, EAPOL and DHCP evidence from bounded lab pcaps."""
import argparse
from datetime import datetime, timezone
import ipaddress
import json
from pathlib import Path
import struct

def read_pcap(path):
    with path.open('rb') as stream:
        header=stream.read(24)
        magic=header[:4]
        endian,nanosecond={b'\xd4\xc3\xb2\xa1':('<',False),b'\xa1\xb2\xc3\xd4':('>',False),
                           b'\x4d\x3c\xb2\xa1':('<',True),b'\xa1\xb2\x3c\x4d':('>',True)}[magic]
        while packet_header:=stream.read(16):
            sec,fraction,length,_=struct.unpack(endian+'IIII',packet_header)
            data=stream.read(length)
            stamp=datetime.fromtimestamp(sec+fraction/(1e9 if nanosecond else 1e6),timezone.utc).isoformat(timespec='milliseconds')
            yield stamp,data

def ip(raw): return str(ipaddress.IPv4Address(raw))

def parse(path):
    result=[]
    for stamp,frame in read_pcap(path):
        if len(frame)<14: continue
        kind=int.from_bytes(frame[12:14],'big'); offset=14
        if kind==0x8100:
            if len(frame)<18: continue
            kind=int.from_bytes(frame[16:18],'big'); offset=18
        if kind==0x888e:
            if len(frame)>=offset+4:
                result.append({'at_utc':stamp,'protocol':'eapol','packet_type':frame[offset+1]})
            continue
        if kind!=0x0800 or len(frame)<offset+20: continue
        ihl=(frame[offset]&0xf)*4
        if frame[offset+9]!=17 or len(frame)<offset+ihl+8: continue
        source,dest=ip(frame[offset+12:offset+16]),ip(frame[offset+16:offset+20])
        udp=frame[offset+ihl:]
        src_port,dst_port,udp_length=struct.unpack('!HHH',udp[:6])
        body=udp[8:udp_length]
        if src_port in (1812,3799) or dst_port in (1812,3799):
            if len(body)<20: continue
            code=body[0]
            name={1:'Access-Request',2:'Access-Accept',3:'Access-Reject',
                  11:'Access-Challenge',43:'CoA-Request',44:'CoA-ACK',45:'CoA-NAK'}.get(code,str(code))
            if name=='Access-Challenge' or name=='Access-Request': continue
            item={'at_utc':stamp,'protocol':'radius','message':name,'source':source,'destination':dest,'id':body[1]}
            attrs=body[20:int.from_bytes(body[2:4],'big')]
            at=0
            while at+2<=len(attrs):
                typ,size=attrs[at],attrs[at+1]
                if size<2 or at+size>len(attrs): break
                value=attrs[at+2:at+size]
                if typ==25: item['class']=value.decode(errors='replace')
                if typ==81: item['vlan']=value.decode(errors='replace').lstrip('\x00')
                if typ==64 and len(value)>=4: item['tunnel_type']=int.from_bytes(value[-4:],'big')
                if typ==65 and len(value)>=4: item['tunnel_medium']=int.from_bytes(value[-4:],'big')
                if typ==101 and len(value)>=4: item['error_cause']=int.from_bytes(value[-4:],'big')
                at+=size
            result.append(item)
        elif src_port in (67,68) or dst_port in (67,68):
            if len(body)<240 or body[236:240]!=b'\x63\x82\x53\x63': continue
            item={'at_utc':stamp,'protocol':'dhcp','source':source,'destination':dest,
                  'ciaddr':ip(body[12:16]),'yiaddr':ip(body[16:20]),
                  'client_mac':':'.join(f'{x:02x}' for x in body[28:34])}
            opts=body[240:]; at=0
            while at<len(opts):
                typ=opts[at]; at+=1
                if typ==255: break
                if typ==0: continue
                if at>=len(opts):break
                size=opts[at];at+=1
                value=opts[at:at+size];at+=size
                if typ==53 and value: item['message']={1:'Discover',2:'Offer',3:'Request',5:'ACK',6:'NAK',7:'Release'}.get(value[0],str(value[0]))
                if typ==51 and len(value)==4:item['lease_seconds']=int.from_bytes(value,'big')
                if typ==58 and len(value)==4:item['t1_seconds']=int.from_bytes(value,'big')
                if typ==59 and len(value)==4:item['t2_seconds']=int.from_bytes(value,'big')
                if typ==54 and len(value)==4:item['server_id']=ip(value)
                if typ==50 and len(value)==4:item['requested_ip']=ip(value)
            if item.get('message') in ('Discover','Offer','Request','ACK','NAK'):
                result.append(item)
    return result

def main():
    cli=argparse.ArgumentParser()
    cli.add_argument('pcaps',nargs='+',type=Path)
    cli.add_argument('--out',type=Path,required=True)
    args=cli.parse_args()
    records=[]
    for path in args.pcaps:
        for item in parse(path): records.append({'capture':path.name,**item})
    records.sort(key=lambda item:item['at_utc'])
    args.out.write_text(json.dumps(records,indent=2)+'\n')
    print('RECORDS',len(records),'OUT',args.out)
    for item in records:
        if item['protocol']=='radius' or item.get('message')=='ACK':
            print(item)

if __name__=='__main__': main()
