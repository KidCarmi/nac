"""Install/start/fetch continuous endpoint probes through its EVE serial port.

Run inside EVE via eve_guest.py. PROBE_ACTION is install, start, status, fetch
or stop; PROBE_RUN_ID is a safe basename. No endpoint management NIC is used.
"""
import base64
import hashlib
import os
from pathlib import Path
import re
import select
import socket
import time

action = os.environ.get('PROBE_ACTION', '')
run_id = os.environ.get('PROBE_RUN_ID', '')
if action not in {'install', 'start', 'status', 'fetch', 'stop'}:
    raise SystemExit('invalid PROBE_ACTION')
if action != 'install' and not re.fullmatch(r'[A-Za-z0-9_-]{1,64}', run_id):
    raise SystemExit('invalid PROBE_RUN_ID')
duration = int(os.environ.get('PROBE_SECONDS', '90'))
if not 1 <= duration <= 300:
    raise SystemExit('invalid PROBE_SECONDS')
password = Path('/root/nac-linux.rootpw').read_text().strip()

def read(sock, seconds):
    data = bytearray()
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if select.select([sock], [], [], 0.1)[0]:
            part = sock.recv(65536)
            if not part:
                break
            data.extend(part)
    return bytes(data)

with socket.create_connection(('127.0.0.1', 32770), timeout=5) as sock:
    sock.sendall(b'\r\n')
    greeting = read(sock, 0.8)
    if b'login:' in greeting:
        sock.sendall(b'root\r\n')
        read(sock, 0.4)
        sock.sendall(password.encode() + b'\r\n')
        read(sock, 1.2)
    sock.sendall(b'stty -echo\r\n')
    read(sock, 0.3)
    try:
        if action == 'install':
            source = Path('/root/nac-qualification/probe_endpoint.py')
            data = source.read_bytes()
            digest = hashlib.sha256(data).hexdigest()
            encoded = base64.b64encode(data)
            command = b"base64 -d > /root/nac-probe.py <<'NAC_PROBE_EOF'\r\n" + encoded + b'\r\nNAC_PROBE_EOF\r\nsha256sum /root/nac-probe.py\r\n'
            sock.sendall(command)
            result = read(sock, 3).decode('utf-8', 'replace')
            if digest not in result:
                raise RuntimeError('endpoint probe upload hash mismatch')
            print('PROBE_INSTALLED_SHA256', digest)
        elif action == 'start':
            output = '/root/nac-probe-' + run_id + '.jsonl'
            command = f'if test -e {output}; then echo NAC_PROBE_ALREADY_EXISTS; else nohup python3 /root/nac-probe.py --seconds {duration} --output {output} > /root/nac-probe-{run_id}.log 2>&1 </dev/null & echo $! > /root/nac-probe-{run_id}.pid; echo NAC_PROBE_STARTED; fi\r\n'
            sock.sendall(command.encode())
            result = read(sock, 2).decode('utf-8', 'replace')
            if 'NAC_PROBE_STARTED' not in result:
                raise RuntimeError('probe start was not acknowledged')
            print('PROBE_STARTED', run_id, duration)
        elif action == 'status':
            sock.sendall(f'tail -n 2 /root/nac-probe-{run_id}.jsonl; cat /root/nac-probe-{run_id}.log\r\n'.encode())
            print(read(sock, 1.5).decode('utf-8', 'replace')[-2000:])
        elif action == 'stop':
            sock.sendall(f'if test -f /root/nac-probe-{run_id}.pid; then kill "$(cat /root/nac-probe-{run_id}.pid)" 2>/dev/null || true; fi\r\n'.encode())
            read(sock, 1)
            print('PROBE_STOP_REQUESTED', run_id)
        elif action == 'fetch':
            output = '/root/nac-probe-' + run_id + '.jsonl'
            sock.sendall(f'echo NAC_PROBE_FETCH_START; base64 -w 76 {output}; echo NAC_PROBE_FETCH_END\r\n'.encode())
            data = bytearray()
            end = time.monotonic() + 30
            while time.monotonic() < end:
                data.extend(read(sock, 0.2))
                if b'NAC_PROBE_FETCH_END' in data:
                    break
            lines = data.replace(b'\r', b'').split(b'\n')
            start = next(i for i, line in enumerate(lines) if line.endswith(b'NAC_PROBE_FETCH_START'))
            finish = next(i for i, line in enumerate(lines[start+1:], start+1) if line.endswith(b'NAC_PROBE_FETCH_END'))
            encoded = b''.join(line for line in lines[start+1:finish] if re.fullmatch(rb'[A-Za-z0-9+/=]+', line))
            content = base64.b64decode(encoded, validate=True)
            target = Path('/root/nac-qualification') / ('probe-' + run_id + '.jsonl')
            target.write_bytes(content)
            print('PROBE_FETCHED', target, 'sha256', hashlib.sha256(content).hexdigest(), 'bytes', len(content))
    finally:
        sock.sendall(b'stty echo\r\n')
        read(sock, 0.2)
