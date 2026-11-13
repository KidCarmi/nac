"""Future-run endpoint logger: one data NIC, fresh TCP, explicit end marker."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json
from pathlib import Path
import socket
import subprocess
import time

TARGETS = {'corp': '10.70.10.10', 'remediation': '10.70.20.10'}


def utc():
    return datetime.now(timezone.utc).isoformat()


def probe(item):
    name, host = item
    try:
        with socket.create_connection((host, 8080), timeout=0.8) as conn:
            conn.settimeout(0.8)
            conn.sendall(b'GET / HTTP/1.0\r\nHost: lab\r\n\r\n')
            response = conn.recv(128)
        result = 'http_200' if b' 200 ' in response else 'other_response'
    except (OSError, TimeoutError) as exc:
        result = type(exc).__name__
    return name, result


def emit(row):
    print(json.dumps(row, sort_keys=True), flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--seconds', type=int, default=60)
    args = parser.parse_args()
    if not 10 <= args.seconds <= 300:
        parser.error('--seconds must be 10..300')
    interfaces = {item.name for item in Path('/sys/class/net').iterdir()}
    if interfaces != {'lo', 'ens3'}:
        raise SystemExit('endpoint must have exactly one data NIC')
    addresses = json.loads(subprocess.check_output(['ip', '-j', 'addr', 'show', 'dev', 'ens3'], text=True))
    if not any(a.get('family') == 'inet' for i in addresses for a in i.get('addr_info', [])):
        raise SystemExit('endpoint has no IPv4 data address')
    started = time.monotonic()
    emit({'event': 'probe_start', 'run_id': args.run_id, 'utc': utc(),
          'seconds': args.seconds, 'interfaces': sorted(interfaces)})
    samples = 0
    with ThreadPoolExecutor(max_workers=len(TARGETS)) as pool:
        while time.monotonic() - started < args.seconds:
            row_start = time.monotonic()
            results = dict(pool.map(probe, TARGETS.items()))
            emit({'event': 'probe', 'run_id': args.run_id, 'utc': utc(),
                  'monotonic': round(row_start, 6), 'probes': results})
            samples += 1
            time.sleep(max(0, 0.5 - (time.monotonic() - row_start)))
    emit({'event': 'probe_end', 'run_id': args.run_id, 'utc': utc(),
          'elapsed_s': round(time.monotonic() - started, 3), 'samples': samples})


if __name__ == '__main__':
    main()
