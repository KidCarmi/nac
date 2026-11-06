"""Start/stop candidate EAPOL and RADIUS captures on isolated EVE bridges."""
import json
import os
from pathlib import Path
import re
import signal
import subprocess

run_id = os.environ.get('CANDIDATE_RUN_ID', 'candidate-36e3a57-20261106')
if not re.fullmatch(r'candidate-[a-zA-Z0-9_-]{1,64}', run_id):
    raise SystemExit('invalid CANDIDATE_RUN_ID')
ROOT = Path('/root/nac-qualification') / run_id
CAPTURES = {
    'eapol': ('vnet0_11', 'endpoint-eapol.pcap', ['ether', 'proto', '0x888e']),
    'radius': ('vnet0_14', 'radius.pcap', ['udp', 'port', '1812', 'or', 'udp', 'port', '3799']),
}
action = os.environ.get('CAPTURE_ACTION')
if action == 'start':
    ROOT.mkdir(mode=0o700, parents=True, exist_ok=False)
    subprocess.run(['sysctl','-n','net.ipv4.ip_forward'], capture_output=True, text=True, check=True)
    for name, (interface, file, filt) in CAPTURES.items():
        log = (ROOT / (name + '.log')).open('wb')
        with log:
            proc = subprocess.Popen(['tcpdump','-U','-i',interface,'-w',str(ROOT/file),*filt],
                                    stdout=log, stderr=log, start_new_session=True)
        (ROOT / (name + '.pid')).write_text(str(proc.pid))
    print(json.dumps({'capture_dir':str(ROOT),'state':'started','interfaces':{k:v[0] for k,v in CAPTURES.items()}},sort_keys=True))
elif action == 'stop':
    for name, (_, file, _) in CAPTURES.items():
        pid = int((ROOT / (name + '.pid')).read_text())
        os.kill(pid, signal.SIGINT)
    print(json.dumps({'capture_dir':str(ROOT),'state':'stopped'},sort_keys=True))
else:
    raise SystemExit('set CAPTURE_ACTION=start|stop')
