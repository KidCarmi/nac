"""Operate candidate control node 15 through its EVE serial console.

Run inside EVE with CONTROL_ACTION=inventory|stage|install_start|install_check.
The guest has one isolated NIC and no SSH listener. Private key bytes are
sent only on the local EVE serial socket; never printed or exported.
"""

import base64
from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import secrets
import select
import socket
import time


PORT = 32803
ROOT = Path("/root/nac-pki")
DIST = Path(os.environ.get("CANDIDATE_BUNDLE_DIR", "/root/nac-pilot-build/source-c121a95/dist"))
BUNDLE_PORT = int(os.environ.get("CANDIDATE_BUNDLE_PORT", "18084"))
if BUNDLE_PORT not in {18084}:
    raise SystemExit("invalid CANDIDATE_BUNDLE_PORT")
BUNDLE = "nac-pilot-pilot-rc2.tar.gz"
GUEST = "/root/nac-pilot-install"


def read_for(sock, seconds):
    result = bytearray()
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if select.select([sock], [], [], 0.1)[0]:
            part = sock.recv(65536)
            if not part:
                raise RuntimeError("candidate serial console closed")
            result.extend(part)
    return bytes(result)


class Console:
    def __init__(self):
        self.sock = socket.create_connection(("127.0.0.1", PORT), timeout=5)
        self.sock.sendall(b"\r\n")
        greeting = read_for(self.sock, 1)
        if b"login:" in greeting:
            self.sock.sendall(b"root\r\n")
            read_for(self.sock, 0.5)
            password = Path("/root/nac-pilot-control.rootpw").read_text().strip()
            self.sock.sendall(password.encode() + b"\r\n")
            password = None
            read_for(self.sock, 1)
        self.sock.sendall(b"stty -echo\r\n")
        read_for(self.sock, 0.3)

    def close(self):
        try:
            self.sock.sendall(b"stty echo\r\n")
            read_for(self.sock, 0.2)
        finally:
            self.sock.close()

    def run(self, command, timeout=15):
        marker = secrets.token_hex(8)
        begin = ("NAC_BEGIN_" + marker).encode()
        end = ("NAC_END_" + marker).encode()
        wrapped = (f"printf '{begin.decode()}\\n'; {command}; nac_rc=$?; "
                   f"printf '{end.decode()} %s\\n' \"$nac_rc\"\r\n")
        self.sock.sendall(wrapped.encode())
        output = bytearray()
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if select.select([self.sock], [], [], 0.2)[0]:
                part = self.sock.recv(65536)
                if not part:
                    raise RuntimeError("candidate serial console closed")
                output.extend(part)
                if end in output and b"\n" in output.split(end, 1)[1]:
                    break
        normalized = bytes(output).replace(b"\r", b"")
        lines = normalized.split(b"\n")
        start = next((i for i, line in enumerate(lines) if line.strip() == begin), None)
        finish = next((i for i, line in enumerate(lines) if line.startswith(end + b" ")), None)
        if start is None or finish is None or finish <= start:
            raise RuntimeError("serial command markers were not observed: " + repr(normalized[-500:]))
        code = int(lines[finish].split()[1])
        body = b"\n".join(lines[start + 1:finish]).decode(errors="replace").strip()
        if code:
            raise RuntimeError(f"serial command failed ({code}): {body[-500:]}")
        return body


def stage_file(console, source, target, mode):
    data = source.read_bytes()
    encoded = base64.b64encode(data).decode()
    payload = f"base64 -d > {target} <<'NAC_STAGE_EOF'\n{encoded}\nNAC_STAGE_EOF\nchmod {mode:o} {target}"
    console.run(payload, timeout=15)
    observed = console.run(f"sha256sum {target}").split()[0]
    if observed != hashlib.sha256(data).hexdigest():
        raise RuntimeError("staging checksum mismatch: " + source.name)
    if source.name.endswith(".key"):
        print("STAGED", source.name, "root-only; checksum verified", flush=True)
    else:
        print("STAGED", source.name, "sha256", observed, flush=True)


def main():
    action = os.environ.get("CONTROL_ACTION", "")
    if action not in {"inventory", "stage", "install_start", "install_check", "install_diag", "verify", "admin_password", "eapi_lab_tls", "eapi_legacy_tls", "eapi_tls_status", "eapi_diag", "stop_nacd", "start_nacd", "restart_components", "diagnose_publish"}:
        raise SystemExit("set CONTROL_ACTION=inventory|stage|install_start|install_check|verify|admin_password|stop_nacd|start_nacd|restart_components")
    console = Console()
    try:
        if action == "inventory":
            print(console.run("ip -br addr; ip route; df -h / | tail -1; command -v python3; command -v freeradius || true"), flush=True)
        elif action == "stage":
            print(console.run(f"test ! -e {GUEST} && install -d -m 0700 {GUEST}/pki"), flush=True)
            for source, target, mode in (
                (ROOT / "ca.crt", f"{GUEST}/pki/ca.crt", 0o644),
                (ROOT / "server.crt", f"{GUEST}/pki/server.crt", 0o644),
                (ROOT / "server.key", f"{GUEST}/pki/server.key", 0o600),
            ):
                stage_file(console, source, target, mode)
            for name in (BUNDLE, BUNDLE + ".sha256"):
                command = ("python3 -c 'import urllib.request; "
                           f"urllib.request.urlretrieve(\"http://10.70.99.254:{BUNDLE_PORT}/{name}\", "
                           f"\"{GUEST}/{name}\")'")
                console.run(command, timeout=40)
                expected = hashlib.sha256((DIST / name).read_bytes()).hexdigest()
                actual = console.run(f"sha256sum {GUEST}/{name}").split()[0]
                if actual != expected:
                    raise RuntimeError("bundle transfer hash mismatch: " + name)
                print("STAGED", name, "sha256", actual, flush=True)
            print("BUNDLE_VERIFY", console.run(f"cd {GUEST} && sha256sum -c {BUNDLE}.sha256"), flush=True)
            print("UNPACK", console.run(f"cd {GUEST} && tar xzf {BUNDLE} && cat nac-pilot-pilot-rc2/BUNDLE-MANIFEST.txt"), flush=True)
        elif action == "install_start":
            if console.run("test ! -e /root/nac-install.rc && echo READY") != "READY":
                raise RuntimeError("install result already exists")
            command = (f"nohup bash -c 'cd {GUEST}/nac-pilot-pilot-rc2 && "
                       f"bash deployments/pilot/install.sh --proxy http://10.70.99.254:3128 "
                       f"--ca {GUEST}/pki/ca.crt --server-cert {GUEST}/pki/server.crt "
                       f"--server-key {GUEST}/pki/server.key; "
                       "printf \"%s\\n\" \"$?\" > /root/nac-install.rc' "
                       "> /root/nac-install.log 2>&1 </dev/null & echo NAC_INSTALL_STARTED")
            print("INSTALL_START_UTC", datetime.now(timezone.utc).isoformat(timespec="seconds"), flush=True)
            print(console.run(command), flush=True)
        elif action == "install_check":
            print(console.run("if test -f /root/nac-install.rc; then printf 'INSTALL_RC='; cat /root/nac-install.rc; else echo INSTALL_RUNNING; fi; wc -c < /root/nac-install.log; grep -E '^==>|^FreeRADIUS |^ERROR:' /root/nac-install.log | tail -15"), flush=True)
        elif action == "install_diag":
            print(console.run("date -u; tail -30 /root/nac-install.log; ps -eo pid,etime,stat,args | grep -E 'apt-get|apt |dpkg|install.sh' | grep -v grep || true; curl -sS --max-time 5 -o /dev/null -w 'proxy_http=%{http_code}\\n' --proxy http://10.70.99.254:3128 http://archive.ubuntu.com/ubuntu/dists/noble/InRelease", timeout=18), flush=True)
        elif action == "verify":
            print(console.run("stat -c 'INSTALL_COMPLETED_AT=%y' /root/nac-install.rc; systemctl is-active nacd freeradius nac-freeradius-reload.path; curl -fs http://127.0.0.1:8080/readyz; cat /etc/nac/install-report.txt", timeout=20), flush=True)
        elif action == "admin_password":
            print(console.run("sed -n 's/^password: //p' /root/nac-initial-admin.txt"), flush=True)
        elif action == "eapi_lab_tls":
            print(console.run("test -f /etc/nac/nacd.env && grep -qx 'NAC_EAPI_INSECURE_SKIP_VERIFY=false' /etc/nac/nacd.env && test ! -e /root/nacd.env.pre-eapi && cp /etc/nac/nacd.env /root/nacd.env.pre-eapi && sed -i 's/^NAC_EAPI_INSECURE_SKIP_VERIFY=false$/NAC_EAPI_INSECURE_SKIP_VERIFY=true/' /etc/nac/nacd.env && systemctl restart nacd && systemctl is-active nacd && grep '^NAC_EAPI_INSECURE_SKIP_VERIFY=' /etc/nac/nacd.env", timeout=25), flush=True)
        elif action == "eapi_legacy_tls":
            print(console.run("grep -qx 'NAC_EAPI_INSECURE_SKIP_VERIFY=true' /etc/nac/nacd.env && ! grep -q '^GODEBUG=' /etc/nac/nacd.env && cp /etc/nac/nacd.env /root/nacd.env.pre-legacy-tls && printf '\\nGODEBUG=tlsrsakex=1\\n' >> /etc/nac/nacd.env && systemctl restart nacd && systemctl is-active nacd && grep '^GODEBUG=' /etc/nac/nacd.env", timeout=25), flush=True)
        elif action == "eapi_tls_status":
            print(console.run("printf 'env='; grep '^NAC_EAPI_INSECURE_SKIP_VERIFY=' /etc/nac/nacd.env || true; printf 'backup='; test -f /root/nacd.env.pre-eapi && echo yes || echo no; printf 'nacd='; systemctl is-active nacd || true; printf 'tail='; journalctl -u nacd -n 8 --no-pager -o cat | tail -8", timeout=15), flush=True)
        elif action == "eapi_diag":
            print(console.run("date -u; timeout 7 openssl s_client -connect 10.70.99.1:443 -tls1_2 -brief < /dev/null 2>&1 | head -28; curl -k --connect-timeout 4 --max-time 8 -sS -o /dev/null -w 'curl_http=%{http_code}\\n' https://10.70.99.1/command-api 2>&1 | tail -8", timeout=20), flush=True)
        elif action == "stop_nacd":
            print(console.run("systemctl stop nacd; printf 'NACD_STATE='; systemctl is-active nacd || true; printf 'RADIUS_STATE='; systemctl is-active freeradius"), flush=True)
        elif action == "start_nacd":
            print(console.run("systemctl start nacd; printf 'NACD_STATE='; systemctl is-active nacd; for i in $(seq 1 20); do if curl -fs http://127.0.0.1:8080/readyz; then exit 0; fi; sleep 1; done; exit 1", timeout=30), flush=True)
        elif action == "restart_components":
            print(console.run("systemctl restart postgresql freeradius nacd; systemctl is-active postgresql freeradius nacd; curl -fs http://127.0.0.1:8080/readyz", timeout=40), flush=True)
        elif action == "diagnose_publish":
            print(console.run("date -u; systemctl is-active nacd postgresql; runuser -u postgres -- psql -Atc \"select state,wait_event_type,wait_event,count(*) from pg_stat_activity where datname is not null group by 1,2,3 order by 1,2,3\"; journalctl -u nacd -n 18 --no-pager -o short-iso | tail -18", timeout=20), flush=True)
    finally:
        console.close()


if __name__ == "__main__":
    main()

