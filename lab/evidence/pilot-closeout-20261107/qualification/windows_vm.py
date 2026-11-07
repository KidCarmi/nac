"""Run PowerShell in disposable Windows Server setup VM over EVE loopback SSH.

Never prints credentials. During qualification, test_start boots without this
management path, so this tool is deliberately unavailable to the test NIC.
"""
import argparse
import base64
from pathlib import Path
import sys
import paramiko

from clean_rc9_vm import eve_connection

ROOT=Path(__file__).resolve().parent.parent
KEYS=ROOT/'windows_vm_known_hosts'
VMROOT='/root/nac-qualification/windows-2025-endpoint'

def main():
    sys.stdout.reconfigure(encoding='utf-8',errors='replace')
    parser=argparse.ArgumentParser()
    parser.add_argument('--script',type=Path)
    parser.add_argument('--command')
    parser.add_argument('--stage-pki',action='store_true')
    parser.add_argument('--stage-probe',action='store_true')
    args=parser.parse_args()
    if not (args.script or args.command or args.stage_pki or args.stage_probe): parser.error('operation required')
    eve=eve_connection()
    try:
        with eve.open_sftp() as sftp:
            with sftp.open(VMROOT+'/admin.secret','r') as source:
                password=source.read().decode().strip()
        channel=eve.get_transport().open_channel('direct-tcpip',('127.0.0.1',2235),('127.0.0.1',0),timeout=10)
        win=paramiko.SSHClient()
        if KEYS.exists():
            win.load_host_keys(str(KEYS))
            win.set_missing_host_key_policy(paramiko.RejectPolicy())
        else:
            win.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        win.connect('nac-windows-setup',username='Administrator',password=password,sock=channel,
                    look_for_keys=False,allow_agent=False,timeout=15,auth_timeout=15)
        password=None
        try:
            if not KEYS.exists(): win.save_host_keys(str(KEYS))
            def powershell(code):
                encoded=base64.b64encode(code.encode('utf-16le')).decode()
                stdin,stdout,stderr=win.exec_command('powershell.exe -NoProfile -NonInteractive -EncodedCommand '+encoded,timeout=120)
                output=stdout.read().decode('utf-8',errors='replace')
                error=stderr.read().decode('utf-8',errors='replace')
                status=stdout.channel.recv_exit_status()
                print(output,end='')
                if error: print(error,end='')
                if status: raise SystemExit(status)
            if args.stage_pki:
                powershell("New-Item -ItemType Directory -Force -Path 'C:\\NACLab' | Out-Null")
                with eve.open_sftp() as source, win.open_sftp() as target:
                    for src,dest in [(VMROOT+'/wired-client.pfx','wired-client.pfx'),
                                     (VMROOT+'/pfx.secret','pfx.secret'),
                                     ('/root/nac-pki/ca.crt','ca.crt')]:
                        with source.open(src,'rb') as incoming:
                            with target.open('C:/NACLab/'+dest,'wb') as outgoing:
                                while chunk:=incoming.read(65536): outgoing.write(chunk)
                        print('STAGED',dest,'disposable Windows guest only')
            if args.stage_probe:
                powershell("New-Item -ItemType Directory -Force -Path 'C:\\NACLab' | Out-Null")
                with win.open_sftp() as target:
                    target.put(str(Path(__file__).with_name('windows_dhcp_probe.ps1')),
                               'C:/NACLab/probe.ps1')
                print('STAGED bounded Windows DHCP probe')
            if args.script: powershell(args.script.read_text(encoding='utf-8'))
            if args.command: powershell(args.command)
        finally:
            win.close()
    finally:
        eve.close()

if __name__=='__main__': main()
