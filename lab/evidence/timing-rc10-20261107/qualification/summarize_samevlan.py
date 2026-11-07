"""Report observed fresh-TCP gaps around same-VLAN reauthentication triggers."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path

def dt(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))

parser = argparse.ArgumentParser()
parser.add_argument('probe', type=Path)
parser.add_argument('actions', nargs='+', help='name=UTC timestamp')
args = parser.parse_args()
rows = [json.loads(line) for line in args.probe.read_text().splitlines() if line.strip()]
tcp = [row for row in rows if row.get('event') == 'tcp_probe' and row.get('segment') == 'corp']
actions = [(name, dt(value)) for name, value in (a.split('=', 1) for a in args.actions)]
print('PROBE', len(rows), rows[0]['at_utc'], rows[-1]['at_utc'])
idle = [r for r in tcp if dt(rows[0]['at_utc']) <= dt(r['at_utc']) < actions[0][1]]
print('IDLE', len(idle), 'failed', sum(not r['connected'] for r in idle))
for i, (name, at) in enumerate(actions):
    end = actions[i+1][1] if i+1 < len(actions) else dt(rows[-1]['at_utc'])
    window = [r for r in tcp if at <= dt(r['at_utc']) < end]
    prior = next((r for r in reversed(tcp) if dt(r['at_utc']) < at), None)
    first_failed = next((r for r in window if not r['connected']), None)
    first_recovered = None
    if first_failed:
        first_recovered = next((r for r in window if r['connected'] and dt(r['at_utc']) > dt(first_failed['at_utc'])), None)
    failures = [r for r in window if not r['connected']]
    print(json.dumps({
        'name': name, 'action_at': at.isoformat(), 'before': prior and {'at':prior['at_utc'],'connected':prior['connected']},
        'samples':len(window), 'failed_samples':len(failures),
        'first_failed_at':first_failed and first_failed['at_utc'],
        'last_failed_at':failures[-1]['at_utc'] if failures else None,
        'first_recovered_at':first_recovered and first_recovered['at_utc'],
        'first_failure_delay_s':round((dt(first_failed['at_utc'])-at).total_seconds(),3) if first_failed else None,
        'recovery_delay_s':round((dt(first_recovered['at_utc'])-at).total_seconds(),3) if first_recovered else None,
        'failed_span_s':round((dt(failures[-1]['at_utc'])-dt(first_failed['at_utc'])).total_seconds(),3) if failures else None,
        'network_states': [{'at':r['at_utc'],'addresses':r.get('addresses'),'lease':r.get('lease')}
                           for r in rows if r.get('event')=='network_state' and at <= dt(r['at_utc']) < end],
    }, sort_keys=True))
