"""Package the existing disposable client certificate for Windows inside EVE."""
from pathlib import Path
import secrets
import subprocess

root=Path('/root/nac-qualification/windows-2025-endpoint')
password=root/'pfx.secret'
archive=root/'wired-client.pfx'
if password.exists() or archive.exists():
    raise SystemExit('PFX already prepared')
password.write_text(secrets.token_urlsafe(32)+'\n')
password.chmod(0o600)
subprocess.run(['openssl','pkcs12','-export','-inkey','/root/nac-pki/endpoint.key',
    '-in','/root/nac-pki/endpoint.crt','-certfile','/root/nac-pki/ca.crt',
    '-out',str(archive),'-passout','file:'+str(password)],check=True,capture_output=True)
archive.chmod(0o600)
print('DISPOSABLE_CLIENT_PFX_READY_GUEST_ONLY',archive.stat().st_size)
