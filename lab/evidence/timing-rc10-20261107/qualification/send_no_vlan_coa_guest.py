"""Send a lab-only CoA to the same test session without VLAN attributes.

This controlled diagnostic is not a NAC product action. The disposable shared
secret remains in a root-only file and is passed to radclient with -S.
"""
from datetime import datetime, timezone
import json
from pathlib import Path
import os
import subprocess
import urllib.request

version = json.load(urllib.request.urlopen('http://127.0.0.1:8080/version', timeout=5))
if version.get('version') != 'pilot-rc10' or version.get('commit') != 'b39649f':
    raise SystemExit('wrong candidate')
if '10.70.99.4' not in subprocess.check_output(['ip', '-4', '-br', 'addr'], text=True):
    raise SystemExit('wrong control network')
secret = Path('/root/nac-existing-lab-pki/radius.secret')
if not secret.is_file() or secret.stat().st_mode & 0o077:
    raise SystemExit('root-only lab CoA secret unavailable')
attributes = ('User-Name = "host/wired-endpoint-01"\n'
              'Calling-Station-Id = "00-50-00-00-02-00"\n'
              'NAS-IP-Address = 10.70.99.1\n'
              'NAS-Port-Id = "Ethernet1"\n')
vlan = os.environ.get('LAB_COA_VLAN', '')
if vlan not in ('', '10'):
    raise SystemExit('only same-VLAN diagnostic is supported')
if vlan:
    attributes += ('Tunnel-Type = VLAN\n'
                   'Tunnel-Medium-Type = IEEE-802\n'
                   f'Tunnel-Private-Group-Id = "{vlan}"\n')
print('VARIANT', 'with-vlan-10' if vlan else 'without-vlan')
print('ACTION_AT', datetime.now(timezone.utc).isoformat(timespec='milliseconds'))
result = subprocess.run(['/usr/bin/radclient', '-x', '-S', str(secret),
                         '10.70.99.1:3799', 'coa'], input=attributes,
                        text=True, capture_output=True, timeout=15)
print('RETURN_CODE', result.returncode)
for line in (result.stdout+'\n'+result.stderr).splitlines():
    if any(marker in line for marker in ('Sent CoA-', 'Received CoA-', 'Error-Cause',
                                        'No reply', 'radclient:')):
        print(line[:350])
print('FINISHED_AT', datetime.now(timezone.utc).isoformat(timespec='milliseconds'))
if result.returncode or 'Received CoA-ACK' not in result.stdout:
    raise SystemExit('lab CoA was not ACKed')
