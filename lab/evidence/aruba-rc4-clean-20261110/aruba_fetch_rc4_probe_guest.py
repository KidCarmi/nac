"""Fetch only bounded, allowlisted rc4 endpoint probe logs through EVE serial."""
import base64
import hashlib
import os
from pathlib import Path
import select
import socket
import time

phase = os.environ.get('RC4_PROBE_PHASE', '')
if phase not in {'quarantine', 'restore', 'quarantine2', 'restore2'}:
    raise SystemExit('RC4_PROBE_PHASE must be quarantine, restore, quarantine2, or restore2')
origin = f'/root/rc4-postrestart-ui-{phase}-probes.jsonl'
destination = Path('/root/nac-qualification/aruba-rc4-clean-20261110') / f'postrestart-{phase}-probes.jsonl'
destination.parent.mkdir(mode=0o700, exist_ok=True)
password = Path('/root/nac-linux.rootpw').read_text().strip()


def read(sock, seconds):
    data = bytearray()
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        if select.select([sock], [], [], 0.1)[0]:
            data.extend(sock.recv(65536))
            if b'RC4_PROBE_END' in data:
                break
    return bytes(data)


with socket.create_connection(('127.0.0.1', 32830), timeout=5) as sock:
    sock.sendall(b'\r\n')
    greeting = read(sock, 1)
    if b'login:' in greeting:
        sock.sendall(b'root\r\n')
        read(sock, 1)
        sock.sendall(password.encode() + b'\r\n')
        read(sock, 2)
    sock.sendall(b'stty -echo\r\n')
    read(sock, 0.5)
    sock.sendall(("printf 'RC4_PROBE_START'; base64 -w0 " + origin +
                  "; printf 'RC4_PROBE_END'\r\n").encode())
    output = read(sock, 8)
    sock.sendall(b'stty echo\r\n')
    read(sock, 0.2)
start = output.find(b'RC4_PROBE_START')
end = output.find(b'RC4_PROBE_END', start + len(b'RC4_PROBE_START'))
if start < 0 or end < 0:
    raise SystemExit('probe fetch markers missing')
payload = base64.b64decode(output[start + len(b'RC4_PROBE_START'):end].strip(), validate=True)
if not payload.endswith(b'\n'):
    raise SystemExit('incomplete probe payload')
destination.write_bytes(payload)
print(f'{phase}: {len(payload)} bytes sha256={hashlib.sha256(payload).hexdigest()}')
