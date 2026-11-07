"""Replace only the disposable Core disk after recording the blocked test."""
from pathlib import Path
import subprocess

root=Path('/root/nac-qualification/windows-2025-endpoint').resolve()
disk=(root/'windows.qcow2').resolve()
if str(root)!='/root/nac-qualification/windows-2025-endpoint' or disk.parent!=root:
    raise SystemExit('unexpected disk location')
if (root/'qemu.pid').exists():
    raise SystemExit('VM still running')
info=subprocess.check_output(['qemu-img','info','--output=json',str(disk)],text=True)
if '"format": "qcow2"' not in info or disk.stat().st_size<2*1024**3:
    raise SystemExit('unexpected disposable disk')
disk.unlink()
subprocess.run(['qemu-img','create','-f','qcow2',str(disk),'32G'],check=True,capture_output=True)
print('DISPOSABLE_CORE_DISK_REPLACED_FOR_DESKTOP_INDEX_2')
print('NEW_DISK_BYTES',disk.stat().st_size)
