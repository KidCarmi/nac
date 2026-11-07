"""Run disposable Windows Server 2025 eval inside EVE; no Proxmox changes."""
import os
from pathlib import Path
import subprocess
import sys

root=Path('/root/nac-qualification/windows-2025-endpoint')
disk=root/'windows.qcow2'
pidfile=root/'qemu.pid'
iso=Path('/root/nac-qualification/windows-server-eval-nac.iso')
action=sys.argv[1] if len(sys.argv)==2 else ''
if action in ('install_start','test_start','diagnostic_start'):
    if pidfile.exists() or not disk.is_file():
        raise SystemExit('Windows disk missing or VM already running')
    available=int(subprocess.check_output(['df','-B1','--output=avail','/'],text=True).splitlines()[-1])
    # Windows was installed and qcow2-checked first. The ISO copy has been
    # removed; captures rotate and the endpoint probe is capped at 8 MiB.
    if available < 8*1024**3:
        raise SystemExit('EVE disk headroom <8GiB, refusing Windows VM')
    argv=['/usr/bin/qemu-system-x86_64','-enable-kvm','-cpu','host,vmx=off,svm=off',
          '-smp','2','-m','3072','-name','nac-windows-2025-eval',
          '-machine','type=pc,accel=kvm',
          '-drive',f'file={disk},if=ide,format=qcow2,cache=none',
          '-display','vnc=127.0.0.1:5',
          '-monitor',f'unix:{root / "monitor.sock"},server=on,wait=off',
          '-pidfile',str(pidfile),'-daemonize']
    if action=='install_start':
        argv.extend(['-drive',f'file={iso},media=cdrom,if=ide,index=2,readonly=on',
                     '-drive',f'file={root / "unattend.img"},if=floppy,format=raw,readonly=on',
                     '-boot','order=d',
                     '-netdev','user,id=setup,hostfwd=tcp:127.0.0.1:2235-:22',
                     '-device','e1000,netdev=setup,mac=52:54:00:90:01:01'])
    else:
        tap='nacwin0'
        if subprocess.run(['ip','link','show',tap],capture_output=True).returncode:
            subprocess.run(['ip','tuntap','add','dev',tap,'mode','tap'],check=True)
        subprocess.run(['ip','link','set',tap,'master','vnet0_31'],check=True)
        subprocess.run(['ip','link','set',tap,'up'],check=True)
        argv.extend(['-boot','order=c',
                     '-netdev',f'tap,id=data,ifname={tap},script=no,downscript=no',
                     '-device','e1000,netdev=data,mac=00:50:00:00:02:00'])
        if action=='diagnostic_start':
            floppy=root/'profile-fix.img'
            if not floppy.is_file(): raise SystemExit('diagnostic floppy missing')
            argv.extend(['-drive',f'file={floppy},if=floppy,format=raw,readonly=on'])
    subprocess.run(argv,check=True,timeout=20)
    print('WINDOWS_VM_STARTED',action,'pidfile',pidfile,'setup_nat_only' if action=='install_start' else 'one_data_nic_only')
elif action=='status':
    print('PIDFILE',pidfile.exists())
    print('DISK_BYTES',disk.stat().st_size)
    print('HOST_FREE_BYTES',subprocess.check_output(['df','-B1','--output=avail','/'],text=True).splitlines()[-1])
elif action=='stop':
    pid=int(pidfile.read_text())
    if 'nac-windows-2025-eval' not in Path(f'/proc/{pid}/cmdline').read_bytes().decode(errors='replace'):
        raise SystemExit('pid does not belong to disposable Windows VM')
    os.kill(pid,15)
    print('WINDOWS_VM_STOP_REQUESTED')
else:
    raise SystemExit('use install_start|test_start|diagnostic_start|status|stop')
