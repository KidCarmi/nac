"""Change only the existing lab policy segment through the NAC API."""
import json
import os
from pathlib import Path
import urllib.request

target = os.environ["TARGET_SEGMENT"]
assert target in ("corp", "quarantine")
env = dict(line.split("=", 1) for line in Path("/root/nac-lab.env").read_text().splitlines() if line and not line.startswith("#"))
base = "http://10.70.99.254:18080"

def request(path, method="GET", data=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(base + path, data=None if data is None else json.dumps(data).encode(), headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=10) as response:
        return json.load(response)

jwt = request("/api/v1/auth/login", "POST", {"email": env["NAC_BOOTSTRAP_ADMIN_EMAIL"], "password": env["NAC_BOOTSTRAP_ADMIN_PASSWORD"]})["token"]
policy = next(item for item in request("/api/v1/policies", token=jwt)["items"] if item["name"] == "lab-wired-endpoint")
before = {key: policy[key] for key in ("name", "description", "priority", "enabled", "conditions", "action", "segment")}
after = dict(before, segment=target)
assert before["action"] == "allow"
if before["segment"] != target:
    updated = request("/api/v1/policies/" + policy["id"], "PUT", after, jwt)
else:
    updated = policy
print(json.dumps({"policy_id": policy["id"], "before": before, "after": {key: updated[key] for key in before}}, sort_keys=True))
