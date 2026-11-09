"""Disposable echo service for established-flow qualification."""

import json
import socket
from datetime import datetime, timezone

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(('10.70.10.10', 8082))
    listener.listen(1)
    print(json.dumps({'event': 'listening', 'utc': datetime.now(timezone.utc).isoformat()}), flush=True)
    conn, address = listener.accept()
    print(json.dumps({'event': 'accepted', 'peer': address,
                      'utc': datetime.now(timezone.utc).isoformat()}), flush=True)
    with conn:
        conn.settimeout(110)
        while True:
            try:
                payload = conn.recv(256)
            except socket.timeout:
                break
            if not payload:
                break
            conn.sendall(payload)
            print(json.dumps({'event': 'echo', 'data': payload.decode(errors='replace').strip(),
                              'utc': datetime.now(timezone.utc).isoformat()}), flush=True)
print(json.dumps({'event': 'closed', 'utc': datetime.now(timezone.utc).isoformat()}), flush=True)
