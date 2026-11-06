"""One-NIC candidate endpoint console driver for fresh TCP admission probes."""

import base64
import hashlib
import os
from pathlib import Path
import re
import select
import socket
import time


PORT = 32780
PASSWORD = Path("/root/nac-linux.rootpw").read_text().strip()


def read(sock, seconds):
    data = bytearray()
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if select.select([sock], [], [], 0.1)[0]:
            part = sock.recv(65536)
            if not part:
                break
            data.extend(part)
    return re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", data.decode(errors="replace")).replace(PASSWORD, "[REDACTED]").replace("\r", "")


with socket.create_connection(("127.0.0.1", PORT), timeout=5) as sock:
    sock.sendall(b"\r\n")
    greeting = read(sock, 0.8)
    if "login:" in greeting:
        sock.sendall(b"root\r\n")
        read(sock, 0.4)
        sock.sendall(PASSWORD.encode() + b"\r\n")
        read(sock, 1.2)

    def cmd(value, seconds=1.5):
        sock.sendall(value.encode() + b"\r\n")
        return read(sock, seconds)

    def probe(segment):
        octet = 10 if segment == "corp" else 20
        marker = "NAC_TCP_" + segment.upper()
        command = (f"if nc -s 10.70.{octet}.50 -vz -w2 10.70.{octet}.10 8080; "
                   f"then echo {marker}=OPEN; else echo {marker}=BLOCKED; fi")
        output = cmd(command, 3.5)
        lines = {line.strip() for line in output.splitlines()}
        if marker + "=OPEN" in lines:
            return "OPEN"
        if marker + "=BLOCKED" in lines:
            return "BLOCKED"
        raise RuntimeError("TCP probe did not produce an exact result line: " + output[-300:])

    action = os.environ.get("ENDPOINT_ACTION", "")
    if action == "inventory":
        print("INTERFACES", cmd("ls /sys/class/net; ip -br addr; ip route"))
        print("SUPPLICANT", cmd("wpa_cli -i ens3 status; pgrep -af wpa_supplicant || true"))
        print("CONFIG_FILES", cmd("ls -l /root/wired-*.conf /root/nac-endpoint.pem /root/nac-endpoint.key /root/nac-ca.pem 2>&1"))
        print("CERT", cmd("openssl x509 -in /root/nac-endpoint.pem -noout -serial -issuer -subject 2>&1"))
    elif action == "stage":
        sock.sendall(b"stty -echo\r\n")
        read(sock, 0.3)
        for source, target, mode in (
            ("ca.crt", "nac-ca.pem", 0o644),
            ("endpoint.crt", "nac-endpoint.pem", 0o644),
            ("endpoint.key", "nac-endpoint.key", 0o600),
            ("invalid.crt", "nac-invalid.pem", 0o644),
            ("invalid.key", "nac-invalid.key", 0o600),
            ("wired-valid.conf", "wired-valid.conf", 0o600),
            ("wired-invalid.conf", "wired-invalid.conf", 0o600),
        ):
            data = (Path("/root/nac-pki") / source).read_bytes()
            encoded = base64.b64encode(data).decode()
            cmd(f"base64 -d > /root/{target} <<'NAC_STAGE_EOF'\n{encoded}\nNAC_STAGE_EOF\nchmod {mode:o} /root/{target}", 1)
            output = cmd(f"sha256sum /root/{target}", 1)
            if hashlib.sha256(data).hexdigest() not in output:
                raise RuntimeError("candidate endpoint staging hash mismatch: " + target)
            if source.endswith(".key"):
                print("STAGED", target, "root-only; checksum verified")
            else:
                print("STAGED", target, "sha256", hashlib.sha256(data).hexdigest())
        print("IDENTITY", cmd("openssl x509 -in /root/nac-endpoint.pem -noout -serial -issuer -subject"))
        sock.sendall(b"stty echo\r\n")
        read(sock, 0.2)
    elif action == "fresh":
        print("SUPPLICANT_STOP", cmd("wpa_cli -i ens3 terminate 2>&1 || true"))
        print("ADDRESSES", cmd("ip addr flush dev ens3; ip addr add 10.70.10.50/24 dev ens3; ip addr add 10.70.20.50/24 dev ens3; ip -br addr show ens3; ip route"))
        print("INTERFACES", cmd("ls /sys/class/net"))
        print("CORP_TCP", probe("corp"))
        print("QUARANTINE_TCP", probe("quarantine"))
    elif action == "valid_start":
        print("START", cmd("wpa_supplicant -i ens3 -D wired -c /root/wired-valid.conf -B -f /root/wpa-rc1-valid.log", 2))
        print("SUPPLICANT", cmd("wpa_cli -i ens3 status", 2))
    elif action == "invalid_start":
        print("STOP", cmd("wpa_cli -i ens3 terminate 2>&1 || true", 1.5))
        print("START", cmd("wpa_supplicant -i ens3 -D wired -c /root/wired-invalid.conf -B -f /root/wpa-rc1-invalid.log", 2))
        print("SUPPLICANT", cmd("wpa_cli -i ens3 status", 2))
    elif action == "valid_restart":
        print("STOP", cmd("wpa_cli -i ens3 terminate 2>&1 || true", 1.5))
        print("START", cmd("wpa_supplicant -i ens3 -D wired -c /root/wired-valid.conf -B -f /root/wpa-rc1-valid.log", 2))
        print("SUPPLICANT", cmd("wpa_cli -i ens3 status", 2))
    elif action == "stop":
        print("STOP", cmd("wpa_cli -i ens3 terminate 2>&1 || true", 1.5))
        print("SUPPLICANT", cmd("wpa_cli -i ens3 status", 1.5))
    elif action == "reauth":
        print("REAUTH", cmd("wpa_cli -i ens3 reauthenticate", 2))
        print("SUPPLICANT", cmd("wpa_cli -i ens3 status", 2))
    elif action == "probe":
        print("CORP_TCP", probe("corp"))
        print("QUARANTINE_TCP", probe("quarantine"))
    else:
        raise SystemExit("set ENDPOINT_ACTION=inventory|stage|fresh|valid_start|invalid_start|valid_restart|stop|reauth|probe")
