"""Print selected, nonsecret RADIUS and EAPOL evidence from bounded captures."""
from pathlib import Path
import subprocess

root = Path('/root/nac-qualification/arista-rc4-20261110')
for phase in ('valid-retry', 'invalid', 'policy-quarantine', 'policy-restore'):
    print('PHASE', phase)
    for name in ('control', 'endpoint'):
        output = subprocess.run(['tcpdump', '-nn', '-vvv', '-r', str(root / f'{phase}-{name}.pcap')],
            text=True, capture_output=True, check=True).stdout
        for line in output.splitlines():
            if any(term in line for term in ('Access-Accept', 'Access-Reject',
                    'CoA-Request', 'CoA-ACK', 'Tunnel-Private-Group-ID',
                    'Tunnel-Type', 'Tunnel-Medium-Type', 'EAPOL start',
                    'EAP-Success', 'EAP-Failure', 'DHCP-')):
                print(name, line.strip()[:240])
