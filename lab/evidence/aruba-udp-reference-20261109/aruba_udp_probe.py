"""Send fresh UDP datagrams and require target-specific echo replies."""
from datetime import datetime, timezone
import json
import socket
import sys
import uuid

targets = {"corp": "10.70.10.10", "remediation": "10.70.20.10"}
source = sys.argv[1] if len(sys.argv) == 2 else "endpoint"
for name, host in targets.items():
    nonce = uuid.uuid4().hex[:12]
    data = f"aruba-udp-probe:{source}:{name}:{nonce}".encode()
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.settimeout(1.2)
            sock.sendto(data, (host, 9099))
            reply, sender = sock.recvfrom(256)
        outcome = "echo_ok" if reply == name.encode() + b":" + data and sender[0] == host else "wrong_reply"
    except OSError as exc:
        outcome = type(exc).__name__
    print(json.dumps({"utc": datetime.now(timezone.utc).isoformat(),
                      "source": source, "target": name, "outcome": outcome}), flush=True)
