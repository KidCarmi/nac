"""Check MAB run artifacts for known plaintext lab credentials; print counts only."""
from pathlib import Path

root = Path('/root/nac-qualification/mikrotik-mab-20261106')
secrets = []
for name in ('/root/nac-pki/radius.secret', '/root/nac-pki/nac-service-token',
             '/root/nac-pilot-control.rootpw', '/root/nac-mikrotik-admin.secret',
             '/root/nac-linux.rootpw', '/root/eve-nac-lab.rootpw',
             '/root/eve-nac-lab.webpw'):
    path = Path(name)
    if path.exists():
        secrets.append(path.read_bytes().strip())
for line in Path('/root/nac-lab.env').read_bytes().splitlines():
    if b'=' in line:
        key, value = line.split(b'=', 1)
        if any(word in key for word in (b'PASSWORD', b'SECRET', b'TOKEN')):
            secrets.append(value.strip())
for name in ('radius.pcap', 'eapol.pcap', 'radius-timeline.txt', 'eapol-timeline.txt'):
    path = root / name
    if not path.exists():
        continue
    data = path.read_bytes()
    matches = sum(1 for value in secrets if len(value) >= 12 and value in data)
    print(name, 'plaintext_credential_matches', matches)
    if matches:
        raise SystemExit(1)
