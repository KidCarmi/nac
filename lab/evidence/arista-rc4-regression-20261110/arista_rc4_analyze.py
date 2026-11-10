"""Summarize bounded continuous probe logs for the pinned Arista rc4 run."""
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

root = (Path(sys.argv[1]) if len(sys.argv) > 1 else
        Path(__file__).parent / 'runs' / 'arista-rc4-20261110' / 'arista-rc4-20261110')
actions = {
    'policy-quarantine': '2026-11-10T08:31:32.095767+00:00',
    'policy-restore': '2026-11-10T08:33:42.713748+00:00',
    'coa-quarantine': '2026-11-10T08:44:31.010589+00:00',
    'coa-restore': '2026-11-10T08:46:11.318649+00:00',
}
for phase, timestamp in actions.items():
    when = datetime.fromisoformat(timestamp)
    rows = [json.loads(line) for line in (root / f'{phase}-probes.jsonl').read_text().splitlines()]
    for row in rows:
        row['dt'] = datetime.fromisoformat(row['utc'])
    prior = [r for r in rows if r['dt'] < when]
    after = [r for r in rows if r['dt'] >= when]
    print(phase, 'samples', len(rows), 'pre', len(prior), 'post', len(after))
    def ipv4(row):
        match = re.search(r'inet (\d+\.\d+\.\d+\.\d+/\d+)', row['address'])
        return match.group(1) if match else None
    print('first', rows[0]['utc'], ipv4(rows[0]), rows[0]['probes'])
    for segment in ('corp', 'quarantine'):
        before = prior[-1]['probes'][segment] if prior else None
        changed = next((r for r in after if r['probes'][segment] != before), None)
        opened = next((r for r in after if r['probes'][segment] == 'http_200'), None)
        denied = next((r for r in after if r['probes'][segment] != 'http_200'), None)
        def summary(row):
            return None if row is None else {'utc': row['utc'],
                'seconds_after_coa': round((row['dt'] - when).total_seconds(), 3),
                'outcome': row['probes'][segment]}
        print(segment, 'before', before, 'first_change', summary(changed),
              'first_open', summary(opened), 'first_denied', summary(denied))
    address_changes = []
    last = None
    for row in rows:
        address = ipv4(row)
        if address != last:
            address_changes.append({'utc': row['utc'], 'address': address})
            last = address
    print('address_changes', address_changes)
    print('last', rows[-1]['utc'], ipv4(rows[-1]), rows[-1]['probes'])
