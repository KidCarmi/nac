"""Run the documented NAC operator API diagnostic via isolated guest serial."""
import os
from pathlib import Path
import sys
sys.path.insert(0,'/root/nac-qualification')
from dhcp_candidate_serial_guest import Console, stage_file
action=os.environ.get('NAC_API_ACTION','')
if action not in {'stage','sessions','details','events','switch','quarantine','restore','reauthenticate'}:
    raise SystemExit('invalid NAC_API_ACTION')
console=Console()
try:
    if action=='stage':
        stage_file(console,Path('/root/nac-qualification/nac_lab_api_control_v3_rc7.py'),
                   '/root/nac-lab-api.py',0o600)
    else:
        print(console.run('python3 /root/nac-lab-api.py '+action,timeout=22))
finally:
    console.close()
