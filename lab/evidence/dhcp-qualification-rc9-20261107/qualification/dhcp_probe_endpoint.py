"""Continuous fresh TCP, address, route and DNS observations on one DHCP NIC."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import select
import socket
import subprocess
import time

TARGETS = {'corp': ('10.70.10.10', 8080),
           'quarantine': ('10.70.20.10', 8080)}

def utc():
    return datetime.now(timezone.utc).isoformat(timespec='milliseconds')

def run(*args):
    return subprocess.run(args, capture_output=True, text=True, timeout=2).stdout.strip()

def state():
    addresses = json.loads(run('ip','-j','addr','show','dev','ens3'))
    ipv4 = sorted(item['local']+'/'+str(item['prefixlen']) for entry in addresses
                  for item in entry.get('addr_info',[]) if item['family']=='inet')
    routes = json.loads(run('ip','-j','-4','route','show'))
    route_summary = sorted(str(item.get('dst','default'))+' via '+str(item.get('gateway','on-link'))
                           for item in routes if item.get('dev')=='ens3')
    return {'addresses':ipv4,'routes':route_summary,
            'dns':run('resolvectl','dns','ens3'),
            'domains':run('resolvectl','domain','ens3'),
            'carrier':run('cat','/sys/class/net/ens3/carrier')}

def probe(segment):
    host, port = TARGETS[segment]
    start = time.monotonic()
    connection = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    connection.settimeout(0.8)
    try:
        connection.connect((host, port))
        return {'segment':segment,'connected':True,'source':connection.getsockname()[0],
                'elapsed_ms':round((time.monotonic()-start)*1000)}
    except OSError as exc:
        return {'segment':segment,'connected':False,'error':type(exc).__name__,
                'elapsed_ms':round((time.monotonic()-start)*1000)}
    finally:
        connection.close()

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--seconds',type=int,required=True)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--hold-corp',action='store_true')
    parser.add_argument('--release-marker',type=Path)
    args=parser.parse_args()
    if not 1 <= args.seconds <= 900 or args.output.exists():
        parser.error('invalid duration or existing output')
    if args.hold_corp and (not args.release_marker or args.release_marker.exists()):
        parser.error('held connection requires new release marker')
    interfaces={item.name for item in Path('/sys/class/net').iterdir()}
    if interfaces != {'ens3','lo'}:
        raise SystemExit('endpoint does not have exactly one data interface')
    held=None
    released=False
    last_state=None
    deadline=time.monotonic()+args.seconds
    with args.output.open('x') as output:
        def write(item):
            output.write(json.dumps({'at_utc':utc(),**item},sort_keys=True)+'\n')
            output.flush()
        write({'event':'start','duration_s':args.seconds,'interfaces':sorted(interfaces)})
        if args.hold_corp:
            try:
                held=socket.create_connection(TARGETS['corp'],timeout=2)
                held.settimeout(5)
                write({'event':'held_corp_connected','local':held.getsockname(),'peer':held.getpeername()})
            except OSError as exc:
                write({'event':'held_corp_failed','error':type(exc).__name__})
                held=None
        try:
            while time.monotonic()<deadline:
                current=state()
                if current!=last_state:
                    write({'event':'network_state',**current})
                    last_state=current
                if held and not released:
                    if args.release_marker and args.release_marker.exists():
                        released=True
                        try:
                            held.sendall(b'GET / HTTP/1.0\r\nHost: corp\r\n\r\n')
                            answer=held.recv(64)
                            write({'event':'held_corp_after_transition','http_reply':answer.startswith(b'HTTP/'),
                                   'reply_bytes':len(answer)})
                        except OSError as exc:
                            write({'event':'held_corp_after_transition','http_reply':False,
                                   'error':type(exc).__name__})
                    elif select.select([held],[],[],0)[0]:
                        try:
                            if not held.recv(1,socket.MSG_PEEK):
                                write({'event':'held_corp_closed_before_release'})
                                held.close(); held=None
                        except OSError as exc:
                            write({'event':'held_corp_error_before_release','error':type(exc).__name__})
                            held.close(); held=None
                for segment in TARGETS:
                    write({'event':'tcp_probe',**probe(segment)})
                time.sleep(0.25)
        finally:
            if held:
                held.close()
            write({'event':'end'})

if __name__=='__main__':
    main()
