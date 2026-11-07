"""Capture a read-only VNC framebuffer snapshot from disposable Windows VM."""
from pathlib import Path
import socket
import time

root = Path('/root/nac-qualification/windows-2025-endpoint')
dest = root / 'install-progress.ppm'
with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
    sock.settimeout(5)
    sock.connect(str(root / 'monitor.sock'))
    sock.recv(65536)
    sock.sendall(f'screendump {dest}\n'.encode())
    time.sleep(1)
    print(sock.recv(65536).decode(errors='replace')[-500:])
print('SCREENSHOT_BYTES', dest.stat().st_size)
