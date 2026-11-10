"""Bounded fresh TCP and address probes from the one-NIC Arista endpoint."""
import json
import socket
import subprocess
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone


def probe(item):
    name, host = item
    try:
        with socket.create_connection((host, 8080), timeout=0.7) as sock:
            sock.settimeout(0.7)
            sock.sendall(b'GET / HTTP/1.0\r\nHost: lab\r\n\r\n')
            response = sock.recv(128)
        result = 'http_200' if b' 200 ' in response else 'other_response'
    except OSError as error:
        result = type(error).__name__
    return name, result


targets = [('corp', '10.70.10.10'), ('quarantine', '10.70.20.10')]
deadline = time.monotonic() + 75
with ThreadPoolExecutor(max_workers=2) as pool:
    while time.monotonic() < deadline:
        start = time.monotonic()
        address = subprocess.run(['ip', '-4', '-o', 'addr', 'show', 'dev', 'ens3'],
                                 capture_output=True, text=True).stdout.strip()
        print(json.dumps({'utc': datetime.now(timezone.utc).isoformat(timespec='milliseconds'),
                          'address': address, 'probes': dict(pool.map(probe, targets))}), flush=True)
        time.sleep(max(0, 0.5 - (time.monotonic() - start)))
