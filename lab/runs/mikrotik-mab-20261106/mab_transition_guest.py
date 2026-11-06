"""Switch between preserved Arista and separate MikroTik EVE labs.

Run inside EVE. Only EVE node state and the isolated host control address move.
Proxmox and unrelated EVE labs are not modified.
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
    "baseline": ("/labs/NAC-Wired-Qualification.unl", set(range(1, 6)),
                 "659656f9030ce694ac01347db943f67a87bc56f451ef3789d5d3f94e2200fad9"),
    "arista": ("/labs/NAC-Pilot-Candidate.unl", set(range(11, 16)),
               "e3407fe0f06ec49769fac5dc3578aa436eaab91d700865cbb5e70838a5a7af8a"),
    "mikrotik": ("/labs/NAC-MAB-MikroTik.unl", set(range(21, 26)),
                 "527f9a4ab2070facd443c3d16e6d3af042769c08282b079d9d8125d6e84bef3a"),
}
CONTROL_ADDR = "10.70.99.254/24"
ARISTA_BRIDGE = "vnet0_14"
MAB_BRIDGE = "vnet0_24"
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def api(path, method="GET", data=None):
    request = urllib.request.Request(BASE + path, method=method,
        data=None if data is None else json.dumps(data).encode(),
        headers={"Content-Type": "application/json"})
    with opener.open(request, timeout=30) as response:
        answer = json.load(response)
    if answer.get("status") != "success":
        raise RuntimeError((path, answer))
    return answer.get("data", answer)


def nodes(name):
    path, expected, _ = LABS[name]
    items = api(path + "/nodes")
    items = items.values() if isinstance(items, dict) else items
    result = {int(item["id"]): int(item["status"]) for item in items}
    if set(result) != expected:
        raise RuntimeError(f"Unexpected {name} node IDs: {result}")
    return result


def address(bridge):
    if not Path("/sys/class/net", bridge).exists():
        return []
    values = json.loads(subprocess.check_output(["ip", "-j", "addr", "show", "dev", bridge]))
    return sorted(item["local"] + "/" + str(item["prefixlen"])
                  for entry in values for item in entry.get("addr_info", []) if item["family"] == "inet")


def inspect():
    if subprocess.check_output(["sysctl", "-n", "net.ipv4.ip_forward"], text=True).strip() != "0":
        raise RuntimeError("EVE IP forwarding is not disabled")
    hashes = {}
    for name, (path, _, expected) in LABS.items():
        actual = hashlib.sha256((Path("/opt/unetlab") / path.lstrip("/")).read_bytes()).hexdigest()
        if actual != expected:
            raise RuntimeError(f"{name} topology hash changed: {actual}")
        hashes[name] = actual
    state = {name: nodes(name) for name in LABS}
    bridges = {bridge: address(bridge) for bridge in (ARISTA_BRIDGE, MAB_BRIDGE)}
    print(json.dumps({"nodes": state, "bridges": bridges, "topology_sha256": hashes,
                      "ip_forward": 0}, sort_keys=True), flush=True)
    return state, bridges


def change(name, node_id, verb):
    path = LABS[name][0] + f"/nodes/{node_id}"
    wanted = 2 if verb == "start" else 0
    if int(api(path)["status"]) == wanted:
        return
    api(path + "/" + verb)
    for _ in range(50):
        if int(api(path)["status"]) == wanted:
            return
        time.sleep(1)
    raise RuntimeError(f"{name} node {node_id} did not {verb}")


def move(source, target):
    if address(source) != [CONTROL_ADDR] or address(target):
        raise RuntimeError(f"Unexpected control bridge addresses: {source} {address(source)}, {target} {address(target)}")
    subprocess.run(["ip", "addr", "del", CONTROL_ADDR, "dev", source], check=True)
    try:
        subprocess.run(["ip", "addr", "add", CONTROL_ADDR, "dev", target], check=True)
    except Exception:
        subprocess.run(["ip", "addr", "add", CONTROL_ADDR, "dev", source], check=True)
        raise
    if address(source) or address(target) != [CONTROL_ADDR]:
        raise RuntimeError("Control address move did not verify")


def start_mikrotik():
    state, bridges = inspect()
    if any(state["baseline"].values()) or any(state["arista"].values()) or any(state["mikrotik"].values()):
        raise RuntimeError("Only stopped baseline, Arista and MikroTik labs can transition")
    if bridges[ARISTA_BRIDGE] != [CONTROL_ADDR] or bridges[MAB_BRIDGE]:
        raise RuntimeError("Control address is not solely on stopped Arista bridge")
    change("mikrotik", 25, "start")
    for _ in range(20):
        if Path("/sys/class/net", MAB_BRIDGE).exists():
            break
        time.sleep(1)
    move(ARISTA_BRIDGE, MAB_BRIDGE)
    for node_id in (23, 24, 21, 22):
        change("mikrotik", node_id, "start")
    inspect()


def stop_mikrotik():
    state, bridges = inspect()
    if any(state["baseline"].values()) or any(state["arista"].values()) or set(state["mikrotik"].values()) != {2}:
        raise RuntimeError("Expected only the five MikroTik lab nodes running")
    if bridges[MAB_BRIDGE] != [CONTROL_ADDR] or bridges[ARISTA_BRIDGE]:
        raise RuntimeError("Control address is not solely on MikroTik bridge")
    for node_id in (22, 21, 24, 23):
        change("mikrotik", node_id, "stop")
    move(MAB_BRIDGE, ARISTA_BRIDGE)
    change("mikrotik", 25, "stop")
    inspect()


def restore_arista():
    state, bridges = inspect()
    if any(value for group in state.values() for value in group.values()):
        raise RuntimeError("All lab nodes must be stopped before restoring Arista")
    if bridges[ARISTA_BRIDGE] != [CONTROL_ADDR] or bridges[MAB_BRIDGE]:
        raise RuntimeError("Control address is not ready for Arista lab")
    for node_id in (15, 13, 14, 11, 12):
        change("arista", node_id, "start")
    inspect()


def main():
    action = os.environ.get("MAB_TRANSITION_ACTION", "inspect")
    if action not in {"inspect", "start_mikrotik", "stop_mikrotik", "restore_arista"}:
        raise SystemExit("Unknown MAB_TRANSITION_ACTION")
    api("/auth/login", "POST", {"username": "admin", "password": Path("/root/eve-nac-lab.webpw").read_text().strip()})
    {"inspect": inspect, "start_mikrotik": start_mikrotik,
     "stop_mikrotik": stop_mikrotik, "restore_arista": restore_arista}[action]()


if __name__ == "__main__":
    main()
