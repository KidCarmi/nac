"""Stage a read-only diagnostic script floppy, without another network NIC."""
from pathlib import Path
import subprocess

root=Path('/root/nac-qualification/windows-2025-endpoint')
if (root/'qemu.pid').exists(): raise SystemExit('Windows VM still running')
source=root/'profile-fix.ps1'
floppy=root/'profile-fix.img'
if not source.is_file() or floppy.exists(): raise SystemExit('unexpected diagnostic file state')
subprocess.run(['truncate','-s','2880K',str(floppy)],check=True)
subprocess.run(['mkfs.vfat','-F','12',str(floppy)],check=True,capture_output=True)
subprocess.run(['mcopy','-i',str(floppy),str(source),'::FIX.PS1'],check=True,capture_output=True)
floppy.chmod(0o600)
print('DIAGNOSTIC_FLOPPY_READY',floppy.stat().st_size,'NO_NETWORK_INTERFACE_ADDED')
