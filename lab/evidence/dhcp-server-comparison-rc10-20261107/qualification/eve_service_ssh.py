"""Run a bounded command or upload on a lab service guest via a temporary EVE-only route.

The service guest's temporary SSH daemon must be started through its serial
console first. This helper removes the EVE bridge address and host route even
when the guest command fails. It does not print any password.
"""
import argparse
from pathlib import Path
import paramiko

ROOT = Path(__file__).resolve().parent.parent

def run(client, command):
    _, out, err = client.exec_command(command, timeout=90)
    body = out.read().decode(errors='replace')
    problem = err.read().decode(errors='replace')
    rc = out.channel.recv_exit_status()
    if rc:
        raise RuntimeError(f'command failed {rc}: {problem[-400:]} {body[-400:]}')
    return body.strip()

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('segment', choices=('corp', 'quarantine'))
    ap.add_argument('--command')
    ap.add_argument('--upload', nargs=2, metavar=('LOCAL', 'REMOTE'))
    ap.add_argument('--download', nargs=2, metavar=('REMOTE', 'LOCAL'))
    args = ap.parse_args()
    if not args.command and not args.upload and not args.download:
        ap.error('command, upload or download required')
    corp = args.segment == 'corp'
    bridge = 'vnet0_32' if corp else 'vnet0_33'
    prefix = '10.70.10' if corp else '10.70.20'
    alias = prefix + '.254/24'
    address = prefix + '.10'

    pve = paramiko.SSHClient()
    pve.load_host_keys('D:/AI/xirp-web-lab/proxmox_known_hosts')
    pve.connect('192.168.1.84', username='root',
                key_filename='C:/Users/Orel/.ssh/devops-training_ed25519',
                look_for_keys=False, allow_agent=False, timeout=10)
    try:
        eve_password = run(pve, 'cat /root/eve-nac-lab.rootpw')
    finally:
        pve.close()
    eve = paramiko.SSHClient()
    eve.load_host_keys(str(ROOT / 'eve_known_hosts'))
    eve.set_missing_host_key_policy(paramiko.RejectPolicy())
    eve.connect('192.168.1.125', username='root', password=eve_password,
                look_for_keys=False, allow_agent=False, timeout=10)
    eve_password = None
    try:
        guest_password = run(eve, 'cat /root/nac-linux.rootpw')
        run(eve, f'ip addr add {alias} dev {bridge}')
        try:
            run(eve, f'ip route add {address}/32 dev {bridge} src {prefix}.254')
            try:
                channel = eve.get_transport().open_channel(
                    'direct-tcpip', (address, 2222), ('127.0.0.1', 0))
                guest = paramiko.Transport(channel)
                guest.start_client(timeout=10)
                guest.auth_password('root', guest_password)
                try:
                    if args.upload:
                        source, target = args.upload
                        with paramiko.SFTPClient.from_transport(guest) as sftp:
                            sftp.put(str(Path(source)), target)
                        print('uploaded', Path(source).name, 'to', target)
                    if args.download:
                        source, target = args.download
                        with paramiko.SFTPClient.from_transport(guest) as sftp:
                            sftp.get(source, str(Path(target)))
                        print('downloaded', source, 'to', target)
                    if args.command:
                        session = guest.open_session()
                        session.exec_command(args.command)
                        output = session.makefile().read().decode(errors='replace')
                        error = session.makefile_stderr().read().decode(errors='replace')
                        rc = session.recv_exit_status()
                        print(output, end='')
                        if error:
                            print(error, end='')
                        if rc:
                            raise SystemExit(rc)
                finally:
                    guest.close()
            finally:
                run(eve, f'ip route del {address}/32 dev {bridge}')
        finally:
            run(eve, f'ip addr del {alias} dev {bridge}')
    finally:
        eve.close()

if __name__ == '__main__':
    main()
