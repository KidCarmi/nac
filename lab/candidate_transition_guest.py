"""Switch only the two NAC EVE topologies; run inside EVE via eve_guest.py.

TRANSITION_ACTION=preflight|activate|restore. Activate stops the preserved
baseline, moves the EVE host's control address to the isolated candidate
bridge, and starts only candidate control node 15. Restore reverses this.
No Proxmox setting or other EVE lab is touched.
"""

import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request


BASE = "http://127.0.0.1/api"
LABS = {
    "baseline": ("/labs/NAC-Wired-Qualification.unl", {1, 2, 3, 4, 5},
                 "659656f9030ce694ac01347db943f67a87bc56f451ef3789d5d3f94e2200fad9"),
    "candidate": ("/labs/NAC-Pilot-Candidate.unl", {11, 12, 13, 14, 15},
                  "e3407fe0f06ec49769fac5dc3578aa436eaab91d700865cbb5e70838a5a7af8a"),
}
BASE_BRIDGE = "vnet0_4"
CANDIDATE_BRIDGE = "vnet0_14"
CONTROL_ADDR = "10.70.99.254/24"
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def api(path, method="GET", data=None):
    req = urllib.request.Request(BASE + path, method=method,
        data=None if data is None else json.dumps(data).encode(),
        headers={"Content-Type": "application/json"})
    with opener.open(req, timeout=30) as response:
        answer = json.load(response)
    if answer.get("status") != "success":
        raise RuntimeError((path, answer))
    return answer.get("data", answer)


def run(*command):
    return subprocess.check_output(command, text=True).strip()


def nodes(name):
    path, expected, _ = LABS[name]
    values = api(path + "/nodes")
    values = values.values() if isinstance(values, dict) else values
    result = {int(item["id"]): int(item["status"]) for item in values}
    if set(result) != expected:
        raise RuntimeError(f"{name} node IDs changed: {result}")
    return result


def hash_lab(name):
    path, _, expected = LABS[name]
    actual = hashlib.sha256((Path("/opt/unetlab") / path.lstrip("/")).read_bytes()).hexdigest()
    if actual != expected:
        raise RuntimeError(f"{name} topology hash changed: {actual}")
    return actual


def addresses(bridge):
    value = json.loads(run("ip", "-j", "addr", "show", "dev", bridge))
    return {a["local"] + "/" + str(a["prefixlen"])
            for item in value for a in item.get("addr_info", []) if a["family"] == "inet"}


def wait_status(name, node_id, wanted):
    path = LABS[name][0] + f"/nodes/{node_id}"
    for _ in range(40):
        status = int(api(path)["status"])
        if status == wanted:
            return
        time.sleep(1)
    raise RuntimeError(f"{name} node {node_id} stayed at status {status}; expected {wanted}")


def change(name, node_id, action):
    path = LABS[name][0] + f"/nodes/{node_id}"
    wanted = 2 if action == "start" else 0
    if int(api(path)["status"]) == wanted:
        return
    api(path + "/" + action)
    wait_status(name, node_id, wanted)


def move_address(source, target):
    if CONTROL_ADDR not in addresses(source) or CONTROL_ADDR in addresses(target):
        raise RuntimeError(f"control address is not exclusively on {source}")
    subprocess.run(["ip", "addr", "del", CONTROL_ADDR, "dev", source], check=True)
    try:
        subprocess.run(["ip", "addr", "add", CONTROL_ADDR, "dev", target], check=True)
    except Exception:
        subprocess.run(["ip", "addr", "add", CONTROL_ADDR, "dev", source], check=True)
        raise
    if CONTROL_ADDR not in addresses(target) or CONTROL_ADDR in addresses(source):
        raise RuntimeError("control address move did not verify")


def preflight():
    if run("sysctl", "-n", "net.ipv4.ip_forward") != "0":
        raise RuntimeError("EVE IP forwarding is not disabled")
    hashes = {name: hash_lab(name) for name in LABS}
    state = {name: nodes(name) for name in LABS}
    addr = {bridge: sorted(addresses(bridge)) for bridge in (BASE_BRIDGE, CANDIDATE_BRIDGE)}
    print(json.dumps({"topology_sha256": hashes, "nodes": state, "bridge_addresses": addr,
                      "ip_forward": 0}, sort_keys=True), flush=True)
    return state, addr


def activate():
    state, addr = preflight()
    if any(v != 2 for v in state["baseline"].values()) or any(v != 0 for v in state["candidate"].values()):
        raise RuntimeError("activation requires all baseline nodes running and all candidate nodes stopped")
    if addr[BASE_BRIDGE] != [CONTROL_ADDR] or addr[CANDIDATE_BRIDGE]:
        raise RuntimeError("unexpected EVE control bridge addresses")
    for node_id in (2, 1, 3, 4, 5):
        change("baseline", node_id, "stop")
    move_address(BASE_BRIDGE, CANDIDATE_BRIDGE)
    change("candidate", 15, "start")
    preflight()


def restore():
    state, addr = preflight()
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR] or addr[BASE_BRIDGE]:
        raise RuntimeError("restore requires control address on candidate bridge only")
    for node_id in sorted(state["candidate"], reverse=True):
        change("candidate", node_id, "stop")
    move_address(CANDIDATE_BRIDGE, BASE_BRIDGE)
    for node_id in (5, 3, 4, 1, 2):
        change("baseline", node_id, "start")
    preflight()


def start_infra():
    state, addr = preflight()
    if any(v != 0 for v in state["baseline"].values()) or state["candidate"] != {11: 0, 12: 0, 13: 0, 14: 0, 15: 2}:
        raise RuntimeError("infrastructure start requires isolated control-only candidate state")
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR]:
        raise RuntimeError("candidate control address is missing")
    for node_id in (13, 14, 11):
        change("candidate", node_id, "start")
    preflight()


def start_endpoint():
    state, addr = preflight()
    if any(v != 0 for v in state["baseline"].values()) or state["candidate"] != {11: 2, 12: 0, 13: 2, 14: 2, 15: 2}:
        raise RuntimeError("endpoint start requires candidate infrastructure running and endpoint stopped")
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR]:
        raise RuntimeError("candidate control address is missing")
    change("candidate", 12, "start")
    preflight()


def stop_candidate():
    state, addr = preflight()
    if any(v != 0 for v in state["baseline"].values()) or any(v != 2 for v in state["candidate"].values()):
        raise RuntimeError("restart stop requires all candidate nodes running and baseline stopped")
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR]:
        raise RuntimeError("candidate control address is missing")
    for node_id in (12, 11, 14, 13, 15):
        change("candidate", node_id, "stop")
    preflight()


def restart_candidate():
    state, addr = preflight()
    if any(v != 0 for v in state["baseline"].values()) or any(v != 0 for v in state["candidate"].values()):
        raise RuntimeError("restart start requires all candidate nodes and baseline stopped")
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR]:
        raise RuntimeError("candidate control address is missing")
    for node_id in (15, 13, 14, 11, 12):
        change("candidate", node_id, "start")
    preflight()


def restart_switch():
    state, addr = preflight()
    if any(v != 0 for v in state["baseline"].values()) or any(v != 2 for v in state["candidate"].values()):
        raise RuntimeError("switch restart requires candidate running and baseline stopped")
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR]:
        raise RuntimeError("candidate control address is missing")
    change("candidate", 11, "stop")
    change("candidate", 11, "start")
    preflight()


def stop_control():
    state, addr = preflight()
    if any(v != 0 for v in state["baseline"].values()) or any(v != 2 for v in state["candidate"].values()):
        raise RuntimeError("control stop requires candidate running and baseline stopped")
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR]:
        raise RuntimeError("candidate control address is missing")
    change("candidate", 15, "stop")
    preflight()


def start_control():
    state, addr = preflight()
    if any(v != 0 for v in state["baseline"].values()) or state["candidate"] != {11: 2, 12: 2, 13: 2, 14: 2, 15: 0}:
        raise RuntimeError("control start requires only candidate control stopped")
    if addr[CANDIDATE_BRIDGE] != [CONTROL_ADDR]:
        raise RuntimeError("candidate control address is missing")
    change("candidate", 15, "start")
    preflight()


def main():
    action = os.environ.get("TRANSITION_ACTION", "")
    if action not in {"preflight", "activate", "start_infra", "start_endpoint", "stop_candidate", "restart_candidate", "restart_switch", "stop_control", "start_control", "restore"}:
        raise SystemExit("set TRANSITION_ACTION=preflight|activate|start_infra|start_endpoint|stop_candidate|restart_candidate|restart_switch|stop_control|start_control|restore")
    api("/auth/login", "POST", {"username": "admin", "password": Path("/root/eve-nac-lab.webpw").read_text().strip()})
    {"preflight": preflight, "activate": activate, "start_infra": start_infra,
     "start_endpoint": start_endpoint, "stop_candidate": stop_candidate,
     "restart_candidate": restart_candidate, "restart_switch": restart_switch,
     "stop_control": stop_control, "start_control": start_control,
     "restore": restore}[action]()


if __name__ == "__main__":
    main()
