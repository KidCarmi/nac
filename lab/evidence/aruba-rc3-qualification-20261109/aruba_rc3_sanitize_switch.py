"""Extract nonsecret switch session fields from a serial transcript."""

import re
import sys
from pathlib import Path

raw = Path(sys.argv[1]).read_text(encoding='utf-8', errors='replace')
raw = re.sub(r'\x1b(?:\[[0-9;?]*[A-Za-z]|\][^\x07]*\x07)', '', raw)
lines = [re.sub(r'[^\x20-\x7e]', '', line).strip() for line in raw.splitlines()]
fields = {}
for line in lines:
    if line.startswith('Version      :'):
        fields['EOS version'] = line.partition(':')[2].strip()
    if line.startswith('Client 00:'):
        fields['Client'] = line.removeprefix('Client ')
    for label in ('Port', 'VLANs Assigned', 'Access', 'Status', 'Auth Precedence', 'Auth History', 'Role'):
        if line.startswith(label + ' ') and ':' in line:
            value = line.partition(':')[2].strip()
            if not value:
                continue
            if label == 'Status' and value not in ('dot1x Authenticated', 'Applied'):
                continue
            if label == 'Role' and value not in ('corp', 'quarantine'):
                continue
            fields[label + (' (authorization)' if value == 'Applied' else '')] = value
print('\n'.join(f'{name}: {value}' for name, value in fields.items()))
