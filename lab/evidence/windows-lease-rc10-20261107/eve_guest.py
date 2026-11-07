"""Run a bounded command on the new EVE guest without exposing its password.

The password is generated and held in the Proxmox root account. Guest SSH host
keys are pinned locally after first connection to the console-observed IP.
"""

import argparse
from pathlib import Path
import sys

import paramiko


ROOT = Path(__file__).resolve().parent
HOST_KEYS = ROOT / "eve_known_hosts"
GUEST_IP = "192.168.1.125"


def connect_pve():
    client = paramiko.SSHClient()
    client.load_host_keys("D:/AI/xirp-web-lab/proxmox_known_hosts")
    client.connect(
        "192.168.1.84",
        username="root",
        key_filename="C:/Users/Orel/.ssh/devops-training_ed25519",
        look_for_keys=False,
        allow_agent=False,
        timeout=10,
    )
    return client


def main():
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    parser = argparse.ArgumentParser()
    parser.add_argument("command", nargs="?")
    parser.add_argument("--script", type=Path)
    parser.add_argument("--download", nargs=2, metavar=("GUEST_PATH", "LOCAL_PATH"))
    parser.add_argument("--upload", nargs=2, metavar=("LOCAL_PATH", "GUEST_PATH"))
    args = parser.parse_args()
    if not args.command and not args.script and not args.download and not args.upload:
        parser.error("provide a command or operation")

    pve = connect_pve()
    try:
        _stdin, stdout, _stderr = pve.exec_command("cat /root/eve-nac-lab.rootpw", timeout=10)
        password = stdout.read().decode().strip()
        if stdout.channel.recv_exit_status() or not password:
            raise RuntimeError("lab guest password unavailable on Proxmox")
    finally:
        pve.close()

    guest = paramiko.SSHClient()
    if HOST_KEYS.exists():
        guest.load_host_keys(str(HOST_KEYS))
        guest.set_missing_host_key_policy(paramiko.RejectPolicy())
    else:
        guest.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    guest.connect(
        GUEST_IP,
        username="root",
        password=password,
        look_for_keys=False,
        allow_agent=False,
        timeout=10,
        auth_timeout=10,
    )
    password = None
    try:
        if not HOST_KEYS.exists():
            guest.save_host_keys(str(HOST_KEYS))
        if args.download:
            with guest.open_sftp() as sftp:
                sftp.get(*args.download)
            print("downloaded", args.download[1])
            return
        if args.upload:
            source, destination = args.upload
            with guest.open_sftp() as sftp:
                try:
                    sftp.stat(destination)
                except FileNotFoundError:
                    pass
                else:
                    raise FileExistsError("guest destination already exists: " + destination)
                sftp.put(source, destination)
            print("uploaded", Path(source).name, "to guest image directory")
            return
        _stdin, stdout, stderr = guest.exec_command(args.command or "python3 -", timeout=120)
        if args.script:
            _stdin.write(args.script.read_text(encoding="utf-8"))
            _stdin.channel.shutdown_write()
        result = stdout.read().decode(errors="replace")
        error = stderr.read().decode(errors="replace")
        status = stdout.channel.recv_exit_status()
        print(result, end="")
        if error:
            print(error, end="")
        if status:
            raise SystemExit(status)
    finally:
        guest.close()


if __name__ == "__main__":
    main()
