"""Run bounded commands in the isolated rc9 clean-install KVM overlay.

EVE's SSH host key is pinned by eve_guest.py. The nested guest is reached only
through EVE's loopback QEMU user-network forward; no credential is printed.
"""
import argparse
from pathlib import Path
import sys

import paramiko

ROOT = Path(__file__).resolve().parent.parent
GUEST_KEYS = ROOT / 'clean_rc9_known_hosts'


def eve_connection():
    pve = paramiko.SSHClient()
    pve.load_host_keys('D:/AI/xirp-web-lab/proxmox_known_hosts')
    pve.connect('192.168.1.84', username='root',
                key_filename='C:/Users/Orel/.ssh/devops-training_ed25519',
                look_for_keys=False, allow_agent=False, timeout=10)
    try:
        _, out, _ = pve.exec_command('cat /root/eve-nac-lab.rootpw', timeout=10)
        password = out.read().decode().strip()
        if out.channel.recv_exit_status():
            raise RuntimeError('EVE credential unavailable')
    finally:
        pve.close()
    eve = paramiko.SSHClient()
    eve.load_host_keys(str(ROOT / 'eve_known_hosts'))
    eve.connect('192.168.1.125', username='root', password=password,
                look_for_keys=False, allow_agent=False, timeout=10)
    return eve


def main():
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    parser = argparse.ArgumentParser()
    parser.add_argument('command')
    parser.add_argument('--script', type=Path)
    parser.add_argument('--bundle', action='store_true',
                        help='copy pinned rc9 bundle/checksum from EVE to guest')
    args = parser.parse_args()
    eve = eve_connection()
    try:
        _, out, _ = eve.exec_command('cat /root/nac-linux.rootpw', timeout=10)
        password = out.read().decode().strip()
        if out.channel.recv_exit_status() or not password:
            raise RuntimeError('clean guest credential unavailable')
        sock = eve.get_transport().open_channel('direct-tcpip',
                      ('127.0.0.1', 2229), ('127.0.0.1', 0), timeout=10)
        guest = paramiko.SSHClient()
        if GUEST_KEYS.exists():
            guest.load_host_keys(str(GUEST_KEYS))
            guest.set_missing_host_key_policy(paramiko.RejectPolicy())
        else:
            guest.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        guest.connect('clean-rc9', username='root', password=password,
                      sock=sock, look_for_keys=False, allow_agent=False,
                      timeout=15, auth_timeout=15)
        password = None
        try:
            if not GUEST_KEYS.exists():
                guest.save_host_keys(str(GUEST_KEYS))
            if args.bundle:
                source = '/root/nac-pilot-build/source-3e92642/dist/'
                with eve.open_sftp() as from_sftp, guest.open_sftp() as to_sftp:
                    for name in ('nac-pilot-pilot-rc9.tar.gz',
                                 'nac-pilot-pilot-rc9.tar.gz.sha256'):
                        with from_sftp.open(source + name, 'rb') as src:
                            with to_sftp.open('/root/' + name, 'wb') as dst:
                                while data := src.read(65536):
                                    dst.write(data)
                        print('copied', name)
            stdin, stdout, stderr = guest.exec_command(args.command, timeout=300)
            if args.script:
                stdin.write(args.script.read_text(encoding='utf-8'))
                stdin.channel.shutdown_write()
            result = stdout.read().decode(errors='replace')
            error = stderr.read().decode(errors='replace')
            code = stdout.channel.recv_exit_status()
            print(result, end='')
            if error:
                print(error, end='')
            if code:
                raise SystemExit(code)
        finally:
            guest.close()
    finally:
        eve.close()


if __name__ == '__main__':
    main()
