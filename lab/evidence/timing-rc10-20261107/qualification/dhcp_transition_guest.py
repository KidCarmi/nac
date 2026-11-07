"""Switch the stopped Arista pilot to its isolated DHCP qualification copy.

Only EVE node state and an isolated EVE-host control address are changed.
Proxmox bridges, other VMs and original disks remain untouched.
"""
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request

LABS = {
    'baseline': ('NAC-Wired-Qualification.unl', set(range(1,6)), '659656f9030ce694ac01347db943f67a87bc56f451ef3789d5d3f94e2200fad9'),
    'arista': ('NAC-Pilot-Candidate.unl', set(range(11,16)), 'e3407fe0f06ec49769fac5dc3578aa436eaab91d700865cbb5e70838a5a7af8a'),
    'mikrotik': ('NAC-MAB-MikroTik.unl', set(range(21,26)), '527f9a4ab2070facd443c3d16e6d3af042769c08282b079d9d8125d6e84bef3a'),
    'dhcp': ('NAC-Pilot-DHCP.unl', set(range(31,36)), '8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935'),
}
ADDRESS = '10.70.99.254/24'
BASE = 'http://127.0.0.1/api'
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def api(path, method='GET', data=None):
    request = urllib.request.Request(BASE + path, method=method,
        data=None if data is None else json.dumps(data).encode(),
        headers={'Content-Type': 'application/json'})
    with opener.open(request, timeout=30) as response:
        answer = json.load(response)
    if answer.get('status') != 'success':
        raise RuntimeError((path, answer))
    return answer.get('data', answer)

def address(bridge):
    if not Path('/sys/class/net', bridge).exists():
        return []
    output = json.loads(subprocess.check_output(['ip', '-j', 'addr', 'show', 'dev', bridge]))
    return sorted(a['local'] + '/' + str(a['prefixlen']) for e in output
                  for a in e.get('addr_info', []) if a['family'] == 'inet')

def status(name):
    filename, expected, _ = LABS[name]
    entries = api('/labs/' + filename + '/nodes')
    entries = entries.values() if isinstance(entries, dict) else entries
    result = {int(n['id']): int(n['status']) for n in entries}
    if set(result) != expected:
        raise RuntimeError('unexpected node IDs: ' + name)
    return result

def inspect():
    if subprocess.check_output(['sysctl', '-n', 'net.ipv4.ip_forward'], text=True).strip() != '0':
        raise RuntimeError('EVE forwarding enabled')
    hashes = {}
    for name, (filename, _, expected) in LABS.items():
        actual = hashlib.sha256((Path('/opt/unetlab/labs') / filename).read_bytes()).hexdigest()
        if actual != expected:
            raise RuntimeError(f'{name} topology changed: {actual}')
        hashes[name] = actual
    state = {name: status(name) for name in LABS}
    bridges = {name: address(name) for name in ('vnet0_14', 'vnet0_34')}
    print(json.dumps({'nodes': state, 'bridges': bridges,
                      'topology_sha256': hashes, 'ip_forward': 0}, sort_keys=True), flush=True)
    return state, bridges

def change(name, node, verb):
    path = '/labs/' + LABS[name][0] + f'/nodes/{node}'
    wanted = 2 if verb == 'start' else 0
    if int(api(path)['status']) == wanted:
        return
    api(path + '/' + verb)
    for _ in range(60):
        if int(api(path)['status']) == wanted:
            return
        time.sleep(1)
    raise RuntimeError(f'{name} node {node} failed to {verb}')

def move(source, target):
    if address(source) != [ADDRESS] or address(target):
        raise RuntimeError('unexpected bridge addresses before move')
    subprocess.run(['ip', 'addr', 'del', ADDRESS, 'dev', source], check=True)
    try:
        subprocess.run(['ip', 'addr', 'add', ADDRESS, 'dev', target], check=True)
    except Exception:
        subprocess.run(['ip', 'addr', 'add', ADDRESS, 'dev', source], check=True)
        raise
    if address(source) or address(target) != [ADDRESS]:
        raise RuntimeError('bridge address move failed verification')

def start_dhcp():
    state, bridges = inspect()
    if any(v for group in state.values() for v in group.values()):
        raise RuntimeError('all four labs must be stopped')
    if bridges['vnet0_14'] != [ADDRESS] or bridges['vnet0_34']:
        raise RuntimeError('unexpected control bridge state')
    change('dhcp', 35, 'start')
    for _ in range(30):
        if Path('/sys/class/net/vnet0_34').exists():
            break
        time.sleep(1)
    move('vnet0_14', 'vnet0_34')
    for node in (33,34,31,32):
        change('dhcp', node, 'start')
    inspect()

def stop_dhcp():
    state, bridges = inspect()
    if any(v for name, group in state.items() if name != 'dhcp' for v in group.values()):
        raise RuntimeError('another lab is active')
    if set(state['dhcp'].values()) != {2} or bridges['vnet0_34'] != [ADDRESS] or bridges['vnet0_14']:
        raise RuntimeError('unexpected DHCP lab state')
    for node in (32,31,34,33):
        change('dhcp', node, 'stop')
    move('vnet0_34', 'vnet0_14')
    change('dhcp', 35, 'stop')
    inspect()

def restore_arista():
    state, bridges = inspect()
    if any(v for group in state.values() for v in group.values()):
        raise RuntimeError('all labs must be stopped')
    if bridges['vnet0_14'] != [ADDRESS] or bridges['vnet0_34']:
        raise RuntimeError('unexpected control bridge state')
    for node in (15,13,14,11,12):
        change('arista', node, 'start')
    inspect()

def start_clean_control():
    state, bridges = inspect()
    if any(v for group in state.values() for v in group.values()):
        raise RuntimeError('clean control start requires all labs stopped')
    if bridges['vnet0_14'] != [ADDRESS] or bridges['vnet0_34']:
        raise RuntimeError('control address must be on isolated candidate bridge')
    change('arista', 15, 'start')
    inspect()

def stop_clean_control():
    state, bridges = inspect()
    if any(v for name, group in state.items() if name != 'arista' for v in group.values()):
        raise RuntimeError('another lab is active')
    if state['arista'] != {11: 0, 12: 0, 13: 0, 14: 0, 15: 2}:
        raise RuntimeError('expected only clean candidate control node running')
    if bridges['vnet0_14'] != [ADDRESS] or bridges['vnet0_34']:
        raise RuntimeError('unexpected control bridge address')
    change('arista', 15, 'stop')
    inspect()

def reboot_dhcp_switch():
    state, bridges = inspect()
    if any(v for name, group in state.items() if name != 'dhcp' for v in group.values()):
        raise RuntimeError('another lab is active')
    if state['dhcp'] != {31: 2, 32: 0, 33: 2, 34: 2, 35: 2}:
        raise RuntimeError('unexpected DHCP lab node state')
    if bridges['vnet0_34'] != [ADDRESS] or bridges['vnet0_14']:
        raise RuntimeError('unexpected control bridge address')
    change('dhcp', 31, 'stop')
    change('dhcp', 31, 'start')
    inspect()

if __name__ == '__main__':
    action = os.environ.get('DHCP_TRANSITION_ACTION', 'inspect')
    if action not in ('inspect', 'start_dhcp', 'stop_dhcp', 'restore_arista',
                      'start_clean_control', 'stop_clean_control', 'reboot_dhcp_switch'):
        raise SystemExit('invalid action')
    api('/auth/login', 'POST', {'username': 'admin',
        'password': Path('/root/eve-nac-lab.webpw').read_text().strip()})
    globals()[action]()
