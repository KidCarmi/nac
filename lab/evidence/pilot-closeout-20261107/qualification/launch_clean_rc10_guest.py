"""Prepare and run a separate rc10 clean-install overlay on EVE's isolated control LAN."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

root = Path('/root/nac-qualification/clean-rc10-20261107')
overlay = root / 'control-overlay.qcow2'
pidfile = root / 'qemu.pid'
tap = 'nacclean10'
action = sys.argv[1] if len(sys.argv) == 2 else ''
if action == 'prepare':
    if root.exists():
        raise SystemExit('rc10 target already exists')
    root.mkdir(mode=0o700)
    base = '/opt/unetlab/addons/qemu/linux-nac24-base/virtioa.qcow2'
    subprocess.run(['qemu-img','create','-f','qcow2','-F','qcow2','-b',base,str(overlay),'12G'],check=True)
    with tempfile.TemporaryDirectory(prefix='nac-rc10-net-') as temporary:
        staging=Path(temporary)
        network=staging/'network'; network.mkdir()
        (network/'05-nac-control.network').write_text(
            '[Match]\nMACAddress=52:54:00:99:00:04\n\n[Network]\nAddress=10.70.99.4/24\nDHCP=no\nLinkLocalAddressing=no\n')
        (network/'10-nac-clean.network').write_text(
            '[Match]\nName=en*\n\n[Network]\nDHCP=yes\n\n[DHCPv4]\nUseDNS=yes\n')
        ssh=staging/'ssh'; ssh.mkdir()
        (ssh/'01-nac-clean.conf').write_text('PasswordAuthentication yes\nPermitRootLogin yes\n')
        subprocess.run(['virt-customize','-a',str(overlay),
                        '--hostname','nac-clean-rc10',
                        '--copy-in',f'{network}/05-nac-control.network:/etc/systemd/network',
                        '--copy-in',f'{network}/10-nac-clean.network:/etc/systemd/network',
                        '--copy-in',f'{ssh}/01-nac-clean.conf:/etc/ssh/sshd_config.d'],
                       check=True,timeout=180,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    print('RC10_CLEAN_OVERLAY_PREPARED',overlay)
elif action == 'start':
    if pidfile.exists() or not overlay.is_file():
        raise SystemExit('overlay missing or VM already has a pidfile')
    if subprocess.run(['ip','link','show',tap],capture_output=True).returncode:
        subprocess.run(['ip','tuntap','add','dev',tap,'mode','tap'],check=True)
    subprocess.run(['ip','link','set',tap,'master','vnet0_34'],check=True)
    subprocess.run(['ip','link','set',tap,'up'],check=True)
    argv=['/usr/bin/qemu-system-x86_64','-enable-kvm','-cpu','host,vmx=off,svm=off',
          '-smp','2','-m','2048','-name','nac-clean-rc10',
          '-drive',f'file={overlay},if=virtio,format=qcow2,cache=none',
          '-netdev','user,id=n0,hostfwd=tcp:127.0.0.1:2231-:22,hostfwd=tcp:127.0.0.1:18449-:8443',
          '-device','virtio-net-pci,netdev=n0,mac=52:54:00:99:00:03',
          '-netdev',f'tap,id=n1,ifname={tap},script=no,downscript=no',
          '-device','virtio-net-pci,netdev=n1,mac=52:54:00:99:00:04',
          '-display','none','-serial','telnet:127.0.0.1:2232,server=on,wait=off',
          '-monitor',f'unix:{root / "monitor.sock"},server=on,wait=off',
          '-pidfile',str(pidfile),'-daemonize']
    subprocess.run(argv,check=True,timeout=20)
    print('RC10_CLEAN_VM_STARTED ssh_forward=2231 console_forward=18449 control=10.70.99.4')
elif action == 'status':
    print('PIDFILE',pidfile.exists())
    print('TAP',subprocess.run(['ip','-br','link','show',tap],capture_output=True,text=True).stdout.strip())
elif action == 'stop':
    pid=int(pidfile.read_text())
    if 'nac-clean-rc10' not in Path(f'/proc/{pid}/cmdline').read_bytes().decode(errors='replace'):
        raise SystemExit('pid is not rc10')
    os.kill(pid,15)
    print('RC10_CLEAN_VM_STOP_REQUESTED')
else:
    raise SystemExit('use prepare|start|status|stop')
