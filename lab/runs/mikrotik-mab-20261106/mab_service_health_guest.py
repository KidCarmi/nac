"""Verify both isolated MAB targets from their own EVE bridges, then clean up."""

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


if command("sysctl", "-n", "net.ipv4.ip_forward") != "0":
    raise SystemExit("EVE IP forwarding is enabled")
if address("vnet0_22") or address("vnet0_23") or address("vnet0_13") or address("vnet0_3") != {"10.70.20.254/24"}:
    raise SystemExit("MAB service bridges already have host addresses")

results = {}
try:
    # The stopped baseline retains this host-only source. Remove it temporarily
    # so the quarantine HTTP probe cannot route to the wrong EVE bridge.
    subprocess.run(["ip", "addr", "del", "10.70.20.254/24", "dev", "vnet0_3"], check=True)
    for name, bridge, local, target in (
        ("corp", "vnet0_22", "10.70.10.254/24", "10.70.10.10"),
        ("quarantine", "vnet0_23", "10.70.20.254/24", "10.70.20.10"),
    ):
        subprocess.run(["ip", "addr", "add", local, "dev", bridge], check=True)
        try:
            deadline = time.monotonic() + 40
            while time.monotonic() < deadline:
                try:
                    with urllib.request.urlopen(f"http://{target}:8080/", timeout=2) as response:
                        results[name] = {"http_status": response.status, "source_bridge": bridge,
                                         "source_address": local, "target": target}
                    break
                except Exception:
                    time.sleep(1)
            if name not in results:
                results[name] = {"error": "HTTP service unavailable", "target": target}
        finally:
            subprocess.run(["ip", "addr", "del", local, "dev", bridge], check=True)
finally:
    if "10.70.20.254/24" not in address("vnet0_3"):
        subprocess.run(["ip", "addr", "add", "10.70.20.254/24", "dev", "vnet0_3"], check=True)
    if address("vnet0_22") or address("vnet0_23"):
        raise SystemExit("Temporary EVE bridge address was not removed")
print(json.dumps({"results": results, "ip_forward": 0}, sort_keys=True))
if set(results) != {"corp", "quarantine"} or any(x.get("http_status") != 200 for x in results.values()):
    raise SystemExit("MAB service health failed")
