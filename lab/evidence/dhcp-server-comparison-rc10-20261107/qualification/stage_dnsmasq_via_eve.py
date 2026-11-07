"""Stage Ubuntu's dnsmasq-base on one isolated service guest via a temporary EVE bridge address.

The data bridge address is removed in finally. The package is copied from EVE,
not from the endpoint. This script does not start or stop the active DHCP daemon.
"""
import argparse
import hashlib
import io
from pathlib import Path

import paramiko

ROOT = Path(__file__).resolve().parent.parent
PKG = '/root/nac-qualification/dhcp-comparison-20261107/dnsmasq-base_2.91-0ubuntu0.22.04.2_amd64.deb'
EXPECTED = 'c999f2c92633d753edcdd67fb039f5866e9413500b2c764993cbb6bb26393309'

def run(client, cmd):
    _, out, err = client.exec_command(cmd, timeout=30)
    data = out.read().decode(errors='replace')
    problem = err.read().decode(errors='replace')
    rc = out.channel.recv_exit_status()
    if rc:
        raise RuntimeError(f'command failed {rc}: {cmd}: {problem[-200:]}')
    return data.strip()

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('segment', choices=('corp', 'quarantine'))
    args = ap.parse_args()
    corp = args.segment == 'corp'
    bridge = 'vnet0_32' if corp else 'vnet0_33'
    prefix = '10.70.10' if corp else '10.70.20'
    alias = prefix + '.254/24'
    service_ip = prefix + '.10'

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
        with eve.open_sftp() as sftp:
            with sftp.open(PKG, 'rb') as source:
                package = source.read()
        digest = hashlib.sha256(package).hexdigest()
        if digest != EXPECTED:
            raise RuntimeError('downloaded package digest mismatch')
        run(eve, f'ip addr add {alias} dev {bridge}')
        try:
            # An older, down EVE bridge may already own this /24. The /32
            # route keeps this temporary transfer on the intended topology.
            run(eve, f'ip route add {service_ip}/32 dev {bridge} src {prefix}.254')
            try:
                channel = eve.get_transport().open_channel(
                    'direct-tcpip', (service_ip, 2222), ('127.0.0.1', 0))
                guest = paramiko.Transport(channel)
                guest.start_client(timeout=10)
                guest.auth_password('root', guest_password)
                try:
                    sftp = paramiko.SFTPClient.from_transport(guest)
                    with sftp.open('/root/nac-dnsmasq-base.deb', 'wb') as dest:
                        dest.write(package)
                    with sftp.open('/etc/nac-dnsmasq.conf', 'wb') as dest:
                        dest.write((
                            'port=0\ninterface=ens3\nbind-interfaces\ndhcp-authoritative\n'
                            'log-dhcp\nlog-facility=/var/log/nac-dnsmasq.log\n'
                            'dhcp-leasefile=/var/lib/nac-dnsmasq.leases\n'
                            f'dhcp-range={prefix}.50,{prefix}.99,255.255.255.0,3600s\n'
                            f'dhcp-option=option:router,{prefix}.10\n'
                            f'dhcp-option=option:dns-server,{prefix}.10\n'
                            'dhcp-option=option:domain-name,nac-lab.test\n'
                        ).encode())
                    sftp.close()
                    session = guest.open_session()
                    session.exec_command('sha256sum /root/nac-dnsmasq-base.deb && '
                                         'dpkg -i /root/nac-dnsmasq-base.deb && '
                                         'dnsmasq --test --conf-file=/etc/nac-dnsmasq.conf && '
                                         'dnsmasq --version | head -1')
                    output = session.makefile().read().decode(errors='replace')
                    error = session.makefile_stderr().read().decode(errors='replace')
                    rc = session.recv_exit_status()
                    if rc:
                        raise RuntimeError(f'guest install failed {rc}: {error[-400:]}')
                    print(args.segment, output.strip(), 'package SHA-256', digest)
                finally:
                    guest.close()
            finally:
                run(eve, f'ip route del {service_ip}/32 dev {bridge}')
        finally:
            run(eve, f'ip addr del {alias} dev {bridge}')
    finally:
        eve.close()

if __name__ == '__main__':
    main()
