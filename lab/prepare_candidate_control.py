"""Make an isolated, clean Ubuntu 24.04 control-node template inside EVE.

The source is the checksum-verified official cloud image. This creates a new
template and a root-only console password; it does not install NAC or RADIUS.
"""
import hashlib
import os
from pathlib import Path
import secrets
import subprocess

images = Path('/opt/unetlab/addons/qemu')
source = images / 'linux-nac24-base/virtioa.qcow2'
expected = '04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94'
digest = hashlib.sha256()
with source.open('rb') as stream:
    while chunk := stream.read(1024 * 1024):
        digest.update(chunk)
if digest.hexdigest() != expected:
    raise SystemExit('official Ubuntu image checksum mismatch')
root = images / 'linux-nac24-control'
root.mkdir(exist_ok=True)
target = root / 'virtioa.qcow2'
marker = root / '.configured'
if marker.exists():
    print('CANDIDATE_CONTROL_READY', target, flush=True)
    raise SystemExit(0)
if target.exists():
    raise SystemExit('unconfigured candidate image exists; inspect before retrying')

password_file = Path('/root/nac-pilot-control.rootpw')
if not password_file.exists():
    password_file.write_text(secrets.token_urlsafe(32) + '\n')
    os.chmod(password_file, 0o600)
build = Path('/root/nac-pilot-build')
build.mkdir(mode=0o700, exist_ok=True)
netplan = build / '99-nac-pilot.yaml'
netplan.write_text('network:\n  version: 2\n  renderer: networkd\n  ethernets:\n    data:\n      match:\n        name: "en*"\n      addresses: [10.70.99.2/24]\n')

subprocess.run(['qemu-img', 'create', '-f', 'qcow2', str(target), '16G'], check=True, stdout=subprocess.DEVNULL)
subprocess.run(['virt-resize', '--expand', '/dev/sda1', str(source), str(target)], check=True, timeout=900, stdout=subprocess.DEVNULL)
subprocess.run([
    'virt-customize', '-a', str(target), '--hostname', 'nac-pilot-control',
    '--password', 'root:file:' + str(password_file),
    '--upload', str(netplan) + ':/etc/netplan/99-nac-pilot.yaml',
    '--touch', '/etc/cloud/cloud-init.disabled',
    '--run-command', 'rm -f /etc/netplan/50-cloud-init.yaml && truncate -s 0 /etc/machine-id && systemctl enable serial-getty@ttyS0.service',
], check=True, timeout=900, stdout=subprocess.DEVNULL)
marker.write_text('Ubuntu 24.04.5 clean control; no NAC or RADIUS installed; source SHA-256 ' + expected + '\n')
print('CANDIDATE_CONTROL_READY', target, flush=True)
