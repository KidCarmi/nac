"""Stage and switch disposable eAPI TLS certs on the isolated vEOS lab node."""
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import os
from pathlib import Path
import select
import shutil
import socket
import subprocess
import tempfile
from threading import Thread
import time

action = os.environ.get('EAPI_TLS_ACTION', '')
root = Path('/root/nac-qualification/eapi-trusted-rc9-20261107')
if not root.is_dir() or action not in {'stage','good','mismatch','restore','status','files','help','harden'}:
    raise SystemExit('invalid TLS action or missing disposable certificates')
if subprocess.check_output(['sysctl','-n','net.ipv4.ip_forward'], text=True).strip() != '0':
    raise SystemExit('EVE forwarding must remain disabled')

def read(sock, seconds):
    data=bytearray(); deadline=time.monotonic()+seconds
    while time.monotonic()<deadline:
        if select.select([sock],[],[],0.1)[0]:
            piece=sock.recv(65536)
            if not piece: break
            data.extend(piece)
    return data.decode(errors='replace').replace('\r','')

class OnlySwitch(SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.client_address[0] != '10.70.99.1':
            self.send_error(403)
            return
        super().do_GET()
    def log_message(self, fmt, *args):
        pass

with socket.create_connection(('127.0.0.1',32799),timeout=5) as sock:
    sock.sendall(b'\r\n')
    greeting=read(sock,4)
    if 'login:' in greeting:
        sock.sendall(b'admin\r\n'); greeting+=read(sock,3)
    if 'nac-veos-rc1>' not in greeting and 'nac-veos-rc1#' not in greeting:
        raise RuntimeError('isolated EOS console did not reach an authenticated prompt')
    def cmd(value, seconds=1.2):
        sock.sendall(value.encode()+b'\r\n')
        body=read(sock,seconds)
        if '% Invalid' in body or '% Error' in body or '% Ambiguous' in body:
            raise RuntimeError(f'EOS command failed: {value}: {body[-500:]}')
        return body
    cmd('enable')
    cmd('terminal length 0')
    if action=='help':
        cmd('configure terminal'); cmd('management security'); cmd('ssl profile nac-eapi')
        sock.sendall(b'certificate ?\r\n')
        print('CERTIFICATE_HELP', read(sock, 2)[-1000:])
        sock.sendall(b'certificate nac-eapi.crt ?\r\n')
        print('KEY_HELP', read(sock, 2)[-1000:])
        exit()
    if action=='stage':
        with tempfile.TemporaryDirectory(prefix='nac-eapi-stage-') as directory:
            for name in ('eapi.crt','eapi.key','mismatch.crt'):
                shutil.copyfile(root / name, Path(directory) / name)
            server=ThreadingHTTPServer(('10.70.99.254',18091),
                partial(OnlySwitch,directory=directory))
            thread=Thread(target=server.serve_forever,daemon=True)
            thread.start()
            try:
                for name in ('eapi.crt','eapi.key','mismatch.crt'):
                    output=cmd(f'copy http://10.70.99.254:18091/{name} flash:nac-{name}',4)
                    if 'Copy completed successfully' not in output:
                        raise RuntimeError(f'copy not confirmed for {name}: {output[-450:]}')
            finally:
                server.shutdown(); thread.join(timeout=3); server.server_close()
        print('STAGED_TLS_CERTS_AND_KEY_ON_SWITCH; temporary HTTP server stopped')
    elif action in {'good','mismatch'}:
        cert='nac-eapi.crt' if action=='good' else 'nac-mismatch.crt'
        imports = []
        if not (root / f'.{cert}-imported').exists():
            imports.append((cert,f'certificate:{cert}'))
        if action == 'good' and not (root / '.eapi-key-imported').exists():
            imports.append(('nac-eapi.key','sslkey:nac-eapi.key'))
        for source, destination in imports:
            copied=cmd(f'copy flash:{source} {destination}',3)
            if 'Copy completed successfully' not in copied and 'copied' not in copied.lower():
                print('IMPORT_OUTPUT',copied[-450:])
        if action == 'good':
            (root / '.eapi-key-imported').touch()
        (root / f'.{cert}-imported').touch()
        cmd('configure terminal')
        cmd('management security')
        cmd('ssl profile nac-eapi')
        cmd('tls versions 1.2')
        cmd(f'certificate {cert} key nac-eapi.key')
        cmd('exit')
        cmd('management api http-commands')
        cmd('protocol https ssl profile nac-eapi')
        cmd('end')
        print('APPLIED', action)
    elif action=='harden':
        cmd('configure terminal')
        cmd('management security')
        cmd('ssl profile nac-eapi')
        cmd('cipher-list ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256')
        cmd('end')
        print('APPLIED_ECDHE_ONLY_CIPHER_LIST')
    elif action=='restore':
        cmd('configure terminal')
        cmd('management api http-commands')
        cmd('no protocol https ssl profile nac-eapi')
        cmd('end')
        print('RESTORED_DEFAULT_EAPI_PROFILE')
    if action == 'files':
        print('FILES', '\n'.join(line for line in cmd('dir flash:',2).splitlines()
                                  if 'nac-' in line)[-600:])
    elif action != 'stage':
        print('PROFILE',cmd('show management security ssl profile nac-eapi',2)[-1000:])
    else:
        print('FILES', '\n'.join(line for line in cmd('dir flash:',2).splitlines()
                                  if 'nac-' in line)[-600:])
    print('EAPI',cmd('show management api http-commands',2)[-750:])
