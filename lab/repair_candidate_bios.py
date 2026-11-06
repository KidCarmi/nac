"""Clone the clean pilot control image and reinstall BIOS GRUB after resize.

The original official base and UEFI template are left intact. This creates a
separate BIOS-bootable candidate template if the repair succeeds.
"""
from pathlib import Path
import shutil
import subprocess

images = Path('/opt/unetlab/addons/qemu')
source = images/'linux-nac24-control/virtioa.qcow2'
root = images/'linux-nac24-control-bios'
root.mkdir(exist_ok=True)
target = root/'virtioa.qcow2'
marker = root/'.configured'
if marker.exists():
    print('BIOS_CONTROL_READY',target,flush=True)
    raise SystemExit(0)
if target.exists():
    raise SystemExit('unconfigured BIOS candidate image exists; inspect before retrying')
shutil.copyfile(source,target)
result = subprocess.run([
    'virt-customize','-a',str(target),'--run-command',
    'grub-install --target=i386-pc --recheck /dev/sda && update-grub',
],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=900)
print('GRUB_REPAIR_EXIT',result.returncode,'TAIL',result.stdout[-1800:],flush=True)
if result.returncode:
    raise SystemExit(result.returncode)
marker.write_text('Ubuntu 24.04.5 clean control; BIOS GRUB reinstalled after virt-resize; no NAC or RADIUS installed\n')
print('BIOS_CONTROL_READY',target,flush=True)
