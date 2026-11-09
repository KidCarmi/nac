"""Start bounded EAPOL and RADIUS captures for the isolated rc3 switch."""

import json
import os
from pathlib import Path
import subprocess
import time

phase = os.environ.get('ARUBA_AUTH_PHASE', 'valid')
if phase not in ('valid', 'invalid', 'policy-quarantine', 'policy-restore'):
    raise SystemExit('invalid ARUBA_AUTH_PHASE')
directory = Path('/root/aruba-rc3-auth-20261109-' + phase)
directory.mkdir(mode=0o700, exist_ok=True)
captures = [
    ('endpoint-eapol', 'vunl0_62_0', 'ether proto 0x888e'),
    ('switch-eapol', 'vunl0_61_1', 'ether proto 0x888e'),
    ('switch-radius', 'vunl0_61_0', 'udp port 1812 or udp port 1813'),
    ('candidate-radius', 'nacarbrc3', 'udp port 1812 or udp port 1813'),
]
started = []
for name, interface, expression in captures:
    output_path = directory / (name + '.pcap')
    with (directory / (name + '.log')).open('wb') as log:
        process = subprocess.Popen(
            ['timeout', '24', 'tcpdump', '-nn', '-i', interface, '-s', '0',
             '-U', '-c', '400', '-w', str(output_path), expression],
            stdin=subprocess.DEVNULL, stdout=log, stderr=log,
            start_new_session=True)
    started.append({'name': name, 'interface': interface, 'pid': process.pid})
time.sleep(1)
print(json.dumps({'capture_started_at_epoch': time.time(), 'captures': started}))
