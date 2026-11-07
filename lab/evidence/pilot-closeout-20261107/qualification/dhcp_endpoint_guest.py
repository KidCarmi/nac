"""Inspect and operate the isolated one-NIC DHCP endpoint through EVE serial."""
import os
from pathlib import Path
import re
import select
import socket
import time

ACTION = os.environ.get('DHCP_ENDPOINT_ACTION', '')
if ACTION not in {'inventory','valid_start','invalid_start','stop','probe','manual_renew','dhcp_logs','storage_repair'}:
    raise SystemExit('invalid DHCP_ENDPOINT_ACTION')
PASSWORD = Path('/root/nac-linux.rootpw').read_text().strip()

def read(sock, seconds):
    data=bytearray(); end=time.monotonic()+seconds
    while time.monotonic()<end:
        if select.select([sock],[],[],0.1)[0]:
            part=sock.recv(65536)
            if not part: break
            data.extend(part)
    return re.sub(r'\x1b\[[0-9;?]*[A-Za-z]','',data.decode(errors='replace')).replace(PASSWORD,'[REDACTED]').replace('\r','')

with socket.create_connection(('127.0.0.1',32800),timeout=5) as sock:
    sock.sendall(b'\r\n')
    if 'login:' in read(sock,0.8):
        sock.sendall(b'root\r\n'); read(sock,0.4)
        sock.sendall(PASSWORD.encode()+b'\r\n'); read(sock,1.2)
    def cmd(value, seconds=2):
        sock.sendall(value.encode()+b'\r\n')
        return read(sock,seconds)
    if ACTION=='inventory':
        print('CLOCK',cmd('date -u +%FT%T.%3NZ'))
        print('NETWORK',cmd('ls /sys/class/net; ip -br addr; ip route; resolvectl status ens3; networkctl status ens3 --no-pager',3))
        print('CERT',cmd('openssl x509 -in /root/nac-endpoint.pem -noout -serial -issuer -subject'))
        print('CONFIG',cmd('ls -l /root/wired-valid.conf /root/wired-invalid.conf /etc/netplan/*.yaml; wpa_cli -i ens3 status'))
    elif ACTION=='valid_start':
        print('CLOCK',cmd('date -u +%FT%T.%3NZ'))
        print('START',cmd('wpa_supplicant -i ens3 -D wired -c /root/wired-valid.conf -B -f /root/wpa-rc2-valid.log',2))
        print('SUPPLICANT',cmd('wpa_cli -i ens3 status',2))
    elif ACTION=='invalid_start':
        print('CLOCK',cmd('date -u +%FT%T.%3NZ'))
        print('STOP',cmd('wpa_cli -i ens3 terminate 2>&1 || true'))
        print('START',cmd('wpa_supplicant -i ens3 -D wired -c /root/wired-invalid.conf -B -f /root/wpa-rc2-invalid.log',2))
        print('SUPPLICANT',cmd('wpa_cli -i ens3 status',2))
    elif ACTION=='stop':
        print('CLOCK',cmd('date -u +%FT%T.%3NZ'))
        print('STOP',cmd('wpa_cli -i ens3 terminate 2>&1 || true'))
    elif ACTION=='probe':
        print('CLOCK',cmd('date -u +%FT%T.%3NZ'))
        print('NETWORK',cmd('ip -br addr show ens3; ip -4 route; resolvectl dns ens3'))
        for segment,ip in [('corp','10.70.10.10'),('quarantine','10.70.20.10')]:
            print(segment.upper(),cmd(f'if nc -vz -w2 {ip} 8080; then echo TCP_OPEN; else echo TCP_BLOCKED; fi',3.5))
    elif ACTION=='manual_renew':
        print('DIAGNOSTIC_MANUAL_RENEW_AT',cmd('date -u +%FT%T.%3NZ'))
        print('RENEW',cmd('networkctl renew ens3; sleep 3; ip -br addr show ens3; ip -4 route',5))
    elif ACTION=='dhcp_logs':
        print('NETWORKD',cmd('journalctl -u systemd-networkd --since "15 minutes ago" --no-pager | tail -n 100',3))
        print('SUPPLICANT',cmd('tail -n 80 /root/wpa-rc2-valid.log 2>/dev/null || true',2))
        print('INVALID_SUPPLICANT',cmd('tail -n 35 /root/wpa-rc2-invalid.log 2>/dev/null || true',2))
    elif ACTION=='storage_repair':
        print('BEFORE',cmd('df -h /; du -sh /var/cache/apt/archives /var/log/journal',3))
        print('APT_TIMERS',cmd('systemctl disable --now apt-daily.timer apt-daily-upgrade.timer',4))
        print('APT_CLEAN',cmd('apt-get clean',4))
        print('JOURNAL_DIR',cmd('install -d -m 0755 /etc/systemd/journald.conf.d',2))
        print('JOURNAL_LIMIT',cmd("printf '[Journal]\\nSystemMaxUse=16M\\nRuntimeMaxUse=16M\\n' > /etc/systemd/journald.conf.d/nac-lab-storage.conf",2))
        print('JOURNAL_ROTATE',cmd('journalctl --rotate --vacuum-size=16M',4))
        print('JOURNAL_RESTART',cmd('systemctl restart systemd-journald',4))
        print('AFTER',cmd('df -h /; du -sh /var/cache/apt/archives /var/log/journal; systemctl is-enabled apt-daily.timer apt-daily-upgrade.timer',3))
