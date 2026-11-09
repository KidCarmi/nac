"""Monitor one established TCP connection across Aruba role CoAs."""

import json
import socket
import time
from datetime import datetime, timezone


def emit(event, **data):
    print(json.dumps({'utc': datetime.now(timezone.utc).isoformat(),
                      'monotonic': round(time.monotonic(), 6),
                      'event': event, **data}), flush=True)


with socket.create_connection(('10.70.10.10', 8082), timeout=3) as sock:
    sock.settimeout(1)
    emit('connected', local=sock.getsockname(), peer=sock.getpeername())
    deadline = time.monotonic() + 95
    number = 0
    while time.monotonic() < deadline:
        payload = f'{number:04d}\n'.encode()
        try:
            sock.sendall(payload)
            response = sock.recv(len(payload))
            emit('echo_ok' if response == payload else 'echo_mismatch', number=number,
                 response=response.decode(errors='replace'))
        except (OSError, TimeoutError) as exc:
            emit('flow_error', number=number, error=type(exc).__name__)
        number += 1
        time.sleep(0.5)
