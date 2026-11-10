"""Documented local operator-API policy diagnostic; lab password stays in guest."""
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import ssl
import sys
import urllib.request

ACTION = sys.argv[1] if len(sys.argv) > 1 else ''
if ACTION not in {'read', 'draft-quarantine', 'preview', 'publish', 'restore-v2', 'details'}:
    raise SystemExit('invalid action')
BASE = 'https://127.0.0.1:8443/api/v1'
TLS = ssl._create_unverified_context()  # disposable self-signed console certificate
password = next(line.removeprefix('password: ') for line in
                Path('/root/nac-initial-admin.txt').read_text().splitlines()
                if line.startswith('password: '))

def api(path, method='GET', body=None, token=None):
    headers = {'Accept': 'application/json'}
    data = None if body is None else json.dumps(body).encode()
    if data is not None:
        headers['Content-Type'] = 'application/json'
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(BASE+path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, context=TLS, timeout=15) as response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else {}

_, auth = api('/auth/login', 'POST', {'email':'admin@nac.local','password':password})
password = None
token = auth['token']
when = datetime.now(timezone.utc).isoformat(timespec='milliseconds')

_, draft = api('/policy-draft', token=token)
_, versions = api('/policy-versions', token=token)
print('ACTION_UTC', when)
print('DRAFT', json.dumps({'digest': draft['digest'],
                           'active_version': draft.get('active_version'),
                           'differs_from_active': draft['differs_from_active'],
                           'validation': draft['validation'],
                           'rules': [{k:r.get(k) for k in ('id','name','priority','enabled','conditions','action','segment')}
                                     for r in draft['rules']]}, sort_keys=True))
print('VERSIONS', json.dumps([{'number':v['number'],'active':v['active'],'digest':v['digest']}
                              for v in versions['versions']],sort_keys=True))
if ACTION == 'details':
    for sid in ('ff304f60-37f8-4027-bda8-de1d3291f2fc',
                '89bec97b-cd89-4a50-ab3b-6505911d41c5',
                '081ae79a-df3c-4e70-ac59-2e4f97e13750',
                '8803b9e6-15a3-4f43-80a1-2be8b186d5bd'):
        _, detail = api('/sessions/'+sid,token=token)
        decision = detail.get('last_decision') or {}
        session = detail.get('session') or {}
        print('SESSION_DECISION',json.dumps({'id':sid,
            'session_state':session.get('state'),'override':session.get('override'),
            'decision_id':decision.get('id'),'policy_version':decision.get('policy_version'),
            'matched_rule':decision.get('matched_rule'),'action':decision.get('action'),
            'segment':decision.get('segment'),'reason':decision.get('reason')},sort_keys=True))
    raise SystemExit(0)
if ACTION == 'read':
    raise SystemExit(0)
if ACTION == 'draft-quarantine':
    if draft.get('active_version') != 2 or len(draft['rules']) != 1:
        raise RuntimeError('expected active v2 and one draft rule')
    rule = draft['rules'][0]
    if rule['name'] != 'bound-eap-corp' or rule['segment'] != 'corp' or rule['action'] != 'allow':
        raise RuntimeError('unexpected draft rule')
    body = {k:rule.get(k) for k in ('name','description','priority','enabled','conditions','action','segment')}
    body['description'] = body['description'] or ''
    body['segment'] = 'quarantine'
    code, result = api('/policies/'+rule['id'],'PUT',body,token)
    print('UPDATED',code,json.dumps({k:result.get(k) for k in ('id','name','segment','action')},sort_keys=True))
elif ACTION == 'preview':
    _, result = api('/policy-draft/preview',token=token)
    rows = [{k:r.get(k) for k in ('case','active_action','active_segment','active_rule',
                                  'draft_action','draft_segment','draft_rule','changed',
                                  'policy_changed','override_applies','insufficient_evidence')}
            for r in result.get('rows',[]) if r.get('case',{}).get('subject','').find('00:50:00:00:02:00')>=0]
    print('PREVIEW',json.dumps({'label':result['label'],'draft_digest':result['draft_digest'],
                                'active_version':result['active_version'],'cases_examined':result['cases_examined'],
                                'changed':result['changed'],'rows':rows[-8:]},sort_keys=True))
elif ACTION == 'publish':
    if draft.get('active_version') != 2 or not draft['differs_from_active'] or not draft['validation']['valid']:
        raise RuntimeError('unexpected or invalid draft')
    if len(draft['rules']) != 1 or draft['rules'][0]['segment'] != 'quarantine':
        raise RuntimeError('expected quarantine draft')
    _, preview = api('/policy-draft/preview',token=token)
    if preview['draft_digest'] != draft['digest']:
        raise RuntimeError('draft changed after preview')
    code, result = api('/policy-draft/publish','POST',
                       {'expected_digest':preview['draft_digest'], 'note':os.environ.get('NAC_POLICY_NOTE', 'ASTRA isolated rc7 post-restart diagnostic')},token)
    print('PUBLISHED',code,json.dumps({'number':result['version']['number'],
                                       'digest':result['version']['digest'],
                                       'semantics':result['semantics']},sort_keys=True))
elif ACTION == 'restore-v2':
    active = next(v for v in versions['versions'] if v['active'])
    if active['number'] <= 2:
        raise RuntimeError('expected a newer active version before restoring v2')
    code, result = api('/policy-versions/2/restore','POST',
                       {'reset_draft':True,'note':os.environ.get('NAC_POLICY_NOTE', 'ASTRA isolated rc7 post-restart diagnostic')},token)
    print('RESTORED',code,json.dumps(result,sort_keys=True)[:1200])
