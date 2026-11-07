"""Summarize one bounded Linux DHCP probe phase with explicit observation end.

Usage: analyze_linux_lease.py PROBE name=UTC,old,new --until UTC
Run a separate phase for manual renewal or reconnect diagnostics.
"""
import argparse
from datetime import datetime
import json
from pathlib import Path

def instant(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('probe', type=Path)
parser.add_argument('action', help='name=UTC,old_segment,new_segment')
parser.add_argument('--until', required=True, help='UTC end of this observation phase')
args = parser.parse_args()
name, rest = args.action.split('=', 1)
at_value, old, new = rest.split(',')
if {old, new} != {'corp', 'quarantine'}:
    parser.error('old and new must be corp and quarantine')
at, until = instant(at_value), instant(args.until)
if until <= at:
    parser.error('observation end must follow action')
rows = [json.loads(line) for line in args.probe.read_text(encoding='utf-8').splitlines()
        if line.strip()]
window = [row for row in rows if at <= instant(row['at_utc']) <= until]
prior = [row for row in rows if row.get('event') == 'network_state'
         and instant(row['at_utc']) <= at]
print('PHASE', name, at.isoformat(), until.isoformat(), old, 'to', new)
print('PROBE_RESOLUTION', 'sequential fresh TCP with 0.8s per-target timeout')
print('STATE_BEFORE', json.dumps(prior[-1] if prior else None, sort_keys=True))
print('OBSERVED_THROUGH', window[-1]['at_utc'] if window else None)
for segment, connected, label in ((old, False, 'OLD_DENIED'),
                                  (new, True, 'NEW_REACHABLE')):
    sample = next((row for row in window if row.get('event') == 'tcp_probe'
                   and row.get('segment') == segment
                   and row.get('connected') is connected), None)
    print(label, json.dumps({
        'at': sample['at_utc'] if sample else None,
        'delay_s': round((instant(sample['at_utc']) - at).total_seconds(), 3)
                   if sample else None,
        'source': sample.get('source') if sample else None}, sort_keys=True))
for row in window:
    if row.get('event') == 'network_state':
        print('NETWORK', json.dumps(row, sort_keys=True))
