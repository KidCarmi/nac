"""Independently build exact pilot-rc9 from a verified Git bundle inside EVE."""
import hashlib
import os
from pathlib import Path
import subprocess
import tarfile

ROOT = Path('/root/nac-pilot-build')
BUNDLE = ROOT / 'pilot-rc9-3e92642.bundle'
SOURCE = ROOT / 'source-3e92642'
COMMIT = '3e92642110f97d3f42a43652d33479e82cfbcc35'
BUNDLE_SHA = '306871d1364a45215f6860a61dc6b3f040417d213ec37b0ea026104adae8e816'
NACD_SHA = 'd2d93e3ddc40a7eadcddfd3275f3d8a7aa5610ca07b43eb219bb9d59267a3749'


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(4 * 1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


if digest(BUNDLE) != BUNDLE_SHA or SOURCE.exists():
    raise SystemExit('bundle hash or source destination precondition failed')
subprocess.run(['git', 'clone', '--quiet', '--no-checkout', str(BUNDLE), str(SOURCE)], check=True)
subprocess.run(['git', '-C', str(SOURCE), 'checkout', '--quiet', '--detach', COMMIT], check=True)
if subprocess.check_output(['git', '-C', str(SOURCE), 'rev-parse', 'HEAD'], text=True).strip() != COMMIT:
    raise SystemExit('source commit mismatch')
env = dict(os.environ, PATH='/root/nac-pilot-build/toolchain/go/bin:' + os.environ['PATH'], GOTOOLCHAIN='go1.25.0')
print('TOOLCHAIN', subprocess.check_output(['/root/nac-pilot-build/toolchain/go/bin/go', 'version'], text=True).strip(), flush=True)
print('SOURCE_COMMIT', COMMIT, flush=True)
result = subprocess.run(['make', 'pilot-bundle', 'VERSION=pilot-rc9'], cwd=SOURCE, env=env,
                        text=True, capture_output=True, timeout=600)
if result.returncode:
    raise SystemExit('bundle build failed: ' + (result.stdout + result.stderr)[-2000:])
archive = SOURCE / 'dist/nac-pilot-pilot-rc9.tar.gz'
with tarfile.open(archive) as tar:
    manifest = tar.extractfile('nac-pilot-pilot-rc9/BUNDLE-MANIFEST.txt').read().decode()
    nacd = tar.extractfile('nac-pilot-pilot-rc9/bin/nacd').read()
actual = hashlib.sha256(nacd).hexdigest()
if actual != NACD_SHA or f'commit: {COMMIT}' not in manifest or f'nacd_sha256: {NACD_SHA}' not in manifest:
    raise SystemExit('nacd or manifest mismatch')
print('NACD_SHA256', actual, flush=True)
print('ARCHIVE_SHA256', digest(archive), flush=True)
print('BUNDLE_MANIFEST', manifest.replace('\n', '; '), flush=True)


