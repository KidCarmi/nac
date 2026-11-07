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
    parser.add_argument('--eapi-ca', action='store_true',
                        help='copy disposable eAPI public CA from EVE to guest')
    parser.add_argument('--eapi-observer-secret', action='store_true',
                        help='copy disposable observer secret from EVE to guest root only')
    parser.add_argument('--rc10-bundle', action='store_true',
                        help='copy pinned rc10 bundle/checksum from EVE to guest')
    parser.add_argument('--rc10', action='store_true',
                        help='connect to separate clean rc10 guest on EVE loopback 2231')
    parser.add_argument('--fresh-ui', action='store_true',
                        help='connect to new rc10 UI guest on EVE loopback 2233')
    parser.add_argument('--fresh-pki', action='store_true',
                        help='copy existing disposable RADIUS PKI only to fresh UI guest')
    parser.add_argument('--endpoint-pki', action='store_true',
                        help='copy the EVE endpoint-matched disposable RADIUS PKI to fresh UI guest')
    args = parser.parse_args()
    if args.rc10 and args.fresh_ui:
        parser.error('select one guest')
    if (args.fresh_pki or args.endpoint_pki) and not args.fresh_ui:
        parser.error('PKI staging requires --fresh-ui')
    eve = eve_connection()
    try:
        _, out, _ = eve.exec_command('cat /root/nac-linux.rootpw', timeout=10)
        password = out.read().decode().strip()
        if out.channel.recv_exit_status() or not password:
            raise RuntimeError('clean guest credential unavailable')
        sock = eve.get_transport().open_channel('direct-tcpip',
                      ('127.0.0.1', 2233 if args.fresh_ui else 2231 if args.rc10 else 2229),
                      ('127.0.0.1', 0), timeout=10)
        guest = paramiko.SSHClient()
        guest_keys = ROOT / ('fresh_ui_known_hosts' if args.fresh_ui else
                             'clean_rc10_known_hosts' if args.rc10 else 'clean_rc9_known_hosts')
        if guest_keys.exists():
            guest.load_host_keys(str(guest_keys))
            guest.set_missing_host_key_policy(paramiko.RejectPolicy())
        else:
            guest.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        guest.connect('fresh-ui-rc10' if args.fresh_ui else 'clean-rc10' if args.rc10 else 'clean-rc9',
                      username='root', password=password,
                      sock=sock, look_for_keys=False, allow_agent=False,
                      timeout=15, auth_timeout=15)
        password = None
        try:
            if not guest_keys.exists():
                guest.save_host_keys(str(guest_keys))
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
            if args.eapi_ca:
                with eve.open_sftp() as from_sftp, guest.open_sftp() as to_sftp:
                    with from_sftp.open('/root/nac-qualification/eapi-trusted-rc9-20261107/ca.crt', 'rb') as src:
                        with to_sftp.open('/root/nac-eapi-ca.crt', 'wb') as dst:
                            while data := src.read(65536):
                                dst.write(data)
                print('copied disposable eAPI public CA')
            if args.eapi_observer_secret:
                with eve.open_sftp() as from_sftp, guest.open_sftp() as to_sftp:
                    with from_sftp.open('/root/nac-pki/eapi-observer.secret', 'rb') as src:
                        with to_sftp.open('/root/nac-eapi-observer.secret', 'wb') as dst:
                            while data := src.read(65536):
                                dst.write(data)
                    to_sftp.chmod('/root/nac-eapi-observer.secret', 0o600)
                print('copied disposable eAPI observer secret without output')
            if args.rc10_bundle:
                source = '/root/nac-pilot-build/source-b39649f/dist/'
                with eve.open_sftp() as from_sftp, guest.open_sftp() as to_sftp:
                    for name in ('nac-pilot-pilot-rc10.tar.gz',
                                 'nac-pilot-pilot-rc10.tar.gz.sha256'):
                        with from_sftp.open(source + name, 'rb') as src:
                            with to_sftp.open('/root/' + name, 'wb') as dst:
                                while data := src.read(65536):
                                    dst.write(data)
                        print('copied', name)
            if args.fresh_pki:
                source = '/root/nac-qualification/fresh-ui-b39649f-20261107/pki/'
                _, out, err = guest.exec_command('install -d -m 0700 /root/lab-pki', timeout=10)
                if out.channel.recv_exit_status():
                    raise RuntimeError(err.read().decode(errors='replace'))
                with eve.open_sftp() as from_sftp, guest.open_sftp() as to_sftp:
                    for name in ('ca.pem', 'server.pem', 'server.key'):
                        with from_sftp.open(source + name, 'rb') as src:
                            with to_sftp.open('/root/lab-pki/' + name, 'wb') as dst:
                                while data := src.read(65536):
                                    dst.write(data)
                        to_sftp.chmod('/root/lab-pki/' + name, 0o600)
                        print('copied disposable', name, 'without output')
            if args.endpoint_pki:
                sources = {'ca.crt': 'ca.pem', 'server.crt': 'server.pem',
                           'server.key': 'server.key'}
                _, out, err = guest.exec_command('install -d -m 0700 /root/lab-pki/endpoint-matched', timeout=10)
                if out.channel.recv_exit_status():
                    raise RuntimeError(err.read().decode(errors='replace'))
                with eve.open_sftp() as from_sftp, guest.open_sftp() as to_sftp:
                    for source_name, dest_name in sources.items():
                        with from_sftp.open('/root/nac-pki/' + source_name, 'rb') as src:
                            dest = '/root/lab-pki/endpoint-matched/' + dest_name
                            with to_sftp.open(dest, 'wb') as dst:
                                while data := src.read(65536):
                                    dst.write(data)
                            to_sftp.chmod(dest, 0o600)
                        print('copied endpoint-matched disposable', dest_name, 'without output')
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
