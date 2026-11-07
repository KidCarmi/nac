"""Verify the disposable initial admin password no longer authenticates."""
import json
from pathlib import Path
import ssl
import urllib.error
import urllib.request

fields = dict(line.split(': ', 1) for line in Path('/root/nac-initial-admin.txt').read_text().splitlines() if ': ' in line)
request = urllib.request.Request('https://127.0.0.1:8443/api/v1/auth/login',
    data=json.dumps({'email': fields['email'], 'password': fields['password']}).encode(),
    headers={'Content-Type': 'application/json'}, method='POST')
try:
    with urllib.request.urlopen(request, context=ssl._create_unverified_context(), timeout=10) as response:
        raise SystemExit(f'FAIL initial password still accepted HTTP {response.status}')
except urllib.error.HTTPError as error:
    if error.code != 401:
        raise SystemExit(f'FAIL unexpected HTTP {error.code}')
    print('PASS initial password rejected HTTP 401')
