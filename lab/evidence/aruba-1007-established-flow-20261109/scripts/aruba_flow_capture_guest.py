"""Bounded access and CoA capture for one established TCP flow."""

import json
from pathlib import Path
import subprocess
import time

directory = Path('/root/aruba-established-flow-20261109')
directory.mkdir(mode=0o700, exist_ok=True)
captures = [
    ('endpoint-tap', 'vunl0_62_0', 'tcp port 8082'),
    ('corp-tap', 'vunl0_63_0', 'tcp port 8082'),
    ('switch-control-tap', 'vunl0_61_0', 'udp port 3799'),
    ('server-tap', 'vunl0_65_0', 'udp port 3799'),
]
for name, interface, expression in captures:
    with (directory / f'{name}.log').open('wb') as output:
        process = subprocess.Popen(
            ['timeout', '105', 'tcpdump', '-nn', '-i', interface, '-s', '0', '-U',
             '-c', '1000', '-w', str(directory / f'{name}.pcap'), expression],
            stdin=subprocess.DEVNULL, stdout=output, stderr=output,
            start_new_session=True)
    print(json.dumps({'name': name, 'interface': interface, 'pid': process.pid}))
time.sleep(1)
