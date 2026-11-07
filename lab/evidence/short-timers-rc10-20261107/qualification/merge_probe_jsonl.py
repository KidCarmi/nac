"""Merge overlapping bounded endpoint probe streams in timestamp order."""

import argparse
import json
from pathlib import Path


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output', type=Path)
    parser.add_argument('inputs', type=Path, nargs='+')
    args = parser.parse_args()
    rows = []
    for source in args.inputs:
        for line in source.read_text(encoding='utf-8').splitlines():
            if line:
                item = json.loads(line)
                if 'at_utc' not in item:
                    raise SystemExit('missing timestamp: ' + str(source))
                rows.append(item)
    rows.sort(key=lambda item: item['at_utc'])
    if args.output.exists():
        raise SystemExit('refusing to overwrite merged probe')
    args.output.write_text(''.join(json.dumps(row, sort_keys=True) + '\n' for row in rows),
                           encoding='utf-8')
    print('MERGED_ROWS', len(rows), 'FROM', len(args.inputs), 'INPUTS')
