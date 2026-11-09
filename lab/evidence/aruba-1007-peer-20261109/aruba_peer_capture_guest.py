"""Bounded capture of peer-initiated traffic on both isolated EVE taps."""

import json
from pathlib import Path
import subprocess
import time

directory = Path('/root/aruba-peer-inbound-20261109')
directory.mkdir(mode=0o700, exist_ok=True)
for name, interface in [('endpoint-tap', 'vunl0_62_0'),
                        ('peer-tap', 'vunl0_66_0')]:
    with (directory / f'{name}.log').open('wb') as output:
        process = subprocess.Popen(
            ['timeout', '35', 'tcpdump', '-nn', '-i', interface, '-s', '0', '-U',
             '-c', '200', '-w', str(directory / f'{name}.pcap'),
             'host 10.70.10.50 and (tcp port 8081 or udp port 8082)'],
            stdin=subprocess.DEVNULL, stdout=output, stderr=output,
            start_new_session=True)
    print(json.dumps({'name': name, 'interface': interface, 'pid': process.pid}))
time.sleep(1)
