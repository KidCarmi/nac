"""Synthetic old-subnet DHCP rebinding test, entirely inside disposable netns."""
import os
from pathlib import Path
import signal
import socket
import struct
import subprocess
import sys
import time

ROOT = Path('/root/nac-dhcp-rebind-preflight')

def call(*argv):
    subprocess.run(argv, check=True, stdout=subprocess.DEVNULL)

def sender():
    control = sys.argv[1:] == ['--sender-control']
    mac = bytes.fromhex('02000000aa01')
    packet = struct.pack('!BBBBIHH4s4s4s4s16s64s128s',
        1, 1, 6, 0, 0x11aabbcc, 0, 0x8000,
        socket.inet_aton('10.70.110.80' if control else '10.70.120.80'),
        bytes(4), bytes(4), bytes(4),
        mac + bytes(10), bytes(64), bytes(128))
    packet += bytes.fromhex('63825363') + bytes([53, 1, 3, 61, 7, 1]) + mac + bytes([255])
    packet = packet.ljust(300, b'\0')
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, b'nacvethc')
    sock.bind(('', 68))
    sock.settimeout(4)
    print('SEND_REBIND ciaddr=' + ('10.70.110.80' if control else '10.70.120.80') +
          ' xid=0x11aabbcc', flush=True)
    sock.sendto(packet, ('255.255.255.255', 67))
    try:
        data, source = sock.recvfrom(2048)
        print('REPLY', source, 'xid', hex(struct.unpack('!I', data[4:8])[0]),
              'yiaddr', socket.inet_ntoa(data[16:20]),
              'options', data[240:].hex(), flush=True)
    except socket.timeout:
        print('NO_REPLY_WITHIN_4S', flush=True)

def main():
    ROOT.mkdir(exist_ok=True)
    (ROOT/'dnsmasq.log').unlink(missing_ok=True)
    config = ROOT / 'dnsmasq.conf'
    config.write_text('port=0\ninterface=nacveths\nbind-interfaces\n'
                      'dhcp-authoritative\nlog-dhcp\n'
                      f'log-facility={ROOT}/dnsmasq.log\n'
                      f'dhcp-leasefile={ROOT}/leases\n'
                      'dhcp-range=10.70.110.50,10.70.110.99,255.255.255.0,3600s\n')
    server = capture = None
    try:
        for name in ('nac_dhcp_s', 'nac_dhcp_c'):
            call('ip', 'netns', 'add', name)
        call('ip', 'link', 'add', 'nacveths', 'type', 'veth', 'peer', 'name', 'nacvethc')
        call('ip', 'link', 'set', 'nacveths', 'netns', 'nac_dhcp_s')
        call('ip', 'link', 'set', 'nacvethc', 'netns', 'nac_dhcp_c')
        call('ip', '-n', 'nac_dhcp_s', 'addr', 'add', '10.70.110.10/24', 'dev', 'nacveths')
        call('ip', '-n', 'nac_dhcp_s', 'link', 'set', 'lo', 'up')
        call('ip', '-n', 'nac_dhcp_s', 'link', 'set', 'nacveths', 'up')
        call('ip', '-n', 'nac_dhcp_c', 'addr', 'add', '10.70.120.80/24', 'dev', 'nacvethc')
        call('ip', '-n', 'nac_dhcp_c', 'link', 'set', 'lo', 'up')
        call('ip', '-n', 'nac_dhcp_c', 'link', 'set', 'nacvethc', 'up')
        print('SERVER_VERSION', subprocess.check_output(['dnsmasq','--version'], text=True).splitlines()[0])
        print('SERVER_CONFIG', config.read_text())
        server = subprocess.Popen(['ip','netns','exec','nac_dhcp_s','dnsmasq',
                                   '--no-daemon',f'--conf-file={config}'],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)
        with (ROOT/'packets.pcap').open('wb') as sink:
            capture = subprocess.Popen(['ip','netns','exec','nac_dhcp_s','tcpdump',
                                        '-i','nacveths','-n','-U','-w','-',
                                        'udp port 67 or udp port 68'],
                                       stdout=sink, stderr=subprocess.DEVNULL)
            time.sleep(1)
            subprocess.run(['ip','netns','exec','nac_dhcp_c','python3',__file__,'--sender'],check=True)
            call('ip','-n','nac_dhcp_c','addr','del','10.70.120.80/24','dev','nacvethc')
            call('ip','-n','nac_dhcp_c','addr','add','10.70.110.80/24','dev','nacvethc')
            subprocess.run(['ip','netns','exec','nac_dhcp_c','python3',__file__,'--sender-control'],check=True)
            time.sleep(1)
        packet_text = subprocess.check_output(['tcpdump','-nn','-vvv','-r',str(ROOT/'packets.pcap')],
                                               text=True,stderr=subprocess.DEVNULL)
        print('PACKETS\n'+packet_text)
        print('DNSMASQ_LOG\n'+(ROOT/'dnsmasq.log').read_text())
    finally:
        for proc in (capture, server):
            if proc and proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    proc.kill()
        for name in ('nac_dhcp_c','nac_dhcp_s'):
            subprocess.run(['ip','netns','del',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)

if __name__ == '__main__':
    sender() if sys.argv[1:] in (['--sender'], ['--sender-control']) else main()
