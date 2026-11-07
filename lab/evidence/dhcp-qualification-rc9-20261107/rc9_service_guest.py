"""Bounded nacd outage/recovery control on exact pilot-rc9 isolated node."""
import os
import sys

sys.path.insert(0, '/root/nac-qualification')
from dhcp_candidate_serial_guest import Console

ACTION = os.environ.get('RC9_SERVICE_ACTION', '')
BIN = 'd2d93e3ddc40a7eadcddfd3275f3d8a7aa5610ca07b43eb219bb9d59267a3749'
if ACTION not in {'stop', 'start', 'status'}:
    raise SystemExit('invalid RC9_SERVICE_ACTION')
console = Console()
try:
    if ACTION == 'stop':
        installed = console.run('sha256sum /usr/local/bin/nacd').split()[0]
        if installed != BIN:
            raise RuntimeError('wrong candidate binary')
        print(console.run('date -u +%FT%T.%3NZ; systemctl stop nacd; '
                          'systemctl is-active nacd || true; systemctl is-active freeradius', timeout=20))
    elif ACTION == 'start':
        print(console.run('date -u +%FT%T.%3NZ; systemctl start nacd; '
                          'systemctl is-active nacd freeradius; '
                          'sha256sum /proc/$(systemctl show -p MainPID --value nacd)/exe; '
                          'curl -fsS --max-time 4 http://127.0.0.1:8080/readyz', timeout=25))
    else:
        print(console.run('date -u +%FT%T.%3NZ; systemctl is-active nacd freeradius; '
                          'curl -fsS --max-time 4 http://127.0.0.1:8080/readyz || true', timeout=15))
finally:
    console.close()

