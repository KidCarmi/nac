"""Lab-scoped candidate runner. Product changes are made in the documented UI.

Each invocation appends a timestamped, sanitized action to one immutable run
directory. The endpoint has one data NIC; this runner never enables routing.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time


HERE = Path(__file__).resolve().parent
LAB = HERE.parent
EVE = Path(os.environ.get("EVE_GUEST_SCRIPT", str(LAB / "eve_guest.py")))
SCRIPTS = {
    "transition": "candidate_transition_guest.py",
    "control": "candidate_serial_guest.py",
    "switch": "candidate_switch_guest.py",
    "endpoint": "candidate_endpoint_guest.py",
    "health": "candidate_service_health_guest.py",
    "capture": "candidate_capture_guest.py",
    "continuous": "probe_control_guest.py",
}
TOPOLOGY_SHA = "e3407fe0f06ec49769fac5dc3578aa436eaab91d700865cbb5e70838a5a7af8a"


def utc():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def guest(script=None, env=None, download=None, timeout=120):
    if download:
        args = [sys.executable, str(EVE), "--download", *download]
    else:
        prefix = " ".join(f"{key}={value}" for key, value in (env or {}).items())
        args = [sys.executable, str(EVE), prefix + " python3 -", "--script", str(HERE / SCRIPTS[script])]
    result = subprocess.run(args, text=True, capture_output=True, timeout=timeout)
    output = (result.stdout + result.stderr).strip()
    if result.returncode:
        raise RuntimeError(f"guest action failed ({result.returncode}): {output[-1600:]}")
    return output


def action(run, name, script, env=None, timeout=120):
    started = utc()
    output = guest(script, env, timeout=timeout)
    record = {"at_utc": started, "action": name, "output": output}
    with (run / "actions.jsonl").open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, sort_keys=True) + "\n")
    print(output)
    return output


def endpoint_probe(run, wanted, deadline):
    target = {
        "none": ("BLOCKED", "BLOCKED"),
        "corp": ("OPEN", "BLOCKED"),
        "quarantine": ("BLOCKED", "OPEN"),
    }[wanted]
    start = time.monotonic()
    while True:
        output = action(run, "probe-" + wanted, "endpoint", {"ENDPOINT_ACTION": "probe"})
        lines = {line.strip() for line in output.splitlines()}
        observed = tuple("OPEN" if f"{name}_TCP OPEN" in lines else "BLOCKED" if f"{name}_TCP BLOCKED" in lines else "UNKNOWN" for name in ("CORP", "QUARANTINE"))
        if observed == target:
            print(f"PASS expected={wanted} observed={observed} elapsed_s={time.monotonic()-start:.1f}")
            return
        if time.monotonic() - start >= deadline:
            raise RuntimeError(f"FAIL expected={target} observed={observed} after {deadline}s")
        time.sleep(2)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_id", help="candidate-SHORTSHA-YYYYMMDD")
    parser.add_argument("phase", choices=("init", "preflight", "service-health", "capture-start", "capture-stop", "fresh", "valid-start", "invalid-start", "valid-restart", "reauth", "probe", "switch-status", "outage-stop", "outage-recover", "restart-stop", "restart-start", "continuous-install", "continuous-start", "continuous-release", "continuous-stop", "continuous-fetch", "collect"))
    parser.add_argument("--commit", help="full NAC candidate commit, required by init")
    parser.add_argument("--nacd-sha256", help="expected reproducible binary hash, required by init")
    parser.add_argument("--expect", choices=("none", "corp", "quarantine"))
    parser.add_argument("--deadline", type=int, default=90)
    parser.add_argument("--probe-id", default="")
    parser.add_argument("--seconds", type=int, default=180)
    parser.add_argument("--hold-corp", action="store_true", help="hold a pre-CoA corp TCP connection until continuous-release")
    args = parser.parse_args()
    if not re.fullmatch(r"candidate-[0-9a-f]{7}-[0-9]{8}", args.run_id):
        parser.error("run_id must be candidate-SHORTSHA-YYYYMMDD")
    run = HERE / "runs" / args.run_id
    if args.phase == "init":
        if not args.commit or not re.fullmatch(r"[0-9a-f]{40}", args.commit) or not args.nacd_sha256 or not re.fullmatch(r"[0-9a-f]{64}", args.nacd_sha256):
            parser.error("init requires full --commit and --nacd-sha256")
        run.mkdir(parents=True, exist_ok=False)
        metadata = {"run_id": args.run_id, "candidate_commit": args.commit,
                    "expected_nacd_sha256": args.nacd_sha256, "started_at_utc": utc(),
                    "topology_sha256": digest(HERE / "NAC-Pilot-Candidate.unl"),
                    "runner_sha256": digest(__file__), "endpoint_management_nic": False}
        if metadata["topology_sha256"] != TOPOLOGY_SHA:
            raise RuntimeError("topology hash mismatch")
        (run / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
        print(run)
        return
    if not (run / "metadata.json").exists():
        parser.error("run not initialized")
    metadata = json.loads((run / "metadata.json").read_text())
    phase = args.phase
    if phase == "preflight":
        output = action(run, phase, "transition", {"TRANSITION_ACTION": "preflight"})
        state = json.loads(output)
        if (state["topology_sha256"]["candidate"] != TOPOLOGY_SHA
                or state["ip_forward"] != 0
                or any(state["nodes"]["baseline"].values())
                or set(state["nodes"]["candidate"].values()) != {2}
                or state["bridge_addresses"]["vnet0_14"] != ["10.70.99.254/24"]):
            raise RuntimeError("lab isolation preflight failed")
        installed = action(run, "installed-version", "control", {"CONTROL_ACTION": "verify"})
        if metadata["candidate_commit"] not in installed or metadata["expected_nacd_sha256"] not in installed:
            raise RuntimeError("installed candidate/hash differs from run metadata")
    elif phase == "service-health":
        health = json.loads(action(run, phase, "health"))
        if (health.get("ip_forward") != "0"
                or any(health.get("candidate_bridge_addresses_after", {}).values())
                or any(item.get("http_status") != 200 for item in health.get("services", {}).values())
                or set(health.get("services", {})) != {"corp", "quarantine"}):
            raise RuntimeError("service health or isolation preflight failed")
    elif phase in ("capture-start", "capture-stop"):
        action(run, phase, "capture", {"CANDIDATE_RUN_ID": args.run_id, "CAPTURE_ACTION": phase.split("-")[1]})
    elif phase == "fresh":
        switch = action(run, "switch-readiness-before-fresh", "switch", {"SWITCH_ACTION": "status"})
        if not all(term in switch for term in ("interface Ethernet1", "dot1x port-control auto", "10    corp", "20    quarantine", "show dot1x hosts")):
            raise RuntimeError("vEOS is not ready with the saved 802.1X/VLAN configuration")
        action(run, "stop-supplicant-before-fresh", "endpoint", {"ENDPOINT_ACTION": "stop"})
        action(run, "clear-before-fresh", "switch", {"SWITCH_ACTION": "clear_session"})
        action(run, phase, "endpoint", {"ENDPOINT_ACTION": "fresh"})
    elif phase in ("valid-start", "invalid-start", "valid-restart", "reauth"):
        action(run, phase, "endpoint", {"ENDPOINT_ACTION": phase.replace("-", "_")})
    elif phase == "probe":
        if not args.expect:
            parser.error("probe requires --expect")
        endpoint_probe(run, args.expect, args.deadline)
    elif phase == "switch-status":
        action(run, phase, "switch", {"SWITCH_ACTION": "status"})
    elif phase == "outage-stop":
        action(run, phase, "control", {"CONTROL_ACTION": "stop_nacd"})
    elif phase == "outage-recover":
        try:
            action(run, phase, "control", {"CONTROL_ACTION": "start_nacd"})
        except RuntimeError as exc:
            if "NACD_STATE=active" not in str(exc):
                raise
        for attempt in range(8):
            try:
                action(run, "outage-ready", "control", {"CONTROL_ACTION": "verify"})
                break
            except RuntimeError:
                if attempt == 7:
                    raise
                time.sleep(2)
    elif phase in ("restart-stop", "restart-start"):
        action(run, phase, "transition", {"TRANSITION_ACTION": "stop_candidate" if phase.endswith("stop") else "restart_candidate"})
    elif phase.startswith("continuous-"):
        step = phase.split("-")[1]
        if step != "install" and not re.fullmatch(r"[A-Za-z0-9_-]{1,64}", args.probe_id):
            parser.error("continuous action requires --probe-id")
        env = {"PROBE_ACTION": step, "PROBE_CONSOLE_PORT": "32780"}
        if step != "install":
            env.update(PROBE_RUN_ID=args.probe_id, PROBE_SECONDS=str(args.seconds))
        if step == "start" and args.hold_corp:
            env["PROBE_MODE"] = "hold"
        action(run, phase, "continuous", env)
    elif phase == "collect":
        for name in ("endpoint-eapol.pcap", "radius.pcap"):
            target = run / name
            if target.exists():
                raise RuntimeError(f"refusing to overwrite {target}")
            guest(download=(f"/root/nac-qualification/{args.run_id}/{name}", str(target)))
            print(name, digest(target))
        if args.probe_id:
            target = run / ("probe-" + args.probe_id + ".jsonl")
            guest(download=(f"/root/nac-qualification/probe-{args.probe_id}.jsonl", str(target)))
            print(target.name, digest(target))


if __name__ == "__main__":
    main()
