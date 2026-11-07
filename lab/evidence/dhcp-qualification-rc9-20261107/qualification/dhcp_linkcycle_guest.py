"""Stop/start only the isolated DHCP endpoint VM to simulate a wire unplug.

Run inside EVE with DHCP_LINK_ACTION=stop|start|inspect. The helper verifies the
topology hashes, disabled EVE forwarding, other labs stopped, and all other
DHCP nodes running before changing endpoint node 32.
"""
import os
from pathlib import Path
import sys

sys.path.insert(0, '/root/nac-qualification')
from dhcp_transition_guest import ADDRESS, api, change, inspect


action = os.environ.get('DHCP_LINK_ACTION', '')
if action not in {'stop', 'start', 'inspect'}:
    raise SystemExit('set DHCP_LINK_ACTION=stop|start|inspect')

api('/auth/login', 'POST', {'username': 'admin',
    'password': Path('/root/eve-nac-lab.webpw').read_text().strip()})
state, bridges = inspect()
if any(v for name, group in state.items() if name != 'dhcp' for v in group.values()):
    raise SystemExit('another lab is active')
if bridges['vnet0_34'] != [ADDRESS] or bridges['vnet0_14']:
    raise SystemExit('unexpected control bridge state')
if any(state['dhcp'][node] != 2 for node in (31, 33, 34, 35)):
    raise SystemExit('switch, services and NAC control must stay running')
if action == 'stop':
    if state['dhcp'][32] != 2:
        raise SystemExit('endpoint was not running')
    change('dhcp', 32, 'stop')
elif action == 'start':
    if state['dhcp'][32] != 0:
        raise SystemExit('endpoint was not stopped')
    change('dhcp', 32, 'start')
inspect()
