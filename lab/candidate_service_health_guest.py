"""Prove each candidate TCP service from its own EVE bridge, then clean up."""

import json
import subprocess
import time
import urllib.request


def command(*args):
    return subprocess.check_output(args, text=True).strip()


def address(bridge):
    values = json.loads(command("ip", "-j", "addr", "show", "dev", bridge))
    return {item["local"] + "/" + str(item["prefixlen"])
            for entry in values for item in entry.get("addr_info", []) if item["family"] == "inet"}


def change(action, value, bridge):
    subprocess.run(("ip", "addr", action, value, "dev", bridge), check=True)


if command("sysctl", "-n", "net.ipv4.ip_forward") != "0":
    raise SystemExit("EVE IP forwarding is enabled")
if address("vnet0_2") or address("vnet0_12") or address("vnet0_13") or address("vnet0_3") != {"10.70.20.254/24"}:
    raise SystemExit("unexpected service bridge addresses; refusing health check")

results = {}
try:
    change("del", "10.70.20.254/24", "vnet0_3")
    change("add", "10.70.10.254/24", "vnet0_12")
    change("add", "10.70.20.254/24", "vnet0_13")
    for name, target, bridge in (("corp", "10.70.10.10", "vnet0_12"),
                                 ("quarantine", "10.70.20.10", "vnet0_13")):
        deadline = time.monotonic() + 80
        last = ""
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(f"http://{target}:8080/", timeout=3) as response:
                    results[name] = {"http_status": response.status, "target": target,
                                     "source_bridge": bridge, "source_address": next(iter(address(bridge))),
                                     "body_prefix": response.read(80).decode(errors="replace")}
                break
            except Exception as exc:
                last = type(exc).__name__ + ": " + str(exc)
                time.sleep(2)
        if name not in results:
            results[name] = {"error": last, "target": target, "source_bridge": bridge}
finally:
    if "10.70.10.254/24" in address("vnet0_12"):
        change("del", "10.70.10.254/24", "vnet0_12")
    if "10.70.20.254/24" in address("vnet0_13"):
        change("del", "10.70.20.254/24", "vnet0_13")
    if "10.70.20.254/24" not in address("vnet0_3"):
        change("add", "10.70.20.254/24", "vnet0_3")

print(json.dumps({"services": results,
                  "candidate_bridge_addresses_after": {"corp": sorted(address("vnet0_12")),
                                                       "quarantine": sorted(address("vnet0_13"))},
                  "ip_forward": command("sysctl", "-n", "net.ipv4.ip_forward")}, sort_keys=True))
if set(results) != {"corp", "quarantine"} or any(item.get("http_status") != 200 for item in results.values()):
    raise SystemExit("candidate service health did not pass")
