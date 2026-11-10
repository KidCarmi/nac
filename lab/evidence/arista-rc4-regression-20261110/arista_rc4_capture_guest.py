"""Start bounded EVE packet captures for one pinned Arista rc4 phase."""
import json
import os
from pathlib import Path
import subprocess
import time

phase = os.environ['ARISTA_RC4_PHASE']
if phase not in {'valid', 'valid-retry', 'invalid', 'dhcp-diagnostic', 'policy-quarantine', 'policy-restore', 'coa-quarantine', 'coa-restore'}:
    raise SystemExit('unexpected phase')
root = Path('/root/nac-qualification/arista-rc4-20261110')
root.mkdir(parents=True, exist_ok=True)
for name, iface, filt in (
    ('endpoint', 'vunl0_32_0', 'ether proto 0x888e or udp port 67 or udp port 68 or tcp port 8080'),
    ('control', 'vnet0_34', 'udp port 1812 or udp port 1813 or udp port 3799'),
):
    pcap = root / f'{phase}-{name}.pcap'
    if pcap.exists():
        raise SystemExit(f'refusing to overwrite {pcap}')
    log = (root / f'{phase}-{name}.tcpdump.log').open('w')
    proc = subprocess.Popen(['timeout', '120', 'tcpdump', '-U', '-n', '-i', iface,
                            '-s', '0', '-c', '10000', '-w', str(pcap), filt],
                            stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                            start_new_session=True)
    print(json.dumps({'phase': phase, 'name': name, 'interface': iface,
                      'pid': proc.pid, 'pcap': str(pcap),
                      'started_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}))
