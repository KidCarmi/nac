"""Repeatable black-box baseline qualification inside the existing EVE guest.

Run through eve_guest.py as `RUN_MODE=baseline python3 -`. This runner uses the
existing lab-only NAC API solely for baseline regression. Candidate acceptance
requires an OPUS-supplied product driver and documented operator workflow.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import select
import socket
import subprocess
import time
import urllib.request

assert os.environ.get('RUN_MODE') == 'baseline', 'candidate driver not delivered; baseline only'
RUN_ID = time.strftime('baseline-%Y%m%dT%H%M%SZ', time.gmtime())
START_UTC = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
ROOT = Path('/root/nac-qualification') / RUN_ID
ROOT.mkdir(parents=True, exist_ok=False)
PASSWORD = Path('/root/nac-linux.rootpw').read_text().strip()
NAC_COMMIT = '4911d2d940797aeb41be3815af1cf23b25573dc1'
BASE = 'http://10.70.99.254:18080'
events = []
results = []
captures = []

def now():
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())

def event(kind, **details):
    item={'time':now(),'kind':kind,**details}
    events.append(item)
    print(json.dumps(item,sort_keys=True),flush=True)

def read(sock, seconds):
    data=bytearray()
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        if select.select([sock],[],[],0.1)[0]:
            part=sock.recv(65536)
            if not part: break
            data.extend(part)
    return data.decode('utf-8','replace')

class Console:
    def __init__(self,port,linux=False):
        self.port=port
        self.sock=socket.create_connection(('127.0.0.1',port),timeout=5)
        self.sock.sendall(b'\r\n')
        greeting=read(self.sock,1)
        if 'login:' in greeting:
            self.sock.sendall((('root' if linux else 'admin')+'\r\n').encode())
            read(self.sock,0.5)
            if linux:
                self.sock.sendall((PASSWORD+'\r\n').encode())
                read(self.sock,1.5)
    def command(self,cmd,seconds=2):
        self.sock.sendall(cmd.encode()+b'\r\n')
        raw=read(self.sock,seconds).replace(PASSWORD,'[REDACTED]')
        clean=re.sub(r'\x1b\[[0-9;?]*[A-Za-z]','',raw).replace('\r','')
        event('console',port=self.port,command=cmd,output=clean[-3000:])
        return clean
    def close(self):
        self.sock.close()

def assess(name,expected,actual,ok,evidence=None):
    result={'test':name,'expected':expected,'actual':actual,'status':'PASS' if ok else 'FAIL','evidence':evidence or []}
    results.append(result)
    event('result',**result)
    return ok

def req(path,method='GET',data=None,token=None):
    headers={'Content-Type':'application/json'}
    if token: headers['Authorization']='Bearer '+token
    payload=None if data is None else json.dumps(data).encode()
    with urllib.request.urlopen(urllib.request.Request(BASE+path,data=payload,headers=headers,method=method),timeout=10) as response:
        return json.load(response)

def auth_token():
    values=dict(line.split('=',1) for line in Path('/root/nac-lab.env').read_text().splitlines() if line and not line.startswith('#'))
    return req('/api/v1/auth/login','POST',{'email':values['NAC_BOOTSTRAP_ADMIN_EMAIL'],'password':values['NAC_BOOTSTRAP_ADMIN_PASSWORD']})['token']

def policy(token):
    return next(item for item in req('/api/v1/policies',token=token)['items'] if item['name']=='lab-wired-endpoint')

def set_segment(token,segment):
    current=policy(token)
    before={key:current[key] for key in ('name','description','priority','enabled','conditions','action','segment')}
    after=dict(before,segment=segment)
    if before['segment'] != segment:
        updated=req('/api/v1/policies/'+current['id'],'PUT',after,token)
    else: updated=current
    event('policy_update',policy_id=current['id'],before=before,after={key:updated[key] for key in before})
    return updated

def capture(interface,name,filter_text):
    log=(ROOT/(name+'.capture.log')).open('wb')
    proc=subprocess.Popen(['tcpdump','-U','-i',interface,'-s','0','-w',str(ROOT/name),filter_text],stdout=log,stderr=subprocess.STDOUT)
    captures.append((proc,log))

def switch_state(switch):
    return switch.command('show dot1x hosts',1.2)

def wait_vlan(switch,vlan,timeout=30):
    end=time.monotonic()+timeout
    while time.monotonic()<end:
        state=switch_state(switch)
        if 'EAPOL SUCCESS' in state and re.search(r'\n'+str(vlan)+r'\s*\n',state):
            return True
        time.sleep(1)
    return False

def wait_failure(switch,timeout=25):
    end=time.monotonic()+timeout
    while time.monotonic()<end:
        state=switch_state(switch)
        if 'EAPOL FAILED' in state:
            return True
        time.sleep(1)
    return False

def address(endpoint,primary):
    first,second=(10,20) if primary==10 else (20,10)
    endpoint.command('ip addr flush dev ens3; ip addr add 10.70.'+str(first)+'.50/24 dev ens3; ip addr add 10.70.'+str(second)+'.50/24 dev ens3; ip -br addr show ens3',2)
    # One data NIC has a primary address for the assigned VLAN and a secondary
    # source address only for a controlled negative probe in the other subnet.
    assert '10.70.10.50/24' in endpoint.command('ip -br addr show ens3',1)
    assert '10.70.20.50/24' in endpoint.command('ip -br addr show ens3',1)

def tcp(endpoint,segment):
    octet=10 if segment=='corp' else 20
    target='10.70.'+str(octet)+'.10'
    source='10.70.'+str(octet)+'.50'
    cmd=f'if nc -s {source} -vz -w2 {target} 8080; then echo NAC_TCP_{segment.upper()}=OPEN; else echo NAC_TCP_{segment.upper()}=BLOCKED; fi'
    out=endpoint.command(cmd,3.5)
    lines={line.strip() for line in out.splitlines()}
    open_marker='NAC_TCP_'+segment.upper()+'=OPEN'
    blocked_marker='NAC_TCP_'+segment.upper()+'=BLOCKED'
    if open_marker in lines: return True
    if blocked_marker in lines: return False
    raise RuntimeError('TCP probe did not produce a result line')

def wait_tcp_open(endpoint,segment,timeout=24):
    start=time.monotonic()
    while time.monotonic()-start < timeout:
        if tcp(endpoint,segment):
            elapsed=round(time.monotonic()-start,1)
            event('tcp_converged',segment=segment,seconds=elapsed)
            return True,elapsed
        time.sleep(1)
    return False,round(time.monotonic()-start,1)

def health():
    subprocess.run(['ip','addr','add','10.70.10.254/24','dev','vnet0_2'],check=True)
    try:
        checks={}
        for segment,addr in (('corp','10.70.10.10'),('quarantine','10.70.20.10')):
            with urllib.request.urlopen('http://'+addr+':8080/',timeout=3) as response:
                checks[segment]=response.status
        return checks
    finally:
        subprocess.run(['ip','addr','del','10.70.10.254/24','dev','vnet0_2'],check=True)

def save():
    (ROOT/'events.jsonl').write_text(''.join(json.dumps(item,sort_keys=True)+'\n' for item in events))
    (ROOT/'results.json').write_text(json.dumps({'run_id':RUN_ID,'nac_commit':NAC_COMMIT,'results':results},indent=2)+'\n')

def save_decisions(control):
    output=control.command('cat /var/log/freeradius/nac-adapter.jsonl',3)
    selected=[]
    for line in output.splitlines():
        try: item=json.loads(line)
        except json.JSONDecodeError: continue
        if isinstance(item,dict) and item.get('time','') >= START_UTC:
            selected.append(item)
    (ROOT/'nac-decisions.jsonl').write_text(''.join(json.dumps(item,sort_keys=True)+'\n' for item in selected))
    event('decision_log_saved',count=len(selected))

endpoint=switch=control=None
token=None
initial_segment=None
try:
    endpoint=Console(32770,True)
    switch=Console(32769)
    control=Console(32773,True)
    metadata={
        'nac_commit':NAC_COMMIT,
        'topology_sha256':hashlib.sha256(Path('/opt/unetlab/labs/NAC-Wired-Qualification.unl').read_bytes()).hexdigest(),
        'adapter_sha256':hashlib.sha256(Path('/root/nac-pki/nac_radius_adapter_v2.py').read_bytes()).hexdigest(),
        'eve_version':subprocess.check_output(['dpkg-query','-W','eve-ng'],text=True).strip(),
        'eos_version_output':switch.command('show version | include Software image version',1.5).strip(),
        'radius_version_output':control.command('freeradius -v 2>&1 | head -1',1.5).strip(),
        'switch_dot1x_config':switch.command('show running-config section dot1x',1.5).strip(),
        'switch_port_config':switch.command('show running-config interfaces Ethernet1',1.5).strip(),
        'radius_adapter_module_sha256':control.command('sha256sum /etc/freeradius/3.0/nac-lab/adapter.py',1.5).strip(),
    }
    (ROOT/'metadata.json').write_text(json.dumps(metadata,indent=2)+'\n')
    event('run_start',run_id=RUN_ID,metadata=metadata)
    token=auth_token()
    initial_segment=policy(token)['segment']
    assert initial_segment=='corp', 'baseline should start in corp; refusing to modify unknown policy state'
    capture('vnet0_1','endpoint-eapol.pcap','ether proto 0x888e')
    capture('vnet0_4','radius.pcap','udp port 1812 or udp port 3799')
    time.sleep(1)

    healthy=health()
    assess('service_health','both HTTP 200 from their own EVE bridges',str(healthy),healthy=={'corp':200,'quarantine':200})
    assert '10.70.10.254' not in subprocess.check_output(['ip','-br','addr','show','vnet0_2'],text=True)
    assert subprocess.check_output(['sysctl','-n','net.ipv4.ip_forward'],text=True).strip()=='0'

    endpoint.command('wpa_cli -i ens3 terminate',1.5)
    switch.command('clear dot1x host all',1.5)
    address(endpoint,10)
    time.sleep(2)
    blocked=not tcp(endpoint,'corp')
    assess('fresh_unauthenticated','corp TCP blocked on one-NIC endpoint',f'corp_open={not blocked}; '+switch_state(switch)[-300:],blocked,['endpoint-eapol.pcap','events.jsonl'])

    endpoint.command('wpa_supplicant -i ens3 -D wired -c /root/wired-valid.conf -B -f /root/wpa-qualification-valid.log',2)
    authorized=wait_vlan(switch,10)
    corp,corp_seconds=wait_tcp_open(endpoint,'corp')
    quarantine=tcp(endpoint,'quarantine')
    assess('valid_corp','EAP-TLS SUCCESS, VLAN 10, corp open, quarantine blocked',f'vlan10={authorized},corp_open={corp},corp_convergence_s={corp_seconds},quarantine_open={quarantine}',authorized and corp and not quarantine,['endpoint-eapol.pcap','radius.pcap','events.jsonl'])

    endpoint.command('wpa_cli -i ens3 terminate',1.5)
    switch.command('clear dot1x host all',1.5)
    endpoint.command('wpa_supplicant -i ens3 -D wired -c /root/wired-invalid.conf -B -f /root/wpa-qualification-invalid.log',2)
    rejected=wait_failure(switch)
    corp=tcp(endpoint,'corp')
    assess('invalid_certificate','EAP-TLS reject, no VLAN, corp blocked',f'eap_failed={rejected},corp_open={corp}',rejected and not corp,['endpoint-eapol.pcap','radius.pcap','events.jsonl'])

    endpoint.command('wpa_cli -i ens3 terminate',1.5)
    switch.command('clear dot1x host all',1.5)
    endpoint.command('wpa_supplicant -i ens3 -D wired -c /root/wired-valid.conf -B -f /root/wpa-qualification-valid2.log',2)
    assert wait_vlan(switch,10), 'could not restore valid corp prerequisite'
    set_segment(token,'quarantine')
    switch.command('clear dot1x host all',1.5)
    endpoint.command('wpa_cli -i ens3 reauthenticate',2)
    authorized=wait_vlan(switch,20)
    address(endpoint,20)
    quarantine,quarantine_seconds=wait_tcp_open(endpoint,'quarantine')
    corp=tcp(endpoint,'corp')
    assess('policy_to_quarantine','same identity and port; only NAC rule segment changed; VLAN 20, quarantine open, corp blocked',f'vlan20={authorized},quarantine_open={quarantine},quarantine_convergence_s={quarantine_seconds},corp_open={corp}',authorized and quarantine and not corp,['endpoint-eapol.pcap','radius.pcap','events.jsonl'])

    set_segment(token,'corp')
    switch.command('clear dot1x host all',1.5)
    endpoint.command('wpa_cli -i ens3 reauthenticate',2)
    authorized=wait_vlan(switch,10)
    address(endpoint,10)
    corp,corp_seconds=wait_tcp_open(endpoint,'corp')
    quarantine=tcp(endpoint,'quarantine')
    assess('policy_restore','previous rule restored; VLAN 10, corp open, quarantine blocked',f'vlan10={authorized},corp_open={corp},corp_convergence_s={corp_seconds},quarantine_open={quarantine}',authorized and corp and not quarantine,['endpoint-eapol.pcap','radius.pcap','events.jsonl'])
finally:
    if token and initial_segment:
        try: set_segment(token,initial_segment)
        except Exception as error: event('cleanup_error',error=type(error).__name__,step='restore_policy')
    for proc,log in captures:
        proc.terminate()
        try: proc.wait(timeout=5)
        except subprocess.TimeoutExpired: proc.kill()
        log.close()
    if control:
        try: save_decisions(control)
        except Exception as error: event('cleanup_error',error=type(error).__name__,step='save_decisions')
    for console in (endpoint,switch,control):
        if console: console.close()
    event('run_end',run_id=RUN_ID)
    save()
    print('RESULT_DIR='+str(ROOT),flush=True)
