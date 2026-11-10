"""Bounded fresh TCP probes from the isolated one-NIC endpoint.

Set NAC_CORP_SERVICE and NAC_REMEDIATION_SERVICE to the lab service IPs.
Start this before timing-runner.py. It logs statuses without service addresses.
"""
from datetime import datetime, timezone
import http.client
import json
import os
from pathlib import Path
import time

CORP = os.environ['NAC_CORP_SERVICE']
REMEDIATION = os.environ['NAC_REMEDIATION_SERVICE']
OUT = Path(os.environ.get('NAC_PROBE_OUTPUT', '/run/nac-100-cycle-probes.jsonl'))


def probe(host):
    conn = http.client.HTTPConnection(host, 8080, timeout=0.5)
    try:
        conn.request('GET', '/', headers={'Connection': 'close'})
        response = conn.getresponse()
        response.read(1)
        return response.status
    except (OSError, TimeoutError):
        return 0
    finally:
        conn.close()


if OUT.exists():
    raise SystemExit('probe output exists; refusing overwrite')
deadline = time.monotonic() + 720
sample = 0
while time.monotonic() < deadline:
    now = datetime.now(timezone.utc).isoformat(timespec='milliseconds')
    with OUT.open('a') as file:
        file.write(json.dumps({'utc': now, 'sample': sample,
                               'corp_http': probe(CORP),
                               'remediation_http': probe(REMEDIATION)}) + '\n')
    sample += 1
    time.sleep(0.08)
