"""Serve verified rc9 bundle on the isolated EVE control link only."""
import hashlib
from pathlib import Path
import subprocess
import time
import urllib.request

ROOT = Path('/root/nac-pilot-build')
DIST = ROOT / 'source-3e92642/dist'
BUNDLE = DIST / 'nac-pilot-pilot-rc9.tar.gz'
SIDECAR = DIST / (BUNDLE.name + '.sha256')
PID = ROOT / 'bundle-3e92642.pid'
if not BUNDLE.is_file() or not SIDECAR.is_file() or PID.exists():
    raise SystemExit('bundle or PID precondition failed')
digest = hashlib.sha256(BUNDLE.read_bytes()).hexdigest()
if SIDECAR.read_text().split()[0] != digest:
    raise SystemExit('bundle sidecar mismatch')
with (ROOT / 'bundle-3e92642.log').open('wb') as log:
    process = subprocess.Popen(['python3', '-m', 'http.server', '18090', '--bind', '10.70.99.254', '--directory', str(DIST)],
                               stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
PID.write_text(str(process.pid))
time.sleep(0.5)
if process.poll() is not None:
    raise SystemExit('bundle server did not start')
with urllib.request.urlopen('http://10.70.99.254:18090/' + SIDECAR.name, timeout=5) as response:
    if response.status != 200:
        raise SystemExit('bundle server health failed')
print('RC9_BUNDLE_SERVER port 18090 sha256', digest)


