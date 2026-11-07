"""Recover the disposable rc10 lab operator for API qualification.

Run only inside the isolated clean rc10 control guest. No credential is printed.
The supported nacd local recovery path revokes existing operator tokens and
records the reset in the product audit log.
"""
import json
from pathlib import Path
import secrets
import subprocess
import urllib.request

version = json.load(urllib.request.urlopen('http://127.0.0.1:8080/version', timeout=5))
if version.get('version') != 'pilot-rc10' or version.get('commit') != 'b39649f':
    raise SystemExit('wrong control guest or candidate')
if '10.70.99.4' not in subprocess.check_output(['ip', '-4', '-br', 'addr'], text=True):
    raise SystemExit('wrong isolated control address')
source = Path('/root/nac-initial-admin.txt')
if not source.is_file() or source.stat().st_mode & 0o077:
    raise SystemExit('root-only initial admin file missing or readable by others')
fields = dict(line.split(': ', 1) for line in source.read_text().splitlines() if ': ' in line)
email = fields.get('email', '')
if not email or 'password' not in fields:
    raise SystemExit('initial admin record incomplete')
new_password = secrets.token_urlsafe(24)
command = ['runuser', '-u', 'nac', '--', 'bash', '-c',
           'set -a; . /etc/nac/nacd.env; exec /usr/local/bin/nacd -reset-password "$1"',
           '_', email]
result = subprocess.run(command, input=new_password+'\n', text=True,
                        capture_output=True, timeout=35)
if result.returncode:
    raise SystemExit('documented local password reset failed: '+result.stderr[-300:])
fields['password'] = new_password
source.write_text('\n'.join(f'{key}: {value}' for key, value in fields.items())+'\n')
source.chmod(0o600)
print('RC10_LAB_OPERATOR_RECOVERED tokens_revoked=true candidate=b39649f')
