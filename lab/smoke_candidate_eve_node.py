"""Bounded start/console/stop check of only candidate control node 15."""
import http.cookiejar
import json
from pathlib import Path
import select
import socket
import time
import urllib.request

base = 'http://127.0.0.1/api'
lab = '/labs/NAC-Pilot-Candidate.unl'
node_path = lab + '/nodes/15'
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def call(path, method='GET', data=None):
    request = urllib.request.Request(base+path, method=method, data=None if data is None else json.dumps(data).encode(), headers={'Content-Type':'application/json'})
    with opener.open(request, timeout=30) as response:
        answer = json.load(response)
    if answer.get('status') != 'success':
        raise RuntimeError((path,answer))
    return answer.get('data', answer)

call('/auth/login','POST',{'username':'admin','password':Path('/root/eve-nac-lab.webpw').read_text().strip()})
before = call(node_path)
if before['status'] != 0:
    raise SystemExit('candidate control must start stopped')
print('NODE_BEFORE',before['status'],flush=True)
log = bytearray()
try:
    print('START_RESULT',call(node_path+'/start'),flush=True)
    for _ in range(20):
        current = call(node_path)
        if current['status'] == 2:
            break
        time.sleep(1)
    print('NODE_AFTER_START',current['status'],flush=True)
    try:
        with socket.create_connection(('127.0.0.1',32783),timeout=5) as sock:
            sock.sendall(b'\r\n')
            end = time.monotonic()+55
            while time.monotonic()<end:
                if select.select([sock],[],[],0.2)[0]:
                    chunk=sock.recv(65536)
                    if not chunk: break
                    log.extend(chunk)
                    if b'login:' in log: break
            if b'login:' in log:
                sock.sendall(b'root\r\n')
                prompt = bytearray()
                end = time.monotonic()+5
                while time.monotonic()<end and b'Password:' not in prompt:
                    if select.select([sock],[],[],0.2)[0]:
                        prompt.extend(sock.recv(65536))
                if b'Password:' not in prompt:
                    raise RuntimeError('root password prompt not observed')
                secret = Path('/root/nac-pilot-control.rootpw').read_text().strip()
                sock.sendall(secret.encode()+b'\r\n')
                time.sleep(0.6)
                sock.sendall(b'ip -br addr show; ip route; df -h /; command -v freeradius || echo FREERADIUS_NOT_INSTALLED\r\n')
                observed = bytearray()
                end = time.monotonic()+4
                while time.monotonic()<end:
                    if select.select([sock],[],[],0.2)[0]:
                        observed.extend(sock.recv(65536))
                clean = observed.decode(errors='replace').replace(secret,'[REDACTED]')
                print('GUEST_CHECK',repr(clean[-1800:]),flush=True)
    except OSError as exc:
        print('CONSOLE_ERROR',type(exc).__name__,str(exc),flush=True)
    path = Path('/root/nac-qualification/candidate-eve-control-boot.log')
    path.write_bytes(log)
    print('CONSOLE_BYTES',len(log),'LOGIN_PROMPT',b'login:' in log, 'TAIL',repr(log[-650:].decode(errors='replace')),flush=True)
finally:
    print('STOP_RESULT',call(node_path+'/stop'),flush=True)
    for _ in range(15):
        after = call(node_path)
        if after['status'] == 0: break
        time.sleep(1)
    print('NODE_AFTER_STOP',after['status'],flush=True)
