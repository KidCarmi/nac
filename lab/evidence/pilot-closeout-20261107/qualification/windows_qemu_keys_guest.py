"""Enter a disposable guest command via its local QEMU monitor; no extra NIC."""
from pathlib import Path
import select
import socket
import sys
import time

root=Path('/root/nac-qualification/windows-2025-endpoint')
action=sys.argv[1] if len(sys.argv)==2 else ''
if action not in {'unlock','login','runfix','shutdown'}: raise SystemExit('use unlock|login|runfix|shutdown')

def key_name(char):
    if 'a' <= char <= 'z' or '0' <= char <= '9': return char
    if 'A' <= char <= 'Z': return 'shift-'+char.lower()
    return {'-':'minus','_':'shift-minus',' ':'spc','.':'dot',
            ':':'shift-semicolon','\\':'backslash'}[char]

with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as sock:
    sock.settimeout(5)
    sock.connect(str(root/'monitor.sock'))
    sock.recv(65536)
    def send(value):
        sock.sendall(('sendkey '+value+' 100\n').encode())
        time.sleep(0.12)
        if select.select([sock],[],[],0.1)[0]:
            response=sock.recv(65536).decode(errors='replace')
            if 'invalid' in response.lower() or 'unknown' in response.lower():
                raise RuntimeError('QEMU key rejected: '+value)
    if action=='unlock':
        send('ctrl-alt-delete')
        print('SECURE_ATTENTION_SENT')
    elif action=='login':
        password=(root/'admin.secret').read_text().strip()
        for char in password: send(key_name(char))
        send('ret')
        password=None
        print('DISPOSABLE_GUEST_LOGIN_SENT_WITHOUT_LOGGING_CREDENTIAL')
    elif action=='runfix':
        send('meta_l-r')
        time.sleep(0.5)
        for char in 'powershell -NoExit -f A:\\FIX.PS1': send(key_name(char))
        send('ret')
        print('PROFILE_FIX_LAUNCH_SENT_NO_NETWORK_INTERFACE_ADDED')
    else:
        for char in 'shutdown /s /t 0': send(key_name(char) if char!='/' else 'slash')
        send('ret')
        print('GUEST_SHUTDOWN_COMMAND_SENT')
