"""Bounded IPv4/IPv6 role traffic capture on isolated Aruba EVE taps."""

import json
from pathlib import Path
import subprocess
import time

directory = Path('/root/aruba-ipv6-role-20261109')
directory.mkdir(mode=0o700, exist_ok=True)
for name, interface, expression in [
    ('endpoint-tap', 'vunl0_62_0', 'tcp port 8080 or tcp port 8083'),
    ('corp-tap', 'vunl0_63_0', 'tcp port 8080 or tcp port 8083'),
    ('server-tap', 'vunl0_65_0', 'udp port 3799'),
]:
    with (directory / f'{name}.log').open('wb') as output:
        process = subprocess.Popen(
            ['timeout', '60', 'tcpdump', '-nn', '-i', interface, '-s', '0', '-U',
             '-c', '500', '-w', str(directory / f'{name}.pcap'), expression],
            stdin=subprocess.DEVNULL, stdout=output, stderr=output,
            start_new_session=True)
    print(json.dumps({'name': name, 'interface': interface, 'pid': process.pid}))
time.sleep(1)
