"""Prepare or restore a recoverable node-35 overlay in the isolated vEOS lab."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys

import arista_rc4_eve_guest as lab

DISK_DIR = Path('/opt/unetlab/tmp/0/f763a592-ae3e-4bec-93ef-5f5bd5f78c18/35')
ACTIVE = DISK_DIR / 'virtioa.qcow2'
BASELINE = DISK_DIR / 'virtioa-baseline-20261110.qcow2'
CANDIDATE = DISK_DIR / 'virtioa-rc4-20261110.qcow2'


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as file:
        for chunk in iter(lambda: file.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def check_stopped():
    actual = hashlib.sha256(lab.LAB.read_bytes()).hexdigest()
    if actual != lab.EXPECTED_HASH:
        raise RuntimeError('topology hash mismatch')
    lab.api('/auth/login', 'POST', {'username': 'admin',
        'password': Path('/root/eve-nac-lab.webpw').read_text().strip()})
    state = lab.snapshot()
    if any(value != 0 for value in state.values()):
        raise RuntimeError(f'vEOS nodes must all be stopped: {state}')
    if DISK_DIR.resolve() != Path('/opt/unetlab/tmp/0/f763a592-ae3e-4bec-93ef-5f5bd5f78c18/35'):
        raise RuntimeError('unexpected disk path')


def main():
    action = sys.argv[1]
    check_stopped()
    if action == 'prepare':
        if not ACTIVE.is_file() or BASELINE.exists() or CANDIDATE.exists():
            raise RuntimeError('unexpected overlay state before prepare')
        original = digest(ACTIVE)
        os.replace(ACTIVE, BASELINE)
        try:
            subprocess.run(['qemu-img', 'create', '-f', 'qcow2', '-F', 'qcow2',
                '-b', BASELINE.name, str(ACTIVE)], check=True, capture_output=True)
            ACTIVE.chmod(BASELINE.stat().st_mode)
            os.chown(ACTIVE, BASELINE.stat().st_uid, BASELINE.stat().st_gid)
        except Exception:
            ACTIVE.unlink(missing_ok=True)
            os.replace(BASELINE, ACTIVE)
            raise
        print(f'prepared: baseline_sha256={original} candidate_overlay={ACTIVE}')
    elif action == 'reactivate':
        if not ACTIVE.is_file() or BASELINE.exists() or not CANDIDATE.is_file():
            raise RuntimeError('unexpected overlay state before reactivate')
        original = digest(ACTIVE)
        if original != '8d51b4fc45307ede0a0e39eb516df2815550fbba76a04e4b0a5e806f1d660c1d':
            raise RuntimeError('original baseline hash mismatch')
        os.replace(ACTIVE, BASELINE)
        os.replace(CANDIDATE, ACTIVE)
        print(f'reactivated: baseline_sha256={original} candidate_overlay={ACTIVE}')
    elif action == 'restore':
        if not ACTIVE.is_file() or not BASELINE.is_file() or CANDIDATE.exists():
            raise RuntimeError('unexpected overlay state before restore')
        original = digest(BASELINE)
        os.replace(ACTIVE, CANDIDATE)
        os.replace(BASELINE, ACTIVE)
        if digest(ACTIVE) != original:
            raise RuntimeError('baseline hash changed')
        print(f'restored: baseline_sha256={original} preserved_candidate={CANDIDATE}')
    else:
        raise SystemExit('prepare|reactivate|restore')


if __name__ == '__main__':
    main()
