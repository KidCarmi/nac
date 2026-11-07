"""Switch disposable Windows setup floppy from Core to Desktop Experience."""
from pathlib import Path
import shutil
import subprocess

root=Path('/root/nac-qualification/windows-2025-endpoint').resolve()
assert str(root)=='/root/nac-qualification/windows-2025-endpoint'
assert not (root/'qemu.pid').exists()
source=root/'Autounattend.xml'
backup=root/'Autounattend.core.xml'
if backup.exists(): raise SystemExit('Desktop reinstall already prepared')
xml=source.read_text()
assert '<Value>1</Value>' in xml and xml.count('<Value>1</Value>')==1
shutil.copy2(source,backup)
source.write_text(xml.replace('<Value>1</Value>','<Value>2</Value>',1))
source.chmod(0o600)
subprocess.run(['mcopy','-o','-i',str(root/'unattend.img'),str(source),'::Autounattend.xml'],check=True,capture_output=True)
print('WINDOWS_DESKTOP_EXPERIENCE_IMAGE_INDEX',2)
print('DISPOSABLE_CORE_INSTALL_REPLACED_AFTER_EVIDENCE')
