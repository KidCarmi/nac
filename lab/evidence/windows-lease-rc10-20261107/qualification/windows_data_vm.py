"""Read a Windows endpoint over its sole authorized data NIC, then remove probe IP.

The temporary EVE address is on the selected service segment only. It does not
route between segments and is removed in finally, including on SSH failure.
"""
import argparse
import base64
from pathlib import Path
import sys

import paramiko
from clean_rc9_vm import eve_connection

BASE=Path(__file__).resolve().parent.parent
KEYS=BASE/'windows_vm_known_hosts'
ROOT='/root/nac-qualification/windows-2025-endpoint'
SEGMENTS={'corp':('vnet0_32','10.70.10.254/24','10.70.10.80'),
          'quarantine':('vnet0_33','10.70.20.254/24','10.70.20.80')}

def remote(eve,command):
    _,stdout,stderr=eve.exec_command(command,timeout=20)
    output=stdout.read().decode(errors='replace')
    error=stderr.read().decode(errors='replace')
    if stdout.channel.recv_exit_status(): raise RuntimeError(command+': '+error+output)
    return output

def main():
    sys.stdout.reconfigure(encoding='utf-8',errors='replace')
    p=argparse.ArgumentParser()
    p.add_argument('--segment',choices=SEGMENTS,required=True)
    p.add_argument('--command')
    p.add_argument('--fetch',action='store_true')
    p.add_argument('--stage-hold',action='store_true')
    p.add_argument('--fetch-hold',action='store_true')
    p.add_argument('--out',type=Path)
    a=p.parse_args()
    if not (a.command or a.fetch or a.stage_hold or a.fetch_hold): p.error('operation required')
    if (a.fetch or a.fetch_hold) and not a.out: p.error('--out required for fetch')
    bridge,address,target=SEGMENTS[a.segment]
    eve=eve_connection()
    added=False
    try:
        if remote(eve,'sysctl -n net.ipv4.ip_forward').strip()!='0':
            raise RuntimeError('EVE forwarding enabled')
        if remote(eve,f'ip -4 -o addr show dev {bridge}').strip():
            raise RuntimeError('segment bridge already has host IP')
        remote(eve,f'ip addr add {address} dev {bridge}')
        added=True
        with eve.open_sftp() as sftp:
            with sftp.open(ROOT+'/admin.secret','rb') as source:
                password=source.read().decode().strip()
        channel=eve.get_transport().open_channel('direct-tcpip',(target,22),('127.0.0.1',0),timeout=10)
        win=paramiko.SSHClient()
        win.load_host_keys(str(KEYS))
        win.set_missing_host_key_policy(paramiko.RejectPolicy())
        win.connect('nac-windows-setup',username='Administrator',password=password,
                    sock=channel,look_for_keys=False,allow_agent=False,timeout=12,auth_timeout=12)
        password=None
        try:
            if a.stage_hold:
                with win.open_sftp() as sftp:
                    sftp.put(str(Path(__file__).with_name('windows_hold_probe.ps1')),
                             'C:/NACLab/hold-probe.ps1')
                print('HOLD_PROBE_STAGED')
            if a.command:
                encoded=base64.b64encode(a.command.encode('utf-16le')).decode()
                _,stdout,stderr=win.exec_command('powershell.exe -NoProfile -NonInteractive -EncodedCommand '+encoded,timeout=90)
                output=stdout.read().decode(errors='replace')
                error=stderr.read().decode(errors='replace')
                print(output,end='')
                if error:print(error,end='')
                if stdout.channel.recv_exit_status(): raise SystemExit('Windows command failed')
            if a.fetch:
                a.out.parent.mkdir(parents=True,exist_ok=True)
                with win.open_sftp() as sftp:
                    sftp.get('C:/NACLab/probe.jsonl',str(a.out))
                print('PROBE_FETCHED',a.out,'bytes',a.out.stat().st_size)
            if a.fetch_hold:
                a.out.parent.mkdir(parents=True,exist_ok=True)
                with win.open_sftp() as sftp:
                    sftp.get('C:/NACLab/hold-probe.jsonl',str(a.out))
                print('HOLD_PROBE_FETCHED',a.out,'bytes',a.out.stat().st_size)
        finally:
            win.close()
    finally:
        if added: print('TEMP_IP_REMOVED',remote(eve,f'ip addr del {address} dev {bridge}').strip() or address)
        eve.close()

if __name__=='__main__': main()
