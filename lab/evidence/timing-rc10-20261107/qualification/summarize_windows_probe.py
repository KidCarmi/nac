"""Summarize one-NIC Windows probe transitions against product action times."""
import argparse
from datetime import datetime
import json
from pathlib import Path


def dt(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('probe', type=Path)
    parser.add_argument('actions', nargs='+', help='name=UTC timestamp')
    args = parser.parse_args()
    rows = [json.loads(line) for line in args.probe.read_text().splitlines() if line.strip()]
    print('PROBE_ROWS', len(rows), 'FIRST', rows[0]['at_utc'], 'LAST', rows[-1]['at_utc'])
    actions = [(name, dt(at), at) for name, at in (item.split('=', 1) for item in args.actions)]
    for index, (name, when, at) in enumerate(actions):
        until = actions[index + 1][1] if index + 1 < len(actions) else None
        print('ACTION', name, at)
        for segment in ('corp', 'quarantine'):
            for connected in (False, True):
                match = next((row for row in rows if row['event'] == 'tcp_probe'
                              and row['segment'] == segment
                              and row['connected'] is connected
                              and dt(row['at_utc']) >= when
                              and (until is None or dt(row['at_utc']) < until)), None)
                if match:
                    print('TCP', segment, connected, match['at_utc'],
                          'delay_seconds', round((dt(match['at_utc']) - when).total_seconds(), 3),
                          'source', match.get('source'))
        last_state = None
        for row in rows:
            if (row['event'] != 'network_state' or dt(row['at_utc']) < when
                    or (until is not None and dt(row['at_utc']) >= until)):
                continue
            key = (tuple(row.get('addresses', [])), tuple(row.get('dns', [])),
                   tuple(row.get('routes', [])))
            if key != last_state:
                print('NETWORK', row['at_utc'], 'delay_seconds',
                      round((dt(row['at_utc']) - when).total_seconds(), 3),
                      'addresses', row.get('addresses'), 'dns', row.get('dns'),
                      'routes', row.get('routes'), 'lease', row.get('lease'))
                last_state = key


if __name__ == '__main__':
    main()
