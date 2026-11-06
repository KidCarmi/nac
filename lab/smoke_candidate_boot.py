"""Boot the clean candidate image briefly with no NIC, using a disposable overlay.

This does not start the EVE topology or modify its image/template.
"""
import os
from pathlib import Path
import subprocess
import time

firmware = os.environ.get('BOOT_FIRMWARE', 'bios')
if firmware not in {'bios', 'uefi'}:
    raise SystemExit('BOOT_FIRMWARE must be bios or uefi')
base = Path('/opt/unetlab/addons/qemu/linux-nac24-control/virtioa.qcow2')
overlay = Path('/root/nac-pilot-boot-smoke-' + firmware + '.qcow2')
log = Path('/root/nac-pilot-boot-smoke-' + firmware + '.log')
if overlay.exists():
    raise SystemExit('boot-smoke overlay already exists; inspect before replacing')
subprocess.run(['qemu-img','create','-f','qcow2','-F','qcow2','-b',str(base),str(overlay)],check=True,stdout=subprocess.DEVNULL)
with log.open('wb') as stream:
    command = [
        'qemu-system-x86_64', '-machine', 'type=pc,accel=kvm', '-cpu', 'host',
        '-m', '1024', '-smp', '1', '-drive', 'file='+str(overlay)+',format=qcow2,if=virtio',
        '-net', 'none', '-nographic', '-serial', 'mon:stdio', '-no-reboot',
    ]
    if firmware == 'uefi':
        command.extend(['-bios', '/usr/share/OVMF/OVMF_CODE.fd'])
    proc = subprocess.Popen(command,stdin=subprocess.DEVNULL,stdout=stream,stderr=subprocess.STDOUT)
    try:
        proc.wait(timeout=75)
    except subprocess.TimeoutExpired:
        proc.terminate()
        try: proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
text = log.read_text(errors='replace')
print('BOOT_SMOKE_BYTES', log.stat().st_size)
print('BOOT_SMOKE_LOGIN_PROMPT', 'login:' in text)
print('BOOT_SMOKE_UBUNTU_24', 'Ubuntu 24.04' in text)
print('BOOT_SMOKE_TAIL', repr(text[-900:]))
