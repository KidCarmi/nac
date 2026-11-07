"""Store a disposable pre-rotation token and verify its revocation.

Run inside the isolated fresh UI guest with TOKEN_ACTION=prepare before the
operator changes the password, then TOKEN_ACTION=verify afterward. No token or
password is printed or exported.
"""
import json
import os
from pathlib import Path
import ssl
import urllib.error
import urllib.request

action = os.environ.get('TOKEN_ACTION')
if action not in {'prepare', 'verify'}:
    raise SystemExit('TOKEN_ACTION=prepare|verify required')
fields = dict(line.split(': ', 1) for line in
              Path('/root/nac-initial-admin.txt').read_text().splitlines()
              if ': ' in line)
base = 'https://127.0.0.1:8443/api/v1'
context = ssl._create_unverified_context()
path = Path('/root/nac-fresh-ui-prechange-token')

def request(method, endpoint, payload=None, token=None):
    body = json.dumps(payload).encode() if payload is not None else None
    headers = {'Content-Type': 'application/json'} if body is not None else {}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + endpoint, data=body, headers=headers,
                                 method=method)
    try:
        with urllib.request.urlopen(req, context=context, timeout=10) as response:
            data = response.read()
            return response.status, json.loads(data) if data else {}
    except urllib.error.HTTPError as error:
        error.read()
        return error.code, {}

if action == 'prepare':
    if path.exists():
        raise SystemExit('prechange token already exists')
    code, login = request('POST', '/auth/login',
                          {'email': fields['email'], 'password': fields['password']})
    if code != 200:
        raise SystemExit('initial login did not succeed')
    token = login['token']
    path.write_text(token)
    path.chmod(0o600)
    code, _ = request('GET', '/auth/me', token=token)
    print('PRECHANGE_TOKEN_ME', code)
    if code != 200:
        raise SystemExit('token unusable before change')
else:
    if not path.is_file() or path.stat().st_mode & 0o077:
        raise SystemExit('protected prechange token missing')
    code, _ = request('GET', '/auth/me', token=path.read_text().strip())
    print('PRECHANGE_TOKEN_ME_AFTER', code)
    if code != 401:
        raise SystemExit('old token was not revoked')
    code, _ = request('POST', '/auth/login',
                      {'email': fields['email'], 'password': fields['password']})
    print('INITIAL_PASSWORD_LOGIN_AFTER', code)
    if code != 401:
        raise SystemExit('initial password was not revoked')
