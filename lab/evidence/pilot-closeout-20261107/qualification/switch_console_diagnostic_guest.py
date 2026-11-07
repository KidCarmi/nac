"""Read a few seconds from the isolated vEOS serial console after restart."""
import select
import socket
import time

with socket.create_connection(('127.0.0.1', 32799), timeout=5) as sock:
    sock.sendall(b'\r\n')
    end = time.monotonic() + 6
    result = bytearray()
    while time.monotonic() < end:
        if select.select([sock], [], [], 0.2)[0]:
            result.extend(sock.recv(65536))
    print('CONSOLE_BYTES', len(result))
    print('CONSOLE_TAIL', result.decode(errors='replace')[-700:].replace('\r',''))
    if b'login:' in result:
        sock.sendall(b'admin\r\n')
        end = time.monotonic() + 4
        reply = bytearray()
        while time.monotonic() < end:
            if select.select([sock], [], [], 0.2)[0]:
                reply.extend(sock.recv(65536))
        print('AFTER_ADMIN', reply.decode(errors='replace')[-500:].replace('\r',''))
