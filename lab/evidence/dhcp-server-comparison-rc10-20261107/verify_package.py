"""Verify the published rc10 DHCP server comparison package without lab access."""
import argparse
import hashlib
import json
from pathlib import Path
import re

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('directory', type=Path)
root = parser.parse_args().directory.resolve()
if root.name != 'dhcp-server-comparison-rc10-20261107':
    parser.error('wrong package directory')
manifest = root / 'sha256-manifest.json'
expected = json.loads(manifest.read_text(encoding='utf-8'))['files']
actual = []
for path in sorted(root.rglob('*')):
    if not path.is_file() or path == manifest:
        continue
    data = path.read_bytes()
    if path.suffix.lower() in {'.key', '.pfx', '.qcow2', '.vmdk', '.iso', '.deb'}:
        raise SystemExit('forbidden file ' + str(path))
    if re.search(rb'-----BEGIN (?:RSA |OPENSSH )?PRIVATE KEY-----', data):
        raise SystemExit('private key marker ' + str(path))
    actual.append({'path': path.relative_to(root).as_posix(),
                   'bytes': len(data),
                   'sha256': hashlib.sha256(data).hexdigest()})
if actual != expected:
    raise SystemExit('manifest mismatch')
for path in root.rglob('*.md'):
    for link in re.findall(r'\]\(([^)]+)\)', path.read_text(encoding='utf-8')):
        if link.startswith(('http://', 'https://', '#')):
            continue
        if not (path.parent / link.split('#', 1)[0]).exists():
            raise SystemExit('broken local link ' + str(path) + ' -> ' + link)
print('VERIFIED_FILES', len(actual), 'BYTES', sum(item['bytes'] for item in actual))
