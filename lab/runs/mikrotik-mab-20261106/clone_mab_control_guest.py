"""Clone stopped pilot control overlay for isolated MikroTik RADIUS testing.

The clone contains lab secrets. It stays root-only in EVE and is never exported.
"""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess


SOURCE = Path("/opt/unetlab/tmp/0/6352f88d-0447-46e5-938f-be6f55c3a5f1/15/virtioa.qcow2")
EXPECTED_BACKING = "/opt/unetlab/addons/qemu/linux-nac24-control-bios/virtioa.qcow2"
TARGET_DIR = Path("/opt/unetlab/addons/qemu/linux-nac24-mab-control")
TARGET = TARGET_DIR / "virtioa.qcow2"


def sha(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


if TARGET_DIR.exists():
    raise SystemExit(f"Refusing to overwrite {TARGET_DIR}")
if not SOURCE.is_file():
    raise SystemExit(f"Missing stopped source overlay: {SOURCE}")
info = json.loads(subprocess.check_output(["qemu-img", "info", "--output=json", str(SOURCE)]))
if info.get("full-backing-filename") != EXPECTED_BACKING or info.get("dirty-flag"):
    raise SystemExit("Unexpected source backing chain or dirty image")
TARGET_DIR.mkdir(mode=0o700)
shutil.copy2(SOURCE, TARGET)
os.chmod(TARGET, 0o600)
if sha(SOURCE) != sha(TARGET):
    raise SystemExit("Control clone hash mismatch")
print("CONTROL_CLONE_SHA256", sha(TARGET))
print("CONTROL_CLONE_BACKING", EXPECTED_BACKING)
