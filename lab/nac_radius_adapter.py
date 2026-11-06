"""FreeRADIUS post-auth adapter for the disposable wired NAC lab.

Only FreeRADIUS invokes this after successful EAP-TLS. Its TLS certificate
attributes, not outer User-Name or a claimed MAC, select the enrolled endpoint.
"""
import json
import os
from pathlib import Path
import re
import sys
import time
import urllib.request

ROOT = Path("/etc/freeradius/3.0/nac-lab")
MAP = ROOT / "identity.json"
TOKEN = ROOT / "service-token"
LOG = Path("/var/log/freeradius/nac-adapter.jsonl")
URL = "http://10.70.99.254:18080/api/v1/access/evaluate"
VLANS = {"corp": "10", "quarantine": "20"}

def record(**fields):
    fields["time"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    try:
        with LOG.open("a") as stream:
            stream.write(json.dumps(fields, sort_keys=True) + "\n")
    except OSError:
        pass

def fail(reason, **fields):
    record(result="deny", reason=reason, **fields)
    print("nac-lab: " + reason, file=sys.stderr)
    raise SystemExit(1)

def serial(value):
    clean = re.fullmatch(r"[0-9a-fA-F]+", value or "")
    return clean.group(0).upper().lstrip("0") if clean else ""

def mac(value):
    chars = re.sub(r"[^0-9a-fA-F]", "", value or "")
    if len(chars) != 12:
        return ""
    return ":".join(chars[i:i+2] for i in range(0, 12, 2)).lower()

binding = json.loads(MAP.read_text())
observed = {
    "serial": os.environ.get("TLS_CLIENT_CERT_SERIAL", "").strip().strip('"'),
    "issuer": os.environ.get("TLS_CLIENT_CERT_ISSUER", "").strip().strip('"'),
    "cn": os.environ.get("TLS_CLIENT_CERT_COMMON_NAME", "").strip().strip('"'),
    "calling_station": os.environ.get("CALLING_STATION_ID", "").strip().strip('"'),
}
if not observed["serial"]:
    record(result="debug", env_keys=sorted(k for k in os.environ if k.startswith("TLS_") or k in ("CALLING_STATION_ID", "USER_NAME", "EAP_TYPE")))
    fail("missing validated certificate serial")
if serial(observed["serial"]) != serial(binding["serial"]):
    fail("certificate serial not enrolled", serial=observed["serial"])
if binding["issuer_cn"] not in observed["issuer"]:
    fail("certificate issuer not enrolled", issuer=observed["issuer"])
if observed["cn"] != binding["cn"]:
    fail("certificate common name mismatch", cn=observed["cn"])
if mac(observed["calling_station"]) != binding["mac"]:
    fail("switch MAC does not match enrolled endpoint", calling_station=observed["calling_station"])

payload = json.dumps({"mac": binding["mac"]}).encode()
request = urllib.request.Request(URL, data=payload, headers={
    "Content-Type": "application/json",
    "Authorization": "Bearer " + TOKEN.read_text().strip(),
})
try:
    with urllib.request.urlopen(request, timeout=3) as response:
        decision = json.load(response)
except Exception as error:
    fail("NAC decision service unavailable", error=type(error).__name__)

if decision.get("endpoint") != binding["mac"] or not decision.get("matched_policy_id"):
    fail("NAC returned an unbound decision")
segment = decision.get("segment", "")
if decision.get("action") not in ("allow", "quarantine") or segment not in VLANS:
    fail("NAC denied or returned an unmapped segment", action=decision.get("action"), segment=segment)
record(result="allow", cert_serial=observed["serial"], endpoint=binding["mac"], matched_policy_id=decision["matched_policy_id"], segment=segment, vlan=VLANS[segment])
print("Tunnel-Type := VLAN")
print("Tunnel-Medium-Type := IEEE-802")
print('Tunnel-Private-Group-Id := "' + VLANS[segment] + '"')
