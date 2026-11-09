"""Bounded capture of CoA, reauthentication and role traffic in isolated EVE."""

import json
import os
from pathlib import Path
import subprocess
import time

phase = os.environ.get('ARUBA_COA_PHASE', '')
if phase not in {'quarantine', 'restore'}:
    raise SystemExit('ARUBA_COA_PHASE must be quarantine or restore')
directory = Path('/root/aruba-coa-20261109-' + phase)
directory.mkdir(mode=0o700, exist_ok=True)
captures = [
    ('endpoint-tap', 'vunl0_62_0', 'ether proto 0x888e or tcp port 8080'),
    ('switch-access-tap', 'vunl0_61_1', 'ether proto 0x888e or tcp port 8080'),
    ('switch-control-tap', 'vunl0_61_0', 'udp port 1812 or udp port 3799'),
    ('server-tap', 'vunl0_65_0', 'udp port 1812 or udp port 3799'),
]
results = []
for name, interface, expression in captures:
    path = directory / (name + '.pcap')
    log = directory / (name + '.log')
    with log.open('wb') as output:
        process = subprocess.Popen(
            ['timeout', '58', 'tcpdump', '-nn', '-i', interface, '-s', '0', '-U',
             '-c', '1200', '-w', str(path), expression],
            stdin=subprocess.DEVNULL, stdout=output, stderr=output,
            start_new_session=True)
    results.append({'name': name, 'interface': interface, 'pid': process.pid})
time.sleep(1)
print(json.dumps({'capture_started_at_epoch': time.time(), 'captures': results}))
