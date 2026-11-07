"""Expose the isolated candidate console on workstation loopback via EVE SSH.

The operator browser uses https://127.0.0.1:18443/. The endpoint has no
management NIC and EVE IP forwarding stays disabled. Stop with Ctrl+C.
"""

import argparse
from pathlib import Path
import select
import socket
import sys
import threading

import paramiko

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
from eve_guest import GUEST_IP, HOST_KEYS, connect_pve  # noqa: E402


def guest_connection():
    pve = connect_pve()
    try:
        _, out, _ = pve.exec_command("cat /root/eve-nac-lab.rootpw", timeout=10)
        password = out.read().decode().strip()
        if out.channel.recv_exit_status() or not password:
            raise RuntimeError("EVE guest credential unavailable on Proxmox")
    finally:
        pve.close()
    guest = paramiko.SSHClient()
    guest.load_host_keys(str(HOST_KEYS))
    guest.set_missing_host_key_policy(paramiko.RejectPolicy())
    guest.connect(GUEST_IP, username="root", password=password,
                  look_for_keys=False, allow_agent=False, timeout=10)
    password = None
    return guest


def relay(client, transport, target):
    try:
        channel = transport.open_channel("direct-tcpip", target, client.getpeername())
        if channel is None:
            return
        with channel:
            while True:
                readable, _, _ = select.select((client, channel), (), (), 30)
                if not readable:
                    continue
                for source in readable:
                    data = source.recv(65536)
                    if not data:
                        return
                    (channel if source is client else client).sendall(data)
    except (OSError, paramiko.SSHException):
        return
    finally:
        client.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--local-port", type=int, default=18443)
    parser.add_argument("--clean-rc9", action="store_true",
                        help="forward only to the retained clean rc9 guest's EVE loopback port")
    parser.add_argument("--clean-rc10", action="store_true",
                        help="forward only to the retained clean rc10 guest's EVE loopback port")
    args = parser.parse_args()
    if args.clean_rc9 and args.clean_rc10:
        parser.error("choose one clean candidate")
    if not 1024 <= args.local_port <= 65535:
        parser.error("local port must be 1024..65535")
    guest = guest_connection()
    transport = guest.get_transport()
    assert transport is not None
    try:
        with socket.socket() as listener:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(("127.0.0.1", args.local_port))
            listener.listen(16)
            print(f"CANDIDATE_CONSOLE https://127.0.0.1:{args.local_port}/", flush=True)
            while True:
                client, _ = listener.accept()
                target = (("127.0.0.1", 18449) if args.clean_rc10 else
                          ("127.0.0.1", 18447) if args.clean_rc9 else
                          ("10.70.99.2", 8443))
                threading.Thread(target=relay, args=(client, transport, target),
                                 daemon=True).start()
    except KeyboardInterrupt:
        pass
    finally:
        guest.close()


if __name__ == "__main__":
    main()
