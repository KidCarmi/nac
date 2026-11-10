"""Bounded 100-pair NAC role-CoA runner; parameterized publication copy.

Run on the disposable candidate guest, alongside independent endpoint probes
and packet captures. Environment: NAC_CA_FILE, NAC_PASSWORD_FILE,
NAC_ENDPOINT_MAC, and optionally NAC_API_BASE, NAC_ADMIN, NAC_OUTPUT.
The lab-executed source and raw outputs remain in the local lab handoff.
"""
import hashlib
import json
import os
from pathlib import Path
import ssl
import time
import urllib.error
import urllib.request

EXPECTED_NACD_SHA256 = 'dd2b61371585d317bf07684733996c69e265dfdb9e762e354eeca4f7821b49d7'
COUNT = 100
BASE = os.environ.get('NAC_API_BASE', 'https://127.0.0.1:8443/api/v1')
MAC = os.environ['NAC_ENDPOINT_MAC'].lower()
ADMIN = os.environ.get('NAC_ADMIN', 'admin@nac.local')
PASSWORD = Path(os.environ['NAC_PASSWORD_FILE']).read_text().strip()
CTX = ssl.create_default_context(cafile=os.environ['NAC_CA_FILE'])
OUT = Path(os.environ.get('NAC_OUTPUT', '/run/nac-100-cycle.jsonl'))


def request(method, path, body=None, token=None):
    data = None if body is None else json.dumps(body).encode()
    headers = {'Accept': 'application/json'}
    if data is not None:
        headers['Content-Type'] = 'application/json'
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, context=CTX, timeout=30) as resp:
            return resp.status, json.load(resp)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def record(**fields):
    with OUT.open('a') as stream:
        stream.write(json.dumps(fields, sort_keys=True) + '\n')


def active(token):
    code, result = request('GET', '/sessions', token=token)
    if code != 200:
        raise RuntimeError(f'sessions HTTP {code}')
    matches = [s for s in result['sessions']
               if s.get('mac', '').lower() == MAC and s.get('state') == 'active']
    if len(matches) != 1:
        raise RuntimeError(f'expected one active endpoint session, got {len(matches)}')
    return matches[0]


def perform(token, cycle, kind, expected_role):
    session_id = active(token)['id']
    start = time.monotonic()
    code, result = request('POST', f'/sessions/{session_id}/{kind}', token=token)
    response_ms = round((time.monotonic() - start) * 1000, 3)
    coa_id = (result.get('coa') or {}).get('id')
    state = None
    deadline = time.monotonic() + 8
    while code == 202 and coa_id and time.monotonic() < deadline:
        last = (active(token).get('last_coa') or {})
        if last.get('id') == coa_id and last.get('state') in ('acked', 'nak', 'timeout', 'failed'):
            state = last['state']
            break
        time.sleep(0.08)
    record(cycle=cycle, action=kind, expected_role=expected_role, http=code,
           request_to_response_ms=response_ms, coa_state=state)
    if code != 202 or state != 'acked':
        raise RuntimeError(f'cycle {cycle} {kind}: HTTP {code}, CoA {state}')
    time.sleep(1.1)


def main():
    if OUT.exists():
        raise SystemExit('output already exists; refusing overwrite')
    digest = hashlib.sha256(Path('/usr/local/bin/nacd').read_bytes()).hexdigest()
    if digest != EXPECTED_NACD_SHA256:
        raise SystemExit('candidate binary hash mismatch')
    code, login = request('POST', '/auth/login', {'email': ADMIN, 'password': PASSWORD})
    if code != 200:
        raise RuntimeError(f'login HTTP {code}')
    token = login['token']
    completed = 0
    try:
        for cycle in range(1, COUNT + 1):
            perform(token, cycle, 'quarantine', 'quarantine')
            perform(token, cycle, 'restore', 'corp')
            completed = cycle
    finally:
        record(kind='finish', completed=completed, planned=COUNT)


if __name__ == '__main__':
    main()
