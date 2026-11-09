"""Send one bounded role CoA to the isolated Aruba reference session."""

import subprocess
import sys
from datetime import datetime, timezone

if len(sys.argv) != 2 or sys.argv[1] not in {"corp", "quarantine"}:
    raise SystemExit("usage: aruba_coa_send_control.py corp|quarantine")
role = sys.argv[1]
attributes = "\n".join([
    'User-Name = "wired-endpoint-01"',
    'NAS-IP-Address = 10.70.99.1',
    'NAS-Port-Id = "1/1/1"',
    'Calling-Station-Id = "00-50-00-00-3E-00"',
    f'Aruba-User-Role = "{role}"',
    '',
])
print(f"COA_SENT_UTC={datetime.now(timezone.utc).isoformat()} ROLE={role}")
print(attributes, end="")
result = subprocess.run(
    ["radclient", "-S", "/root/aruba-radius-v2.secret", "-t", "2", "-r", "1",
     "10.70.99.1:3799", "coa"],
    input=attributes, text=True, capture_output=True, timeout=8,
)
print(result.stdout, end="")
print(result.stderr, end="", file=sys.stderr)
print(f"RADCLIENT_EXIT={result.returncode}")
sys.exit(result.returncode)
