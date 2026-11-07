"""Capture the isolated rc2 DHCP/EAPOL/RADIUS/CoA/accounting run in EVE."""
import json
import os
from pathlib import Path
import signal
import subprocess
import time

ROOT = Path(os.environ.get('DHCP_CAPTURE_ROOT',
    '/root/nac-qualification/candidate-c121a95-dhcp-20261106'))
CAPTURES = {
    'endpoint': ('vnet0_31', 'ether proto 0x888e or (udp and (port 67 or port 68))'),
    'radius': ('vnet0_34', 'udp and (port 1812 or port 1813 or port 3799)'),
    'corp-dhcp': ('vnet0_32', 'udp and (port 67 or port 68)'),
    'quarantine-dhcp': ('vnet0_33', 'udp and (port 67 or port 68)'),
}
action = os.environ.get('DHCP_CAPTURE_ACTION', '')
if subprocess.check_output(['sysctl','-n','net.ipv4.ip_forward'], text=True).strip() != '0':
    raise SystemExit('EVE forwarding enabled')
if action == 'start':
    ROOT.mkdir(mode=0o700, parents=True, exist_ok=False)
    for name, (iface, selector) in CAPTURES.items():
        with (ROOT / (name + '.stderr')).open('wb') as errors:
            process = subprocess.Popen(['tcpdump','-U','-n','-i',iface,
                '-w',str(ROOT / (name + '.pcap')),selector],
                stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=errors,
                start_new_session=True)
        (ROOT / (name + '.pid')).write_text(str(process.pid))
    time.sleep(1)
    print(json.dumps({'state':'started','root':str(ROOT),
                      'interfaces':{name:iface for name,(iface,_) in CAPTURES.items()}}, sort_keys=True))
elif action == 'stop':
    for name in CAPTURES:
        os.kill(int((ROOT / (name + '.pid')).read_text()), signal.SIGINT)
    time.sleep(1)
    print(json.dumps({'state':'stopped','sizes':{name:(ROOT/(name+'.pcap')).stat().st_size
                      for name in CAPTURES}}, sort_keys=True))
else:
    raise SystemExit('set DHCP_CAPTURE_ACTION=start|stop')
