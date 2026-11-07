"""Operate only the two disposable DHCP service guests over EVE serial consoles.

Usage from eve_guest.py: DHCP_COMPARE_ACTION=inspect|timers|start|restore
DHCP_COMPARE_PORT=32801|32802 python3 - --script this-file.
Never prints the lab console password or package bytes.
"""
import base64
import os
from pathlib import Path
import re
import secrets
import select
import socket
import time

PORT = int(os.environ['DHCP_COMPARE_PORT'])
ACTION = os.environ['DHCP_COMPARE_ACTION']
if PORT not in (32801, 32802) or ACTION not in ('inspect', 'timers', 'start', 'restore'):
    raise SystemExit('invalid target/action')
SUBNET = '10.70.10' if PORT == 32801 else '10.70.20'
PKG = Path('/root/nac-qualification/dhcp-comparison-20261107/dnsmasq-base_2.91-0ubuntu0.22.04.2_amd64.deb')
PASSWORD = Path('/root/nac-linux.rootpw').read_text().strip()

def read(sock, seconds):
    data = bytearray()
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        if select.select([sock], [], [], .1)[0]:
            part = sock.recv(65536)
            if not part:
                break
            data.extend(part)
    return bytes(data)

class Console:
    def __init__(self):
        self.sock = socket.create_connection(('127.0.0.1', PORT), timeout=5)
        self.sock.sendall(b'\r\n')
        greeting = read(self.sock, .7)
        if b'login:' in greeting:
            self.sock.sendall(b'root\r\n')
            read(self.sock, .4)
            self.sock.sendall(PASSWORD.encode() + b'\r\n')
            read(self.sock, 1)
        self.sock.sendall(b'stty -echo\r\n')
        read(self.sock, .3)

    def run(self, cmd, timeout=30):
        token = secrets.token_hex(8)
        begin, end = f'BEGIN_{token}'.encode(), f'END_{token}'.encode()
        self.sock.sendall((f"printf '{begin.decode()}\\n'; {cmd}; rc=$?; "
                           f"printf '{end.decode()} %s\\n' \"$rc\"\r\n").encode())
        buf = bytearray()
        until = time.monotonic() + timeout
        while time.monotonic() < until:
            if select.select([self.sock], [], [], .2)[0]:
                part = self.sock.recv(65536)
                if not part:
                    break
                buf.extend(part)
                if end in buf and b'\n' in buf.split(end, 1)[1]:
                    break
        raw = re.sub(rb'\x1b\[[0-9;?]*[A-Za-z]', b'', bytes(buf).replace(b'\r', b''))
        lines = raw.split(b'\n')
        first = next((i for i, x in enumerate(lines) if x.strip().endswith(begin)), None)
        last = next((i for i, x in enumerate(lines) if end + b' ' in x), None)
        if first is None or last is None:
            raise RuntimeError('console response marker missing: ' + repr(raw[-300:]))
        body = b'\n'.join(lines[first+1:last]).decode(errors='replace').replace(PASSWORD, '[REDACTED]')
        rc = int(lines[last].split(end + b' ', 1)[1].split()[0])
        if rc:
            raise RuntimeError(f'guest command failed {rc}: {body[-600:]}')
        return body.strip()

    def file(self, path, data):
        payload = base64.b64encode(data).decode()
        # Keep each terminal input line short; the QEMU serial console is a tty.
        lines = '\n'.join(payload[i:i+76] for i in range(0, len(payload), 76))
        self.run(f"base64 -d > {path} <<'NAC_PACKAGE_END'\n{lines}\nNAC_PACKAGE_END", timeout=120)

    def close(self):
        self.sock.sendall(b'stty echo\r\n')
        read(self.sock, .2)
        self.sock.close()

c = Console()
try:
    if ACTION == 'inspect':
        print(c.run("date -u +%FT%TZ; hostname; df -h /; systemctl is-active nac-udhcpd; "
                    "cat /etc/nac-udhcpd.conf; dpkg-query -W dnsmasq-base 2>/dev/null || true; "
                    "ss -lunp | grep ':67 ' || true"))
    elif ACTION == 'stage':
        if not PKG.is_file():
            raise SystemExit('package missing on EVE guest')
        print('package sha256 on EVE', __import__('hashlib').sha256(PKG.read_bytes()).hexdigest())
        c.file('/root/nac-dnsmasq-base.deb', PKG.read_bytes())
        print('guest package', c.run('sha256sum /root/nac-dnsmasq-base.deb; dpkg -i /root/nac-dnsmasq-base.deb; dnsmasq --version | head -1', timeout=60))
        config = ('port=0\ninterface=ens3\nbind-interfaces\ndhcp-authoritative\n'
                  'log-dhcp\nlog-facility=/var/log/nac-dnsmasq.log\n'
                  'dhcp-leasefile=/var/lib/nac-dnsmasq.leases\n'
                  f'dhcp-range={SUBNET}.50,{SUBNET}.99,255.255.255.0,3600s\n'
                  f'dhcp-option=option:router,{SUBNET}.10\n'
                  f'dhcp-option=option:dns-server,{SUBNET}.10\n'
                  'dhcp-option=option:domain-name,nac-lab.test\n')
        c.file('/etc/nac-dnsmasq.conf', config.encode())
        print('config check', c.run('dnsmasq --test --conf-file=/etc/nac-dnsmasq.conf; cat /etc/nac-dnsmasq.conf'))
    elif ACTION == 'preflight':
        config = ('port=0\ninterface=nacveths\nbind-interfaces\ndhcp-authoritative\n'
                  'log-dhcp\nlog-facility=/root/nac-dnsmasq-preflight.log\n'
                  'dhcp-leasefile=/root/nac-dnsmasq-preflight.leases\n'
                  'dhcp-range=10.70.110.50,10.70.110.99,255.255.255.0,3600s\n')
        sender = '''import socket, struct, time
mac = bytes.fromhex('02000000aa01')
packet = struct.pack('!BBBBIHH4s4s4s4s16s64s128s',
    1, 1, 6, 0, 0x11aabbcc, 0, 0x8000,
    socket.inet_aton('10.70.120.80'), bytes(4), bytes(4), bytes(4),
    mac + bytes(10), bytes(64), bytes(128))
packet += bytes.fromhex('63825363') + bytes([53, 1, 3, 61, 7, 1]) + mac + bytes([255])
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
s.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, b'nacvethc')
s.bind(('', 68))
s.settimeout(4)
print('SEND_REBIND ciaddr=10.70.120.80 xid=0x11aabbcc', flush=True)
s.sendto(packet, ('255.255.255.255', 67))
try:
    data, source = s.recvfrom(2048)
    opts = data[240:]
    print('REPLY', source, 'xid', hex(struct.unpack('!I', data[4:8])[0]),
          'yiaddr', socket.inet_ntoa(data[16:20]), 'options', opts.hex(), flush=True)
except socket.timeout:
    print('NO_REPLY_WITHIN_4S', flush=True)
'''
        c.file('/root/nac-dnsmasq-preflight.conf', config.encode())
        c.file('/root/nac-dnsmasq-rebind.py', sender.encode())
        try:
            print('setup', c.run('ip netns add nac_dhcp_s; ip netns add nac_dhcp_c; '
                                 'ip link add nacveths type veth peer name nacvethc; '
                                 'ip link set nacveths netns nac_dhcp_s; '
                                 'ip link set nacvethc netns nac_dhcp_c; '
                                 'ip -n nac_dhcp_s addr add 10.70.110.10/24 dev nacveths; '
                                 'ip -n nac_dhcp_s link set lo up; ip -n nac_dhcp_s link set nacveths up; '
                                 'ip -n nac_dhcp_c addr add 10.70.120.80/24 dev nacvethc; '
                                 'ip -n nac_dhcp_c link set lo up; ip -n nac_dhcp_c link set nacvethc up; '
                                 'ip netns exec nac_dhcp_s dnsmasq --test --conf-file=/root/nac-dnsmasq-preflight.conf',
                                 timeout=20))
            print('server', c.run('systemd-run --unit=nac-dnsmasq-preflight --collect '
                                  'ip netns exec nac_dhcp_s dnsmasq --no-daemon '
                                  '--conf-file=/root/nac-dnsmasq-preflight.conf; '
                                  'sleep 1; systemctl is-active nac-dnsmasq-preflight', timeout=15))
            print('packet', c.run('timeout 8 tcpdump -i nacveths -n -vvv -l '
                                  '\'udp port 67 or udp port 68\' > /root/nac-dnsmasq-preflight.packets 2>&1 & '
                                  'sleep 1; ip netns exec nac_dhcp_c python3 /root/nac-dnsmasq-rebind.py; '
                                  'sleep 1; cat /root/nac-dnsmasq-preflight.packets; '
                                  'cat /root/nac-dnsmasq-preflight.log', timeout=18))
        finally:
            print('cleanup', c.run('systemctl stop nac-dnsmasq-preflight 2>/dev/null || true; '
                                   'ip netns del nac_dhcp_c 2>/dev/null || true; '
                                   'ip netns del nac_dhcp_s 2>/dev/null || true; '
                                   'systemctl is-active nac-udhcpd', timeout=15))
    elif ACTION == 'timers':
        print(c.run('test ! -e /etc/nac-dnsmasq.conf.astra-before-short-timers && '
                    'cp /etc/nac-dnsmasq.conf /etc/nac-dnsmasq.conf.astra-before-short-timers && '
                    "printf 'dhcp-option=58,30i\\ndhcp-option=59,45i\\n' >> /etc/nac-dnsmasq.conf && "
                    'dnsmasq --test --conf-file=/etc/nac-dnsmasq.conf && '
                    'tail -n 3 /etc/nac-dnsmasq.conf', timeout=15))
    elif ACTION == 'start':
        print(c.run('systemctl stop nac-udhcpd; '
                    'systemd-run --unit=nac-dnsmasq-compare --collect '
                    '/usr/sbin/dnsmasq --no-daemon --conf-file=/etc/nac-dnsmasq.conf; '
                    'sleep 1; systemctl is-active nac-dnsmasq-compare; '
                    "ss -lunp | grep ':67 '", timeout=20))
    else:
        print(c.run('systemctl stop nac-dnsmasq-compare 2>/dev/null || true; '
                    'if test -e /etc/nac-dnsmasq.conf.astra-before-short-timers; then '
                    'mv /etc/nac-dnsmasq.conf.astra-before-short-timers /etc/nac-dnsmasq.conf; fi; '
                    'systemctl start nac-udhcpd; systemctl is-active nac-udhcpd; '
                    "ss -lunp | grep ':67 '", timeout=20))
finally:
    c.close()
