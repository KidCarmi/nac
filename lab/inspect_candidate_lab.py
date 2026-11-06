"""Read-only EVE API check for the uploaded, unstarted candidate topology."""
import http.cookiejar
import json
from pathlib import Path
import urllib.request

base = 'http://127.0.0.1/api'
lab = '/labs/NAC-Pilot-Candidate.unl'
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def get(path):
    with opener.open(base + path, timeout=10) as response:
        answer = json.load(response)
    if answer.get('status') != 'success':
        raise RuntimeError(answer)
    return answer['data']

request = urllib.request.Request(base + '/auth/login', method='POST', data=json.dumps({'username':'admin','password':Path('/root/eve-nac-lab.webpw').read_text().strip()}).encode(), headers={'Content-Type':'application/json'})
with opener.open(request, timeout=10) as response:
    login = json.load(response)
if login.get('status') != 'success':
    raise SystemExit('EVE API login failed')
for kind in ('nodes','networks'):
    values = get(lab + '/' + kind)
    items = values.values() if isinstance(values,dict) else values
    print(kind, json.dumps([{'name':item.get('name'),'id':item.get('id'),'image':item.get('image'),'status':item.get('status')} for item in items],sort_keys=True))
print('control', json.dumps({key:value for key,value in get(lab+'/nodes/15').items() if key in ('id','name','image','status','port','console','qemu_options','ram','cpu')},sort_keys=True))
