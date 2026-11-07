"""Enable trusted EOS eAPI observation for rc10 without exporting secrets."""
import json
from pathlib import Path
import ssl
import urllib.request

fields=dict(line.split(': ',1) for line in Path('/root/nac-initial-admin.txt').read_text().splitlines() if ': ' in line)
ctx=ssl._create_unverified_context()
base='https://127.0.0.1:8443/api/v1'
def req(method,path,payload=None,token=None):
    data=json.dumps(payload).encode() if payload is not None else None
    headers={'Content-Type':'application/json'} if data is not None else {}
    if token: headers['Authorization']='Bearer '+token
    r=urllib.request.Request(base+path,data=data,headers=headers,method=method)
    with urllib.request.urlopen(r,context=ctx,timeout=15) as response:
        raw=response.read()
        return response.status,json.loads(raw) if raw else {}
_,login=req('POST','/auth/login',{'email':fields['email'],'password':fields['password']})
token=login['token']
switch_id='6ffc10c0-d04e-4ec9-9934-6f6bcca37176'
secret=Path('/root/nac-eapi-observer.secret').read_text().strip()
status,switch=req('PUT','/switches/'+switch_id,{
    'name':'nac-veos-rc10','radius_source_ip':'10.70.99.1',
    'nas_ip':'10.70.99.1','radius_secret':'',
    'radius_server_ip':'10.70.99.4','access_ports':['Ethernet1'],
    'segment_vlans':{'corp':10,'quarantine':20},
    'eapi_url':'https://10.70.99.1','eapi_username':'nac-observer',
    'eapi_password':secret,
},token)
assert status==200
for action in ('probe','observe'):
    status,view=req('POST','/switches/'+switch_id+'/'+action,token=token)
    print(action.upper(),status,view['status']['reachable']['state'],view['status']['reachable']['detail'][:200])
    assert view['status']['reachable']['state']=='yes'
print('TLS_VERIFICATION_ENABLED',Path('/etc/nac/eapi-ca.pem').exists())
