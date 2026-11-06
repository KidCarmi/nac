"""Continuous fresh TCP probes on the lab's one-NIC wired endpoint.

Run on the endpoint during a candidate CoA action. It writes JSONL while the
product action proceeds separately. No authentication or address change occurs.
"""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import socket
import subprocess
import time

TARGETS = {
    'corp': ('10.70.10.50', '10.70.10.10', 8080),
    'quarantine': ('10.70.20.50', '10.70.20.10', 8080),
}

def utc():
    return datetime.now(timezone.utc).isoformat(timespec='milliseconds')

def probe(segment):
    source, target, port = TARGETS[segment]
    started = time.monotonic()
    error = None
    connected = False
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as conn:
            conn.settimeout(1.2)
            conn.bind((source, 0))
            conn.connect((target, port))
            connected = True
    except OSError as exc:
        error = type(exc).__name__
    return {
        'time': utc(), 'segment': segment, 'source': source,
        'target': target, 'port': port, 'connected': connected,
        'error': error, 'elapsed_ms': round((time.monotonic()-started)*1000),
    }

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--seconds', type=int, default=90)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if not 1 <= args.seconds <= 300:
        parser.error('--seconds must be 1..300')
    if args.output.exists():
        parser.error('output already exists')
    interfaces = set(path.name for path in Path('/sys/class/net').iterdir())
    if interfaces != {'lo', 'ens3'}:
        raise SystemExit('endpoint interface inventory is not one data NIC: ' + repr(sorted(interfaces)))
    address_state = subprocess.check_output(['ip', '-j', 'addr', 'show', 'dev', 'ens3'], text=True)
    observed = {item['local'] for entry in json.loads(address_state) for item in entry.get('addr_info', []) if item.get('family') == 'inet'}
    expected = {item[0] for item in TARGETS.values()}
    if not expected.issubset(observed):
        raise SystemExit('both controlled source addresses must be on ens3')
    deadline = time.monotonic() + args.seconds
    with args.output.open('x') as output:
        output.write(json.dumps({'time': utc(), 'event': 'probe_start', 'interfaces': sorted(interfaces), 'addresses': sorted(observed), 'duration_s': args.seconds}) + '\n')
        output.flush()
        while time.monotonic() < deadline:
            for segment in TARGETS:
                output.write(json.dumps(probe(segment), sort_keys=True) + '\n')
                output.flush()
            time.sleep(0.25)
        output.write(json.dumps({'time': utc(), 'event': 'probe_end'}) + '\n')
        output.flush()

if __name__ == '__main__':
    main()
