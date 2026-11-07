"""Verify hashes, local links, and forbidden files in the Windows lease package."""
from pathlib import Path
import argparse
import hashlib
import json
import re

parser = argparse.ArgumentParser()
parser.add_argument('directory', type=Path)
parser.add_argument('--write', action='store_true', help='refresh the manifest after reviewed file edits')
args = parser.parse_args()
root = args.directory.resolve()
if root.name != 'windows-lease-rc10-20261107':
    parser.error('wrong package directory')
manifest = root / 'sha256-manifest.json'
expected = json.loads(manifest.read_text(encoding='utf-8'))['files']
actual = []
for path in sorted(root.rglob('*')):
    if path.is_file() and path != manifest:
        data = path.read_bytes()
        actual.append({'path': path.relative_to(root).as_posix(),
                       'bytes': len(data),
                       'sha256': hashlib.sha256(data).hexdigest()})
        if path.suffix.lower() in {'.key', '.pfx', '.qcow2', '.vmdk', '.iso', '.tar', '.gz'}:
            raise SystemExit('forbidden file ' + str(path))
        if re.search(rb'-----BEGIN (?:RSA |OPENSSH )?PRIVATE KEY-----', data):
            raise SystemExit('private key marker ' + str(path))
if args.write:
    manifest.write_text(json.dumps({'files': actual}, indent=2) + '\n', encoding='utf-8')
    expected = actual
if actual != expected:
    raise SystemExit('manifest mismatch')
for path in root.rglob('*.md'):
    for link in re.findall(r'\]\(([^)]+)\)', path.read_text(encoding='utf-8')):
        if link.startswith(('http://', 'https://', '#')):
            continue
        if not (path.parent / link.split('#', 1)[0]).exists():
            raise SystemExit('broken link ' + str(path) + ' -> ' + link)
print('VERIFIED_FILES', len(actual), 'BYTES', sum(x['bytes'] for x in actual))
