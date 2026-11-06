"""Fetch a pinned official Ubuntu 24.04 QCOW2 inside EVE for candidate prep.

This only adds a new EVE image directory; it does not change the baseline lab.
Run through eve_guest.py --script. No vendor or credential material is exported.
"""
import hashlib
import os
from pathlib import Path
import time
import urllib.request

URL = 'https://cloud-images.ubuntu.com/releases/noble/release-20261017/ubuntu-24.04-server-cloudimg-amd64.img'
EXPECTED = '04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94'
root = Path('/opt/unetlab/addons/qemu/linux-nac24-base')
target = root / 'virtioa.qcow2'
part = root / 'virtioa.qcow2.part'
root.mkdir(exist_ok=True)
if target.exists():
    digest = hashlib.sha256()
    with target.open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            digest.update(chunk)
    observed = digest.hexdigest()
    if observed != EXPECTED:
        raise SystemExit('existing Noble image has unexpected SHA-256')
    print('NOBLE_IMAGE_READY', target, observed, flush=True)
    raise SystemExit(0)
if part.exists():
    raise SystemExit('partial image exists; inspect before retrying')

digest = hashlib.sha256()
received = 0
next_progress = time.monotonic() + 20
try:
    with urllib.request.urlopen(URL, timeout=60) as response, part.open('xb') as output:
        total = int(response.headers.get('Content-Length', '0'))
        while chunk := response.read(1024 * 1024):
            output.write(chunk)
            digest.update(chunk)
            received += len(chunk)
            if time.monotonic() >= next_progress:
                print('NOBLE_DOWNLOAD_MIB', round(received / 1048576), 'OF', round(total / 1048576), flush=True)
                next_progress = time.monotonic() + 20
    observed = digest.hexdigest()
    if observed != EXPECTED:
        raise SystemExit('official Noble image checksum mismatch: ' + observed)
    os.replace(part, target)
    print('NOBLE_IMAGE_READY', target, observed, flush=True)
except Exception:
    # Keep a partial image for inspection. Never publish it as an EVE image.
    raise
