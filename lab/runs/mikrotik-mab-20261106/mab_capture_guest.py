"""Capture switch RADIUS and endpoint EAPOL in the isolated MAB topology."""

import os
from pathlib import Path
import signal
import subprocess
import time


ROOT = Path("/root/nac-qualification/mikrotik-mab-20261106")
ACTION = os.environ.get("MAB_CAPTURE_ACTION", "")
CAPTURES = {
    "radius": ("vnet0_24", "udp and (port 1812 or port 1813 or port 3799)"),
    "eapol": ("vnet0_21", "ether proto 0x888e"),
}

if ACTION == "start":
    ROOT.mkdir(mode=0o700, parents=True, exist_ok=False)
    for name, (interface, selector) in CAPTURES.items():
        output = ROOT / f"{name}.pcap"
        with (ROOT / f"{name}.stderr").open("wb") as errors:
            process = subprocess.Popen(["tcpdump", "-U", "-n", "-i", interface,
                                        "-w", str(output), selector],
                                       stdout=subprocess.DEVNULL, stderr=errors,
                                       start_new_session=True)
        (ROOT / f"{name}.pid").write_text(str(process.pid))
    time.sleep(1)
    for name in CAPTURES:
        pid = int((ROOT / f"{name}.pid").read_text())
        os.kill(pid, 0)
    print("CAPTURE_STARTED", ROOT)
elif ACTION == "stop":
    for name in CAPTURES:
        pid = int((ROOT / f"{name}.pid").read_text())
        os.kill(pid, signal.SIGINT)
    time.sleep(1)
    for name in CAPTURES:
        output = ROOT / f"{name}.pcap"
        print(name, output.stat().st_size, "bytes")
else:
    raise SystemExit("Set MAB_CAPTURE_ACTION=start|stop")
