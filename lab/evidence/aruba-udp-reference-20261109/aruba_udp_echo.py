"""Bounded UDP echo service for isolated Aruba role-policy qualification."""
import socket
import sys
import time

name = sys.argv[1] if len(sys.argv) == 2 else ""
if name not in {"corp", "remediation"}:
    raise SystemExit("usage: aruba_udp_echo.py corp|remediation")

with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
    sock.bind(("0.0.0.0", 9099))
    sock.settimeout(1.0)
    end = time.monotonic() + 360
    while time.monotonic() < end:
        try:
            data, peer = sock.recvfrom(256)
        except socket.timeout:
            continue
        if data.startswith(b"aruba-udp-probe:"):
            sock.sendto(name.encode() + b":" + data, peer)
