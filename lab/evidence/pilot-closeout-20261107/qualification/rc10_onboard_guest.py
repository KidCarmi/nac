"""Onboard the isolated rc10 control guest through documented product APIs.

Run with clean_rc9_vm.py --rc10 --script. Secrets stay inside the guest.
"""
import json
from pathlib import Path
import ssl
import urllib.error
import urllib.request

base = 'https://127.0.0.1:8443/api/v1'
ctx = ssl._create_unverified_context()  # disposable installation certificate
fields = dict(line.split(': ', 1) for line in Path('/root/nac-initial-admin.txt').read_text().splitlines() if ': ' in line)

def request(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {'Content-Type': 'application/json'} if data is not None else {}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=20) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raw = error.read()
        detail = raw.decode(errors='replace')[:500]
        raise RuntimeError(f'{method} {path} failed HTTP {error.code}: {detail}') from None

status, login = request('POST', '/auth/login', {'email': fields['email'], 'password': fields['password']})
assert status == 200
token = login['token']
secret = Path('/root/nac-existing-lab-pki/radius.secret').read_text().strip()
status, switch = request('POST', '/switches', {
    'name': 'nac-veos-rc10', 'radius_source_ip': '10.70.99.1',
    'nas_ip': '10.70.99.1', 'radius_secret': secret,
    'radius_server_ip': '10.70.99.4', 'access_ports': ['Ethernet1'],
    'segment_vlans': {'corp': 10, 'quarantine': 20},
}, token)
assert status == 201
status, endpoint = request('POST', '/endpoints', {
    'mac': '00:50:00:00:02:00', 'hostname': 'wired-endpoint-01',
    'device_type': 'workstation',
}, token)
assert status == 201
status, rule = request('POST', '/policies', {
    'name': 'bound-eap-corp', 'priority': 100, 'enabled': True,
    'conditions': [
        {'field': 'identity.bound', 'operator': 'eq', 'values': ['true']},
        {'field': 'mac.matches_device', 'operator': 'eq', 'values': ['true']},
    ],
    'action': 'allow', 'segment': 'corp',
}, token)
assert status == 201
status, draft = request('GET', '/policy-draft', token=token)
assert status == 200 and draft['validation']['valid']
status, preview = request('GET', '/policy-draft/preview', token=token)
assert status == 200 and preview['draft_digest'] == draft['digest']
status, published = request('POST', '/policy-draft/publish', {
    'expected_digest': draft['digest'], 'note': 'ASTRA isolated rc10 qualification'
}, token)
assert status == 201
print('SWITCH_ID', switch['id'])
print('ENDPOINT_ID', endpoint['id'])
print('RULE_ID', rule['id'])
print('POLICY_VERSION', published['version']['number'])
print('PREVIEW_CASES', preview['cases_examined'])
print('RADIUS_SECRET_GUEST_ONLY')
