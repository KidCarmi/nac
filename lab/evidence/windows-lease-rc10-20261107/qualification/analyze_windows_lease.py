"""Correlate bounded one-NIC Windows probes with exact NAC action times.

Usage: analyze_windows_lease.py probe.jsonl name=UTC,old,new [...]
The DHCP server's ACK must be checked separately for options 51, 58 and 59.
"""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re


def instant(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


def lease_time(value):
    match = re.fullmatch(r'/Date\((\d+)\)/', value or '')
    return datetime.fromtimestamp(int(match.group(1)) / 1000, timezone.utc) if match else None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('probe', type=Path)
    parser.add_argument('actions', nargs='+', help='name=UTC,old_segment,new_segment')
    args = parser.parse_args()
    rows = [json.loads(line) for line in args.probe.read_text(encoding='utf-8').splitlines()
            if line.strip()]
    actions = []
    for item in args.actions:
        name, rest = item.split('=', 1)
        at, old, new = rest.split(',')
        if old not in ('corp', 'quarantine') or new not in ('corp', 'quarantine') or old == new:
            parser.error('segments must be distinct corp/quarantine values')
        actions.append((name, instant(at), old, new))
    actions.sort(key=lambda value: value[1])
    print('PROBE', rows[0]['at_utc'], rows[-1]['at_utc'], 'rows', len(rows))
    for index, (name, at, old, new) in enumerate(actions):
        until = actions[index + 1][1] if index + 1 < len(actions) else None
        prior = [r for r in rows if r.get('event') == 'network_state'
                 and instant(r['at_utc']) <= at]
        lease = prior[-1].get('lease', [{}])[0] if prior and prior[-1].get('lease') else {}
        obtained = lease_time(lease.get('obtained'))
        expires = lease_time(lease.get('expires'))
        print('ACTION', name, at.isoformat(), 'old', old, 'new', new)
        print('LEASE_BEFORE', json.dumps({
            'address': prior[-1].get('addresses') if prior else None,
            'obtained': obtained.isoformat() if obtained else None,
            'expires': expires.isoformat() if expires else None,
            'duration_s': (expires - obtained).total_seconds() if obtained and expires else None,
            'remaining_s': round((expires - at).total_seconds(), 3) if expires else None,
            'server': lease.get('dhcp_server')}, sort_keys=True))
        window = [r for r in rows if r.get('event') == 'tcp_probe'
                  and instant(r['at_utc']) >= at
                  and (until is None or instant(r['at_utc']) < until)]
        for segment, connected, label in ((old, False, 'OLD_DENIED'), (new, True, 'NEW_REACHABLE')):
            match = next((r for r in window if r['segment'] == segment
                          and r['connected'] is connected), None)
            print(label, json.dumps({
                'at': match['at_utc'] if match else None,
                'delay_s': round((instant(match['at_utc']) - at).total_seconds(), 3) if match else None,
                'source': match.get('source') if match else None}, sort_keys=True))
        changed = [r for r in rows if r.get('event') == 'network_state'
                   and instant(r['at_utc']) >= at
                   and (until is None or instant(r['at_utc']) < until)]
        for state in changed:
            print('NETWORK', json.dumps({key: state.get(key) for key in
                  ('at_utc', 'addresses', 'routes', 'dns', 'lease')}, sort_keys=True))


if __name__ == '__main__':
    main()
