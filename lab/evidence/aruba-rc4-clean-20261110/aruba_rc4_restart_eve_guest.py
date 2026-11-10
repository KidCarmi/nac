"""Restart only nodes 61-65 of the pinned, isolated Aruba EVE topology.

The separate rc4 NAC guest is stopped and started by its own launcher.
"""
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import time
import urllib.request

LAB = Path('/opt/unetlab/labs/Aruba-Role-ACL-1007.unl')
EXPECTED_HASH = '004c7fbf948fa3e9fe06367c94a889c88a73afb56c0b65719006e10f3970ae94'
PATH = '/labs/Aruba-Role-ACL-1007.unl/nodes'
NODES = {
    61: 'Node61',
    62: 'Aruba-DHCP-endpoint',
    63: 'Aruba-corp-service',
    64: 'Aruba-remediation-service',
    65: 'Aruba-FreeRADIUS-control',
}
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def api(path, method='GET', data=None):
    req = urllib.request.Request('http://127.0.0.1/api' + path,
        method=method, data=None if data is None else json.dumps(data).encode(),
        headers={'Content-Type': 'application/json'})
    with opener.open(req, timeout=30) as response:
        answer = json.load(response)
    if answer.get('status') != 'success':
        raise RuntimeError((path, answer))
    return answer.get('data', answer)


def snapshot():
    result = {}
    for node_id, expected_name in NODES.items():
        node = api(f'{PATH}/{node_id}')
        if node.get('name') != expected_name:
            raise RuntimeError(f'node {node_id} name mismatch: {node.get("name")}')
        if node_id == 61 and (node.get('image'), node.get('template')) != ('arubacx-10.07', 'arubacx'):
            raise RuntimeError('switch image/template mismatch')
        result[node_id] = int(node['status'])
    return result


def change(node_id, action):
    wanted = 0 if action == 'stop' else 2
    if snapshot()[node_id] != wanted:
        api(f'{PATH}/{node_id}/{action}')
        for _ in range(60):
            if snapshot()[node_id] == wanted:
                break
            time.sleep(1)
        else:
            raise RuntimeError(f'{node_id} did not reach {wanted}')
    print(json.dumps({'utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        'node_id': node_id, 'action': action, 'status': wanted}), flush=True)


def main():
    action = os.environ.get('RC4_RESTART_ACTION', 'status')
    if action not in {'status', 'stop', 'start'}:
        raise SystemExit('use status|stop|start')
    digest = hashlib.sha256(LAB.read_bytes()).hexdigest()
    if digest != EXPECTED_HASH:
        raise RuntimeError(f'topology hash mismatch: {digest}')
    api('/auth/login', 'POST', {'username': 'admin',
        'password': Path('/root/eve-nac-lab.webpw').read_text().strip()})
    before = snapshot()
    print(json.dumps({'phase': 'before', 'action': action, 'topology_sha256': digest,
        'nodes': before}), flush=True)
    if action == 'stop':
        if any(v != 2 for v in before.values()):
            raise RuntimeError('all target nodes must be running before stop')
        for node_id in (62, 61, 65, 64, 63):
            change(node_id, 'stop')
    elif action == 'start':
        if any(v != 0 for v in before.values()):
            raise RuntimeError('all target nodes must be stopped before start')
        for node_id in (63, 64, 65, 61, 62):
            change(node_id, 'start')
    print(json.dumps({'phase': 'after', 'action': action, 'nodes': snapshot()}), flush=True)


if __name__ == '__main__':
    main()
