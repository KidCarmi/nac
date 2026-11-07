"""ACPI shutdown of only the disposable Windows evaluation VM."""
from pathlib import Path
import socket
import time

root=Path('/root/nac-qualification/windows-2025-endpoint')
pidfile=root/'qemu.pid'
pid=int(pidfile.read_text())
if 'nac-windows-2025-eval' not in Path(f'/proc/{pid}/cmdline').read_bytes().decode(errors='replace'):
    raise SystemExit('unexpected QEMU process')
with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as sock:
    sock.settimeout(5)
    sock.connect(str(root/'monitor.sock'))
    sock.recv(65536)
    sock.sendall(b'system_powerdown\n')
    print('ACPI_SHUTDOWN_REQUESTED',sock.recv(65536).decode(errors='replace')[-200:])
for _ in range(90):
    if not pidfile.exists():
        print('WINDOWS_VM_STOPPED')
        break
    time.sleep(1)
else:
    raise SystemExit('ACPI shutdown timeout; VM still running')
