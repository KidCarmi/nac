"""Bounded, fresh TCP probes from the isolated wired endpoint."""

import json
import socket
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone

TARGETS = {'corp': '10.70.10.10', 'remediation': '10.70.20.10', 'peer': '10.70.10.11'}


def probe(item):
    name, host = item
    try:
        with socket.create_connection((host, 8080), timeout=0.8) as sock:
            sock.settimeout(0.8)
            sock.sendall(b'GET / HTTP/1.0\r\nHost: lab\r\n\r\n')
            response = sock.recv(128)
        outcome = 'http_200' if b' 200 ' in response else 'other_response'
    except (OSError, TimeoutError) as exc:
        outcome = type(exc).__name__
    return name, outcome


deadline = time.monotonic() + 48
with ThreadPoolExecutor(max_workers=3) as pool:
    while time.monotonic() < deadline:
        started = time.monotonic()
        results = dict(pool.map(probe, TARGETS.items()))
        print(json.dumps({'utc': datetime.now(timezone.utc).isoformat(),
                          'monotonic': round(started, 6), 'probes': results}), flush=True)
        time.sleep(max(0, 0.5 - (time.monotonic() - started)))
