"""Exercise documented account change and local recovery on disposable rc9 VM.

Secrets and tokens stay in guest memory or a root-only file; output is status only.
"""
import json
from pathlib import Path
import secrets
import ssl
import subprocess
import urllib.error
import urllib.request

base = 'https://127.0.0.1:8443/api/v1/auth'
ctx = ssl._create_unverified_context()  # disposable lab HTTPS certificate
fields = {}
for line in Path('/root/nac-initial-admin.txt').read_text().splitlines():
    if ': ' in line:
        key, value = line.split(': ', 1)
        fields[key] = value
email = fields['email']
initial = fields['password']
changed = secrets.token_urlsafe(24)
recovered = secrets.token_urlsafe(24)


def call(method, path, payload=None, token=None):
    body = json.dumps(payload).encode() if payload is not None else None
    headers = {'Content-Type': 'application/json'} if body is not None else {}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + path, data=body, headers=headers,
                                 method=method)
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=8) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as exc:
        exc.read()
        return exc.code, {}


def check(label, result, expected):
    actual = result[0]
    print(label, actual)
    if actual != expected:
        raise RuntimeError(f'{label}: expected {expected}, got {actual}')
    return result[1]


old_token = check('initial_login', call('POST', '/login',
                            {'email': email, 'password': initial}), 200)['token']
check('initial_me', call('GET', '/me', token=old_token), 200)
check('wrong_current_change', call('POST', '/password',
      {'current_password': 'definitely-wrong', 'new_password': changed},
      old_token), 403)
check('old_token_after_bad_change', call('GET', '/me', token=old_token), 200)
new_token = check('correct_change', call('POST', '/password',
      {'current_password': initial, 'new_password': changed},
      old_token), 200)['token']
check('old_token_after_change', call('GET', '/me', token=old_token), 401)
check('new_token_after_change', call('GET', '/me', token=new_token), 200)
check('old_password_after_change', call('POST', '/login',
      {'email': email, 'password': initial}), 401)
check('new_password_after_change', call('POST', '/login',
      {'email': email, 'password': changed}), 200)

reset = subprocess.run(['/usr/local/sbin/nac-reset-password', email],
                       input=recovered + '\n' + recovered + '\n',
                       text=True, capture_output=True, timeout=20)
print('local_reset_exit', reset.returncode)
if reset.returncode:
    raise RuntimeError('local reset failed without disclosing its output')
check('token_after_local_reset', call('GET', '/me', token=new_token), 401)
check('changed_password_after_reset', call('POST', '/login',
      {'email': email, 'password': changed}), 401)
check('recovered_password_login', call('POST', '/login',
      {'email': email, 'password': recovered}), 200)

final = Path('/root/nac-account-test-final')
final.write_text('email: ' + email + '\npassword: ' + recovered + '\n')
final.chmod(0o600)
print('FINAL_SECRET_STORED_GUEST_ONLY', final)
