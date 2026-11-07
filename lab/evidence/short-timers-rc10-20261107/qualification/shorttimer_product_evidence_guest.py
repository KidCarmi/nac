"""Read only the two rc10 short-timer CoA records, keeping the lab admin token guest-local."""
import json
from pathlib import Path
import ssl
import urllib.request

ids = {'c2d06983-1f3a-4492-b3b3-7280d7194433',
       'f17b3a4d-e394-42ee-9bdd-e65ffe18241e'}
decision_ids = {'150f7d8c-04be-4328-b41a-7d17e22b3e68',
                '41d4330a-1f85-48ca-90c4-27df2561e345'}
seen = set()
fields = dict(line.split(': ', 1) for line in
              Path('/root/nac-initial-admin.txt').read_text().splitlines() if ': ' in line)
context = ssl._create_unverified_context()
base = 'https://127.0.0.1:8443/api/v1'


def request(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {'Content-Type': 'application/json'} if data is not None else {}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, context=context, timeout=20) as response:
        return json.load(response)


token = request('POST', '/auth/login',
                {'email': fields['email'], 'password': fields['password']})['token']
result = request('GET', '/sessions?limit=100', token=token)
sessions = result.get('sessions', result.get('items', []))
for session in sessions:
    if session.get('mac', '').lower() != '00:50:00:00:02:00':
        continue
    detail = request('GET', '/sessions/' + session['id'], token=token)
    for coa in detail.get('coa_requests', []):
        if coa.get('id') in ids and coa['id'] not in seen:
            seen.add(coa['id'])
            print('COA', json.dumps({key: coa.get(key) for key in
                  ('id', 'kind', 'state', 'created_at', 'requested_vlan',
                   'attempts', 'error_cause', 'detail')}, sort_keys=True))
    for decision in detail.get('decisions', []):
        if decision.get('id') in decision_ids and decision['id'] not in seen:
            seen.add(decision['id'])
            print('DECISION', json.dumps({key: decision.get(key) for key in
                  ('id', 'action', 'segment', 'matched_rule', 'policy_version')},
                  sort_keys=True))
