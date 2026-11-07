"""Repeatable EVE-side evidence runner for the isolated DHCP NAC topology.

Product policy and CoA actions are performed in the documented NAC console.
This runner controls only lab endpoint/switch probes, captures, and transfers.
It never exports test keys, shared secrets, or vendor disks.
"""
import argparse
from datetime import datetime, timezone
from pathlib import Path
import re
import subprocess
import sys

HERE = Path(__file__).resolve().parent
EVE = HERE.parent / 'eve_guest.py'
SCRIPTS = {
    'health': ('', 'dhcp_service_health_guest.py'),
    'topology': ('DHCP_TRANSITION_ACTION=inspect', 'dhcp_transition_guest.py'),
    'endpoint-status': ('DHCP_ENDPOINT_ACTION=inventory', 'dhcp_endpoint_guest.py'),
    'endpoint-probe': ('DHCP_ENDPOINT_ACTION=probe', 'dhcp_endpoint_guest.py'),
    'endpoint-valid': ('DHCP_ENDPOINT_ACTION=valid_start', 'dhcp_endpoint_guest.py'),
    'endpoint-invalid': ('DHCP_ENDPOINT_ACTION=invalid_start', 'dhcp_endpoint_guest.py'),
    'endpoint-stop': ('DHCP_ENDPOINT_ACTION=stop', 'dhcp_endpoint_guest.py'),
    'endpoint-renew-diagnostic': ('DHCP_ENDPOINT_ACTION=manual_renew', 'dhcp_endpoint_guest.py'),
    'endpoint-dhcp-logs': ('DHCP_ENDPOINT_ACTION=dhcp_logs', 'dhcp_endpoint_guest.py'),
    'switch-status': ('SWITCH_ACTION=status', 'dhcp_switch_guest.py'),
    'switch-clear': ('SWITCH_ACTION=clear_session', 'dhcp_switch_guest.py'),
    'control-status': ('', 'dhcp_control_status_guest.py'),
}

def invoke(args, label, command=None, script=None, download=None):
    argv = [sys.executable, str(EVE)]
    if command:
        argv.append(command + ' python3 -')
    if script:
        argv += ['--script', str(HERE / script)]
    if download:
        argv += ['--download', download[0], str(download[1])]
    result = subprocess.run(argv, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, errors='replace')
    entry = f'[{datetime.now(timezone.utc).isoformat()}] {label}\n{result.stdout}\n'
    with (args.out / 'runner.log').open('a', encoding='utf-8') as log:
        log.write(entry)
    print(result.stdout, end='')
    if result.returncode:
        raise SystemExit(result.returncode)

def main():
    # Windows operator consoles may use cp1255; guest UTF-8 output can include
    # replacement characters when serial captures contain partial sequences.
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=sorted(set(SCRIPTS) | {
        'capture-start','capture-stop','capture-download','probe-start',
        'probe-fetch','probe-stop','probe-release'}))
    parser.add_argument('--candidate', required=True, help='full 40-character NAC commit')
    parser.add_argument('--run', required=True, help='unique run ID for output and probe')
    parser.add_argument('--seconds', type=int, default=600)
    parser.add_argument('--hold-corp', action='store_true')
    args = parser.parse_args()
    if not re.fullmatch(r'[0-9a-f]{40}', args.candidate):
        parser.error('candidate must be a full commit SHA')
    if not re.fullmatch(r'[A-Za-z0-9_-]{1,64}', args.run):
        parser.error('run must be alphanumeric with dash or underscore')
    if not 1 <= args.seconds <= 900:
        parser.error('seconds must be 1..900')
    args.out = HERE / 'runs' / f'candidate-{args.candidate[:7]}-dhcp-{args.run}'
    args.out.mkdir(parents=True, exist_ok=True)
    root = f'/root/nac-qualification/candidate-{args.candidate[:7]}-dhcp-{args.run}'
    if args.action in SCRIPTS:
        env, script = SCRIPTS[args.action]
        invoke(args, args.action, env, script)
    elif args.action.startswith('capture-'):
        if args.action == 'capture-download':
            listing = subprocess.run([sys.executable, str(EVE),
                f"find {root} -maxdepth 1 -type f -name '*.pcap*' -printf '%f\\n'"],
                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                errors='replace')
            if listing.returncode:
                raise SystemExit(listing.stdout)
            filenames = sorted(name for name in listing.stdout.splitlines()
                if re.fullmatch(r'(endpoint|radius|corp-dhcp|quarantine-dhcp)\.pcap[01]', name))
            if not filenames:
                raise SystemExit('no bounded capture files found')
            for filename in filenames:
                invoke(args, f'download {filename}',
                    download=(f'{root}/{filename}', args.out / filename))
        else:
            verb = args.action.removeprefix('capture-')
            invoke(args, args.action,
                   f'DHCP_CAPTURE_ACTION={verb} DHCP_CAPTURE_ROOT={root}',
                   'dhcp_capture_guest.py')
    else:
        verb = args.action.removeprefix('probe-')
        if verb == 'fetch':
            invoke(args, args.action,
                   f'PROBE_ACTION=fetch PROBE_RUN_ID={args.run}',
                   'dhcp_probe_control_guest.py')
            invoke(args, 'download probe', download=(
                f'/root/nac-qualification/dhcp-probe-{args.run}.jsonl',
                args.out / 'endpoint-probe.jsonl'))
        else:
            env = f'PROBE_ACTION={verb} PROBE_RUN_ID={args.run}'
            if verb == 'start':
                env += f' PROBE_SECONDS={args.seconds} PROBE_MODE={"hold" if args.hold_corp else "fresh"}'
            invoke(args, args.action, env, 'dhcp_probe_control_guest.py')

if __name__ == '__main__':
    main()
