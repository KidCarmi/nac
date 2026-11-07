"""Check rc10 eAPI certificate verification against current disposable EOS cert."""
import json
import os
from pathlib import Path
import ssl
import urllib.request

expected=os.environ.get('EAPI_EXPECT')
assert expected in ('yes','no')
fields=dict(line.split(': ',1) for line in Path('/root/nac-initial-admin.txt').read_text().splitlines() if ': ' in line)
ctx=ssl._create_unverified_context()
base='https://127.0.0.1:8443/api/v1'
def req(path,payload=None,token=None):
    data=json.dumps(payload).encode() if payload is not None else None
    headers={'Content-Type':'application/json'} if data is not None else {}
    if token: headers['Authorization']='Bearer '+token
    with urllib.request.urlopen(urllib.request.Request(base+path,data=data,headers=headers),context=ctx,timeout=12) as r:
        return json.load(r)
token=req('/auth/login',{'email':fields['email'],'password':fields['password']})['token']
view=req('/switches/6ffc10c0-d04e-4ec9-9934-6f6bcca37176/probe',{},token)
state=view['status']['reachable']['state'];detail=view['status']['reachable']['detail']
print('EAPI_PROBE_STATE',state)
print('EAPI_PROBE_DETAIL',detail)
print('EAPI_SKIP_VERIFY',dict(line.split('=',1) for line in Path('/etc/nac/nacd.env').read_text().splitlines() if '=' in line).get('NAC_EAPI_INSECURE_SKIP_VERIFY'))
assert state==expected
if expected=='no': assert 'certificate is valid for 10.70.99.9, not 10.70.99.1' in detail
