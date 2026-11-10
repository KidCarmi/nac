"""Fetch allowlisted bounded one-NIC probe logs from the Arista endpoint."""
import base64
import hashlib
import os
from pathlib import Path
import select
import socket
import time

phase = os.environ['ARISTA_RC4_PHASE']
if phase not in {'policy-quarantine', 'policy-restore', 'coa-quarantine', 'coa-restore'}:
    raise SystemExit('unexpected phase')
origin = f'/root/arista-rc4-{phase}-probes.jsonl'
destination = Path('/root/nac-qualification/arista-rc4-20261110') / f'{phase}-probes.jsonl'
password = Path('/root/nac-linux.rootpw').read_text().strip()


def read(sock, seconds):
    data = bytearray()
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        if select.select([sock], [], [], 0.1)[0]:
            data.extend(sock.recv(65536))
            if b'ASTRA_END' in data:
                break
    return bytes(data)


with socket.create_connection(('127.0.0.1', 32800), timeout=5) as sock:
    sock.sendall(b'\r\n')
    greeting = read(sock, 1)
    if b'login:' in greeting:
        sock.sendall(b'root\r\n')
        read(sock, 1)
        sock.sendall(password.encode() + b'\r\n')
        read(sock, 2)
    sock.sendall(b'stty -echo\r\n')
    read(sock, 0.5)
    sock.sendall(("printf 'ASTRA_START'; base64 -w0 " + origin +
                  "; printf 'ASTRA_END'\r\n").encode())
    output = read(sock, 15)
    sock.sendall(b'stty echo\r\n')
    read(sock, 0.2)
start = output.find(b'ASTRA_START')
end = output.find(b'ASTRA_END', start + len(b'ASTRA_START'))
if start < 0 or end < 0:
    raise SystemExit('probe markers missing')
payload = base64.b64decode(output[start+len(b'ASTRA_START'):end].strip(), validate=True)
if not payload.endswith(b'\n'):
    raise SystemExit('incomplete probe')
destination.parent.mkdir(parents=True, exist_ok=True)
destination.write_bytes(payload)
print(f'{phase}: bytes={len(payload)} sha256={hashlib.sha256(payload).hexdigest()}')
