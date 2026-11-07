"""Inspect isolated clean Ubuntu guest through its serial login, redacting password."""
from pathlib import Path
import select
import socket
import time

password = Path('/root/nac-linux.rootpw').read_text().strip()


def read(sock, seconds):
    data = bytearray()
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if select.select([sock], [], [], 0.1)[0]:
            part = sock.recv(65536)
            if not part:
                break
            data.extend(part)
    return data.decode(errors='replace').replace(password, '[REDACTED]')


with socket.create_connection(('127.0.0.1', 32900), timeout=5) as sock:
    sock.sendall(b'\r\n')
    initial = read(sock, 0.8)
    if 'login:' in initial:
        sock.sendall(b'root\r\n')
        challenge = read(sock, 0.8)
        if 'Password:' not in challenge:
            raise RuntimeError('serial password prompt absent')
        sock.sendall(password.encode() + b'\r\n')
        read(sock, 1.2)
    sock.sendall(b'echo NAC_CLEAN_START; date -u +%FT%TZ; systemctl is-active nacd freeradius postgresql nac-freeradius-reload.path; curl -fsS http://127.0.0.1:8080/readyz; echo; curl -ksS -o /dev/null -w "CONSOLE_HTTP=%{http_code}\\n" https://127.0.0.1:8443/ui/; echo NAC_CLEAN_END\r\n')
    print('INVENTORY', read(sock, 6)[-5000:])
