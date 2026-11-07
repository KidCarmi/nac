"""Run documented rc10 API actions from the isolated control guest.

RC10_ACTION=status|quarantine|restore|reauth|rule-quarantine|rule-corp|preview|publish.
Outputs only sanitized operator evidence; credentials remain guest-local.
"""
import json
import os
from pathlib import Path
import ssl
import urllib.error
import urllib.request

action = os.environ.get('RC10_ACTION', '')
assert action in {'status','quarantine','restore','reauth','rule-quarantine','rule-corp','preview','publish'}
fields = dict(line.split(': ',1) for line in Path('/root/nac-initial-admin.txt').read_text().splitlines() if ': ' in line)
ctx = ssl._create_unverified_context()
base = 'https://127.0.0.1:8443/api/v1'

def req(method,path,body=None,token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {'Content-Type':'application/json'} if data is not None else {}
    if token: headers['Authorization']='Bearer '+token
    r = urllib.request.Request(base+path,data=data,headers=headers,method=method)
    try:
        with urllib.request.urlopen(r,context=ctx,timeout=20) as response:
            raw=response.read()
            return response.status,json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raise RuntimeError(f'{method} {path} HTTP {error.code}: {error.read().decode(errors="replace")[:500]}') from None

_,login=req('POST','/auth/login',{'email':fields['email'],'password':fields['password']})
token=login['token']
def call(method,path,body=None): return req(method,path,body,token)

if action in {'status','quarantine','restore','reauth'}:
    _,result=call('GET','/sessions?limit=30')
    sessions=result.get('sessions',result.get('items',[])) or []
    matches=[s for s in sessions if s.get('mac','').lower()=='00:50:00:00:02:00']
    matches.sort(key=lambda s:s.get('last_auth_at',''),reverse=True)
    if not matches: raise SystemExit('no rc10 EAP-TLS session yet')
    session=matches[0]
    print('SESSION',json.dumps({key:session.get(key) for key in (
        'id','state','started_at','last_auth_at','cert_cn','mac','nas_port_id','override','requested','observed','liveness','endpoint_reachability')},sort_keys=True))
    _,detail=call('GET','/sessions/'+session['id'])
    for d in detail.get('decisions',[])[:2]:
        print('DECISION',json.dumps({key:d.get(key) for key in ('id','action','segment','matched_rule','policy_version','reason')},sort_keys=True))
    if action!='status':
        status,result=call('POST','/sessions/'+session['id']+'/'+{'reauth':'reauthenticate'}.get(action,action))
        print('ACTION',action,'HTTP',status,'COA',json.dumps({key:result.get('coa',result).get(key) for key in ('id','kind','state','requested_vlan','created_at')},sort_keys=True))
elif action in {'rule-quarantine','rule-corp'}:
    _,draft=call('GET','/policy-draft')
    rule=next(r for r in draft['rules'] if r['name']=='bound-eap-corp')
    target='quarantine' if action=='rule-quarantine' else 'corp'
    payload={key:rule[key] for key in ('name','priority','enabled','conditions','action')}
    payload['segment']=target
    status,changed=call('PUT','/policies/'+rule['id'],payload)
    print('RULE_UPDATE',status,changed['name'],changed['segment'])
elif action=='preview':
    _,result=call('GET','/policy-draft/preview')
    print('PREVIEW',json.dumps({key:result.get(key) for key in ('draft_digest','active_version','cases_examined','changed','insufficient_evidence','rows')},sort_keys=True))
elif action=='publish':
    _,draft=call('GET','/policy-draft')
    status,result=call('POST','/policy-draft/publish',{'expected_digest':draft['digest'],'note':'ASTRA rc10 qualification'})
    print('PUBLISHED',status,result['version']['number'],result['version']['digest'])
