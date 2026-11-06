"""Install a verified, free MikroTik CHR disk into EVE without touching other images.

Run inside EVE as root. The vendor image itself is not an evidence deliverable.
"""

from hashlib import sha256
from pathlib import Path
import os
import subprocess
import zipfile


VERSION = "7.24.6"
URL = f"https://download.mikrotik.com/routeros/{VERSION}/chr-{VERSION}.img.zip"
EXPECTED_ZIP_SHA256 = "692672e6cac9cfaa4f37c6334d07760d49e074b50befc61f0904270553f17126"
STAGING = Path("/root/nac-mikrotik-staging")
ZIP = STAGING / f"chr-{VERSION}.img.zip"
IMAGE = STAGING / f"chr-{VERSION}.img"
TARGET = Path(f"/opt/unetlab/addons/qemu/mikrotik-{VERSION}")
DISK = TARGET / "hda.qcow2"


def digest(path):
    value = sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


if TARGET.exists():
    raise SystemExit(f"Refusing to overwrite existing image directory: {TARGET}")
STAGING.mkdir(mode=0o700, parents=True, exist_ok=True)
if ZIP.exists() and digest(ZIP) != EXPECTED_ZIP_SHA256:
    raise SystemExit("Existing download has unexpected SHA-256")
if not ZIP.exists():
    subprocess.run(["curl", "-fL", "--retry", "3", "--max-time", "90", URL,
                    "-o", str(ZIP)], check=True)
actual = digest(ZIP)
if actual != EXPECTED_ZIP_SHA256:
    raise SystemExit(f"Vendor ZIP SHA-256 mismatch: {actual}")
with zipfile.ZipFile(ZIP) as archive:
    if archive.namelist() != [IMAGE.name]:
        raise SystemExit(f"Unexpected ZIP members: {archive.namelist()}")
    with archive.open(IMAGE.name) as source, IMAGE.open("xb") as target:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            target.write(chunk)
TARGET.mkdir(mode=0o755)
subprocess.run(["qemu-img", "convert", "-f", "raw", "-O", "qcow2",
                str(IMAGE), str(DISK)], check=True)
os.chmod(DISK, 0o644)
subprocess.run(["qemu-img", "info", str(DISK)], check=True)
print("CHR_VERSION", VERSION)
print("VENDOR_ZIP_SHA256", actual)
print("EVE_DISK_SHA256", digest(DISK))
