"""Run the pre-staged, documented policy API diagnostic on exact rc9."""
import os
import sys

sys.path.insert(0, '/root/nac-qualification')
from dhcp_candidate_serial_guest import Console

action = os.environ.get('NAC_POLICY_ACTION', '')
if action not in {'read', 'draft-quarantine', 'preview', 'publish', 'restore-v2'}:
    raise SystemExit('invalid NAC_POLICY_ACTION')
console = Console()
try:
    print(console.run("NAC_POLICY_NOTE='ASTRA isolated pilot-rc9 qualification' "
                      'python3 /root/nac-policy-api.py ' + action, timeout=30))
finally:
    console.close()

