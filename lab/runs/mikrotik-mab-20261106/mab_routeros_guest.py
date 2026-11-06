"""Configure or inspect isolated MikroTik CHR over its EVE serial console.

RADIUS and console credentials are read only from root-only EVE files.
"""

import os
from pathlib import Path
import re
import select
import socket
import time


PORT = 32789
ADMIN_SECRET = Path("/root/nac-mikrotik-admin.secret").read_text().strip()
RADIUS_SECRET = Path("/root/nac-pki/radius.secret").read_text().strip()


def clean(value):
    text = value.decode(errors="replace").replace("\r", "")
    text = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", text)
    text = text.replace(ADMIN_SECRET, "[REDACTED]").replace(RADIUS_SECRET, "[REDACTED]")
    return text[-6000:]


def read(sock, seconds):
    data = bytearray()
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if select.select([sock], [], [], 0.15)[0]:
            part = sock.recv(65536)
            if not part:
                break
            data.extend(part)
    return clean(data)


with socket.create_connection(("127.0.0.1", PORT), timeout=5) as sock:
    sock.sendall(b"\r")
    greeting = read(sock, 1.2)
    if "Login:" in greeting:
        sock.sendall(b"admin\r")
        read(sock, 0.6)
        sock.sendall(ADMIN_SECRET.encode() + b"\r")
        greeting = read(sock, 1.5)
    if "[admin@" not in greeting:
        raise RuntimeError("CHR console did not reach admin prompt: " + greeting[-700:])

    def command(value, delay=1.0):
        sock.sendall(value.encode() + b"\r")
        output = read(sock, delay)
        if any(marker in output for marker in ("failure:", "expected end of command", "syntax error", "input does not match")):
            raise RuntimeError(f"CHR rejected {value.replace(RADIUS_SECRET, '[REDACTED]')}: {output[-800:]}")
        return output

    action = os.environ.get("MAB_ROUTEROS_ACTION", "inventory")
    if action == "inventory":
        for value in ("/system resource print", "/interface print terse",
                      "/ip address print terse", "/interface bridge print terse",
                      "/interface dot1x server print terse"):
            print("COMMAND", value, "OUTPUT", command(value, 1.3))
    elif action == "base_config":
        commands = (
            "/system identity set name=mab-chr",
            "/ip address add address=10.70.99.1/24 interface=ether4",
            "/interface bridge add name=nac-bridge protocol-mode=rstp vlan-filtering=no",
            "/interface bridge port add bridge=nac-bridge interface=ether1 edge=yes pvid=1",
            "/interface bridge port add bridge=nac-bridge interface=ether2 edge=yes pvid=10",
            "/interface bridge port add bridge=nac-bridge interface=ether3 edge=yes pvid=20",
            "/interface bridge vlan add bridge=nac-bridge vlan-ids=10 untagged=ether2",
            "/interface bridge vlan add bridge=nac-bridge vlan-ids=20 untagged=ether3",
            "/interface bridge set nac-bridge vlan-filtering=yes",
            f"/radius add address=10.70.99.2 secret={RADIUS_SECRET} service=dot1x",
        )
        for value in commands:
            if RADIUS_SECRET in value:
                try:
                    command(value, 1.3)
                except RuntimeError:
                    raise RuntimeError("CHR rejected redacted RADIUS client command") from None
                print("APPLIED /radius add address=10.70.99.2 secret=[REDACTED] service=dot1x")
            else:
                print("APPLIED", value, command(value, 1.3)[-350:])
    elif action == "mab_only":
        value = ("/interface dot1x server add interface=ether1 auth-types=mac-auth "
                 "mac-auth-mode=mac-as-username radius-mac-format=XXXXXXXXXXXX "
                 "accounting=no retrans-timeout=3s auth-timeout=20s")
        print("APPLIED", value, command(value, 2.0))
    elif action == "dot1x_then_mab":
        value = "/interface dot1x server set [find interface=ether1] auth-types=dot1x,mac-auth retrans-timeout=5s"
        print("APPLIED", value, command(value, 2.0))
    elif action == "reset_mab":
        print("RESET", command("/interface dot1x server disable [find interface=ether1]", 1.2)[-250:])
        print("RESET", command("/interface dot1x server enable [find interface=ether1]", 1.2)[-250:])
    elif action == "mab_status":
        for value in ("/interface dot1x server print terse", "/interface dot1x server active print terse",
                      "/interface dot1x server state print terse", "/interface bridge vlan print terse",
                      "/interface bridge port print terse", "/radius monitor 0 once"):
            print("COMMAND", value, "OUTPUT", command(value, 1.3))
    elif action == "radius_debug":
        print(command("/log print where topics~\"radius\"", 1.5))
    else:
        raise SystemExit("Unknown MAB_ROUTEROS_ACTION")
