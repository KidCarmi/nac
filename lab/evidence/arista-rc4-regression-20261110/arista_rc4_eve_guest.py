"""Start or stop only nodes 31-35 of the pinned, isolated vEOS DHCP EVE topology.

The node-35 control overlay is snapshotted separately before candidate changes.
"""
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import time
import urllib.request

LAB = Path('/opt/unetlab/labs/NAC-Pilot-DHCP.unl')
EXPECTED_HASH = '8b69cf973f3cc545082aac0334aaf0aaa2e9c7b6563a3f66d5d4ec86c62ea935'
PATH = '/labs/NAC-Pilot-DHCP.unl/nodes'
NODES = {
    31: 'vEOS-DHCP-access',
    32: 'DHCP-endpoint',
    33: 'DHCP-corp-service',
    34: 'DHCP-quarantine-service',
    35: 'NAC-candidate-control',
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
        if node_id == 31 and (node.get('image'), node.get('template')) != ('veos-4.29.2F-dhcp', 'veos'):
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
    action = os.environ.get('ARISTA_RC4_ACTION', 'status')
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
        if any(v not in (0, 2) for v in before.values()):
            raise RuntimeError('unexpected node status before stop')
        for node_id in (32, 31, 35, 34, 33):
            change(node_id, 'stop')
    elif action == 'start':
        if any(v not in (0, 2) for v in before.values()):
            raise RuntimeError('unexpected node status before start')
        for node_id in (33, 34, 35, 31, 32):
            change(node_id, 'start')
    print(json.dumps({'phase': 'after', 'action': action, 'nodes': snapshot()}), flush=True)


if __name__ == '__main__':
    main()

