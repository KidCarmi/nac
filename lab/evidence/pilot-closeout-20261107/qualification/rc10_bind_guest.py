"""Bind rc9 validated test certificate to the rc10 endpoint via product API."""
import json
from pathlib import Path
import ssl
import urllib.request

fields = dict(line.split(': ', 1) for line in Path('/root/nac-initial-admin.txt').read_text().splitlines() if ': ' in line)
ctx = ssl._create_unverified_context()
base = 'https://127.0.0.1:8443/api/v1'
def call(path, body, token=None):
    data = json.dumps(body).encode()
    headers = {'Content-Type':'application/json'}
    if token: headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + path, data=data, headers=headers)
    with urllib.request.urlopen(req, context=ctx, timeout=10) as r:
        return r.status, json.load(r)
_, login = call('/auth/login', {'email': fields['email'], 'password': fields['password']})
status, binding = call('/identity-bindings', {
    'endpoint_id': 'dda872ea-0fef-4257-a9a8-497412d88bd1',
    'issuer': '/CN=NAC Lab Disposable CA',
    'serial': '2E1B47DC86A00B7F7250500B4A21F454B2EFD819',
    'subject_cn': 'wired-endpoint-01',
    'note': 'Verified EAP-TLS identity from clean rc9 switch-originated RADIUS session',
}, login['token'])
assert status == 201
print('BINDING_ID', binding['id'])
print('ISSUER', binding['issuer'])
print('SERIAL', binding['serial'])
