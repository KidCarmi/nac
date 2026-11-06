"""Add a fixed, printer-only MAB rule on the isolated cloned control node.

The original Arista control disk and product policy are not modified. This
tests vendor switch/RADIUS enforcement only; it is not a NAC MAB feature.
"""

import base64
from pathlib import Path
import secrets
import select
import socket
import time


PORT = 32793
PASSWORD = Path("/root/nac-pilot-control.rootpw").read_text().strip()
PATCH_SOURCE = r"""
from pathlib import Path
import os
import subprocess

site = Path("/etc/freeradius/3.0/sites-available/nac")
backup = site.with_name("nac.pre-mikrotik-mab")
original = site.read_text()
if backup.exists() and backup.read_text() != original:
    raise SystemExit("Existing backup differs from active site; refusing overwrite")
authorize = "\tauthorize {\n\t\tfilter_username\n"
post_auth = "\tpost-auth {\n\t\tupdate control {\n"
if original.count(authorize) != 1 or original.count(post_auth) != 1:
    raise SystemExit("Unexpected FreeRADIUS site structure")
printer = '''\t\t# ASTRA fixed MAB qualification: exact printer MAC and switch port only.
\t\t# A MAC alone is spoofable; this does not establish EAP-TLS identity.
\t\tif (&request:User-Name == "005000000200" && &request:Calling-Station-Id == "00-50-00-00-02-00" && &request:NAS-Port-Id == "ether1" && !&request:EAP-Message) {
\t\t\tupdate control {
\t\t\t\t&Auth-Type := Accept
\t\t\t\t&Tmp-String-1 := "mab-fixed-allow"
\t\t\t}
\t\t\tupdate reply {
\t\t\t\t&Tunnel-Type := VLAN
\t\t\t\t&Tunnel-Medium-Type := IEEE-802
\t\t\t\t&Tunnel-Private-Group-Id := "20"
\t\t\t\t&Reply-Message := "fixed-mab-printer-quarantine"
\t\t\t}
\t\t\treturn
\t\t}
'''
skip_nac = '''\t\tif (&control:Tmp-String-1 == "mab-fixed-allow") {
\t\t\treturn
\t\t}
'''
modified = original.replace(authorize, authorize + printer).replace(post_auth, "\tpost-auth {\n" + skip_nac + "\t\tupdate control {\n")
if not backup.exists():
    backup.write_text(original)
    os.chmod(backup, 0o600)
site.write_text(modified)
check = subprocess.run(["freeradius", "-XC"], capture_output=True, text=True)
if check.returncode:
    site.write_text(original)
    errors = [line for line in (check.stdout + check.stderr).splitlines()
              if any(word in line.lower() for word in ("error", "failed", "invalid", "parse"))]
    raise SystemExit("RADIUS config rejected, restored original: " + " | ".join(errors[-8:])[-1000:])
subprocess.run(["systemctl", "restart", "freeradius"], check=True)
subprocess.run(["systemctl", "is-active", "--quiet", "freeradius"], check=True)
print("FIXED_MAB_POLICY_READY exact MAC 005000000200, port ether1, VLAN 20")
"""


def read(sock, seconds):
    data = bytearray()
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if select.select([sock], [], [], 0.1)[0]:
            part = sock.recv(65536)
            if not part:
                break
            data.extend(part)
    return data.decode(errors="replace").replace(PASSWORD, "[REDACTED]").replace("\r", "")


with socket.create_connection(("127.0.0.1", PORT), timeout=5) as sock:
    sock.sendall(b"\r\n")
    greeting = read(sock, 0.7)
    if "login:" in greeting:
        sock.sendall(b"root\r\n")
        read(sock, 0.3)
        sock.sendall(PASSWORD.encode() + b"\r\n")
        read(sock, 1.0)
    sock.sendall(b"stty -echo\r\n")
    read(sock, 0.3)
    encoded = base64.b64encode(PATCH_SOURCE.encode()).decode()
    marker = secrets.token_hex(8)
    command = ("base64 -d > /root/mab_radius_patch.py <<'MAB_PATCH_EOF'\n"
               + encoded + "\nMAB_PATCH_EOF\n"
               + "chmod 600 /root/mab_radius_patch.py; "
               + "python3 /root/mab_radius_patch.py; rc=$?; "
               + f"printf '\nMAB_PATCH_{marker} %s\n' \"$rc\"\r\n")
    sock.sendall(command.encode())
    output = bytearray()
    deadline = time.monotonic() + 35
    end = f"MAB_PATCH_{marker}".encode()
    while time.monotonic() < deadline:
        if select.select([sock], [], [], 0.2)[0]:
            part = sock.recv(65536)
            if not part:
                break
            output.extend(part)
            if end in output and b"\n" in output.split(end, 1)[1]:
                break
    text = bytes(output).replace(b"\r", b"").decode(errors="replace")
    if end.decode() not in text:
        raise RuntimeError("No MAB patch completion marker: " + text[-700:])
    line = next(line for line in text.splitlines() if line.startswith(end.decode() + " "))
    code = int(line.split()[1])
    if code:
        raise RuntimeError("MAB patch failed: " + text[-1000:])
    print("FIXED_MAB_POLICY_READY exact printer MAC, ether1, VLAN20; original Arista disk untouched")
    sock.sendall(b"stty echo\r\n")
    read(sock, 0.2)
