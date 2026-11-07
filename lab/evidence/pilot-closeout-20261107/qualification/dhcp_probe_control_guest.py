"""Install, run and retrieve one-NIC DHCP probe through endpoint serial."""
import base64
import hashlib
import os
from pathlib import Path
import re
import select
import socket
import time

ACTION=os.environ.get('PROBE_ACTION','')
RUN=os.environ.get('PROBE_RUN_ID','')
if ACTION not in {'install','start','fetch','status','stop','release'}:
    raise SystemExit('invalid PROBE_ACTION')
if ACTION!='install' and not re.fullmatch(r'[A-Za-z0-9_-]{1,64}',RUN):
    raise SystemExit('invalid PROBE_RUN_ID')
DURATION=int(os.environ.get('PROBE_SECONDS','360'))
if not 1<=DURATION<=900: raise SystemExit('invalid duration')
MODE=os.environ.get('PROBE_MODE','fresh')
if MODE not in {'fresh','hold'}: raise SystemExit('invalid mode')
PASSWORD=Path('/root/nac-linux.rootpw').read_text().strip()

def read(sock,seconds):
    data=bytearray(); end=time.monotonic()+seconds
    while time.monotonic()<end:
        if select.select([sock],[],[],0.1)[0]:
            part=sock.recv(65536)
            if not part: break
            data.extend(part)
    return bytes(data)

with socket.create_connection(('127.0.0.1',32800),timeout=5) as sock:
    sock.sendall(b'\r\n')
    if b'login:' in read(sock,0.8):
        sock.sendall(b'root\r\n'); read(sock,0.4)
        sock.sendall(PASSWORD.encode()+b'\r\n'); read(sock,1.2)
    sock.sendall(b'stty -echo\r\n'); read(sock,0.3)
    try:
        prefix=f'/root/nac-dhcp-probe-{RUN}'
        if ACTION=='install':
            data=Path('/root/nac-qualification/dhcp_probe_endpoint_bounded.py').read_bytes()
            digest=hashlib.sha256(data).hexdigest()
            encoded=base64.b64encode(data)
            sock.sendall(b"base64 -d > /root/nac-dhcp-probe-bounded.py <<'NAC_EOF'\r\n"+encoded+b'\r\nNAC_EOF\r\nsha256sum /root/nac-dhcp-probe-bounded.py\r\n')
            if digest not in read(sock,3).decode(errors='replace'):
                raise RuntimeError('probe upload hash mismatch')
            print('PROBE_INSTALLED_SHA256',digest)
        elif ACTION=='start':
            flags=f'--hold-corp --release-marker {prefix}.release' if MODE=='hold' else ''
            unit=f'nac-dhcp-probe-{RUN}'
            command=(f'if test -e {prefix}.jsonl; then echo ALREADY_EXISTS; '
                     f'else systemd-run --collect --unit={unit} -- /usr/bin/python3 '
                     f'/root/nac-dhcp-probe-bounded.py --seconds {DURATION} '
                     f'--output {prefix}.jsonl {flags} >/dev/null && '
                     f'sleep 1 && test -s {prefix}.jsonl && echo NAC_PROBE_STARTED; fi\r\n')
            sock.sendall(command.encode())
            # The marker is printed only after systemd-run starts the probe and
            # the endpoint has flushed its first JSON record to disk.
            response=read(sock,10)
            if b'NAC_PROBE_STARTED' not in response:
                raise RuntimeError('probe start was not confirmed: '+repr(response[-500:]))
            print('PROBE_STARTED',RUN,DURATION,MODE)
        elif ACTION=='release':
            sock.sendall(f'touch {prefix}.release; echo NAC_HOLD_RELEASED\r\n'.encode())
            if b'NAC_HOLD_RELEASED' not in read(sock,1): raise RuntimeError('release failed')
            print('HOLD_RELEASED',RUN)
        elif ACTION=='status':
            sock.sendall(f'tail -n 3 {prefix}.jsonl; cat {prefix}.log\r\n'.encode())
            print(read(sock,2).decode(errors='replace')[-2500:])
        elif ACTION=='stop':
            sock.sendall(f'systemctl stop nac-dhcp-probe-{RUN} 2>/dev/null || true\r\n'.encode())
            read(sock,1); print('PROBE_STOP_REQUESTED',RUN)
        elif ACTION=='fetch':
            sock.sendall(f'echo NAC_FETCH_START; base64 -w 76 {prefix}.jsonl; echo NAC_FETCH_END\r\n'.encode())
            data=bytearray(); end=time.monotonic()+45
            while time.monotonic()<end:
                data.extend(read(sock,0.2))
                if b'NAC_FETCH_END' in data: break
            lines=data.replace(b'\r',b'').split(b'\n')
            start=next(i for i,line in enumerate(lines) if line.endswith(b'NAC_FETCH_START'))
            finish=next(i for i,line in enumerate(lines[start+1:],start+1) if line.endswith(b'NAC_FETCH_END'))
            encoded=b''.join(line for line in lines[start+1:finish] if re.fullmatch(rb'[A-Za-z0-9+/=]+',line))
            content=base64.b64decode(encoded,validate=True)
            target=Path('/root/nac-qualification')/f'dhcp-probe-{RUN}.jsonl'
            target.write_bytes(content)
            print('PROBE_FETCHED',target,'sha256',hashlib.sha256(content).hexdigest(),'bytes',len(content))
    finally:
        sock.sendall(b'stty echo\r\n'); read(sock,0.2)
