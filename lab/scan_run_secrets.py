"""Check an EVE qualification run for known plaintext lab credentials.

Run inside EVE with RUN_ID set. Prints counts only, never credential values.
"""
import os
from pathlib import Path
import re

run_id = os.environ['RUN_ID']
if not re.fullmatch(r'baseline-[0-9]{8}T[0-9]{6}Z', run_id):
    raise SystemExit('invalid run id')
root = Path('/root/nac-qualification') / run_id
files = ('radius.pcap', 'endpoint-eapol.pcap', 'nac-decisions.jsonl', 'events.jsonl')
secrets = [
    Path('/root/nac-pki/radius.secret').read_bytes().strip(),
    Path('/root/nac-pki/nac-service-token').read_bytes().strip(),
]
for line in Path('/root/nac-lab.env').read_bytes().splitlines():
    if b'=' in line:
        key, value = line.split(b'=', 1)
        if b'PASSWORD' in key or b'SECRET' in key or b'TOKEN' in key:
            secrets.append(value.strip())
for name in files:
    data = (root / name).read_bytes()
    matches = sum(1 for secret in secrets if len(secret) >= 12 and secret in data)
    print(name, 'plaintext_credential_matches', matches)
    if matches:
        raise SystemExit(1)
