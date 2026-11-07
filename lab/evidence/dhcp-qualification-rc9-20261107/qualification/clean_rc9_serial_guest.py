"""Read the isolated clean-install VM's serial console without login."""
import select
import socket
import time

with socket.create_connection(('127.0.0.1', 32900), timeout=5) as sock:
    sock.sendall(b'\r\n')
    data = bytearray()
    deadline = time.monotonic() + 4
    while time.monotonic() < deadline:
        if select.select([sock], [], [], 0.2)[0]:
            part = sock.recv(65536)
            if not part:
                break
            data.extend(part)
    print(data.decode(errors='replace')[-4500:])
