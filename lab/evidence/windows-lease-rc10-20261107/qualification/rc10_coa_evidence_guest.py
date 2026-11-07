"""Read sanitized recent CoA evidence from the isolated NAC API."""
import json
from pathlib import Path
import ssl
import urllib.request

fields = dict(line.split(': ', 1) for line in
              Path('/root/nac-initial-admin.txt').read_text().splitlines()
              if ': ' in line)
context = ssl._create_unverified_context()
base = 'https://127.0.0.1:8443/api/v1'

def api(method, path, body=None, token=None):
    raw = json.dumps(body).encode() if body is not None else None
    headers = {'Content-Type': 'application/json'} if raw is not None else {}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(base + path, data=raw, headers=headers, method=method)
    with urllib.request.urlopen(request, context=context, timeout=20) as response:
        return json.load(response)

token = api('POST', '/auth/login',
            {'email': fields['email'], 'password': fields['password']})['token']
result = api('GET', '/sessions?limit=8', token=token)
sessions = result.get('sessions', result.get('items', []))
seen = set()
for session in sessions:
    if session.get('mac', '').lower() != '00:50:00:00:02:00':
        continue
    if session.get('last_auth_at', '') < '2026-11-07T11:23:00':
        continue
    detail = api('GET', '/sessions/' + session['id'], token=token)
    for key in ('coa_requests', 'coas', 'timeline', 'events'):
        value = detail.get(key, [])
        if not isinstance(value, list):
            continue
        for item in value:
            if not isinstance(item, dict):
                continue
            if key not in ('coa_requests', 'coas') and item.get('kind') != 'coa':
                continue
            if not item.get('created_at') or item['created_at'] < '2026-11-07T11:23:00':
                continue
            if item.get('id') in seen:
                continue
            seen.add(item.get('id'))
            selected = {name: item.get(name) for name in (
                'id', 'kind', 'action', 'state', 'created_at', 'requested_at',
                'updated_at', 'attempted_at', 'completed_at', 'detail', 'message')
                if name in item}
            print('COA', json.dumps(selected, sort_keys=True))
