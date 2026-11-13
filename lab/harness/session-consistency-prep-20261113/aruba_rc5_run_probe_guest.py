"""EVE-side probe gate for future candidate runs; never writes rc4/rc5 evidence.

Run in a persistent terminal. Wait for READY, then use `arm` immediately before
the UI action. `finalize` must report VALID before any transition time is used.
"""
import argparse
from dataclasses import asdict
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import select
import socket
import sys
import time

from aruba_transition_probe_gate import ProbeGate

ROOT = Path('/mnt/nac-rc5/transition-probes')
RUN_ID = re.compile(r'[a-zA-Z0-9][a-zA-Z0-9_-]{2,63}\Z')


def utc():
    return datetime.now(timezone.utc).isoformat()


def atomic_json(path, value):
    temp = path.with_name(path.name + '.tmp')
    temp.write_text(json.dumps(value, sort_keys=True) + '\n', encoding='utf-8')
    os.replace(temp, path)


def process_alive(pid):
    try:
        if int(pid) == os.getpid():
            return True
        os.kill(int(pid), 0)
        return True
    except (OSError, ValueError):
        return False


def read_some(sock, seconds):
    chunks = bytearray()
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        if select.select([sock], [], [], .1)[0]:
            piece = sock.recv(65536)
            if not piece:
                break
            chunks.extend(piece)
    return bytes(chunks)


def run(args, directory):
    directory.mkdir(parents=True, mode=0o700, exist_ok=False)
    gate = ProbeGate(args.run_id, args.baseline, args.seconds)
    state_path = directory / 'state.json'
    marker_path = directory / 'action.json'
    last_receive = time.monotonic()
    phase = 'starting'

    def save():
        atomic_json(state_path, {'phase': phase, 'pid': os.getpid(),
                    'updated_utc': utc(), 'last_seen_mono': last_receive,
                    'gate': asdict(gate)})

    save()
    try:
        password = args.password_file.read_text().strip()
        with socket.create_connection(('127.0.0.1', args.port), timeout=5) as sock:
            sock.sendall(b'\r\n')
            greeting = read_some(sock, 1)
            if b'login:' in greeting:
                sock.sendall(b'root\r\n')
                read_some(sock, 1)
                sock.sendall(password.encode() + b'\r\n')
                read_some(sock, 2)
            elif b'Password:' in greeting:
                sock.sendall(password.encode() + b'\r\n')
                read_some(sock, 2)
            password = None
            sock.sendall(b'stty -echo\r\n')
            read_some(sock, .3)
            command = (f'python3 -u /root/aruba_transition_probe_endpoint.py '
                       f'--run-id {args.run_id} --seconds {args.seconds}\r\n')
            sock.sendall(command.encode())
            ready_deadline = time.monotonic() + 15
            final_deadline = time.monotonic() + args.seconds + 25
            pending = bytearray()
            with (directory / 'probes.jsonl').open('x', encoding='utf-8') as output:
                while time.monotonic() < final_deadline:
                    now = time.monotonic()
                    if marker_path.exists() and gate.action_mono is None:
                        marker = json.loads(marker_path.read_text(encoding='utf-8'))
                        if marker.get('run_id') != args.run_id or marker['action_mono'] < (gate.ready_mono or float('inf')):
                            raise RuntimeError('invalid_action_marker')
                        gate.action_mono = marker['action_mono']
                        phase = 'running'
                        save()
                    if gate.ready_mono is None and now > ready_deadline:
                        raise RuntimeError('baseline_not_ready_before_timeout')
                    if gate.started_mono is not None and now - last_receive > 5:
                        raise RuntimeError('probe_logger_stalled_or_exited_early')
                    if not select.select([sock], [], [], .2)[0]:
                        continue
                    piece = sock.recv(65536)
                    if not piece:
                        raise RuntimeError('serial_logger_disconnected_early')
                    pending.extend(piece.replace(b'\r', b''))
                    while b'\n' in pending:
                        raw, _, rest = pending.partition(b'\n')
                        pending = bytearray(rest)
                        if not raw.lstrip().startswith(b'{'):
                            continue
                        try:
                            row = json.loads(raw)
                        except json.JSONDecodeError:
                            raise RuntimeError('malformed_logger_json') from None
                        observed = time.monotonic()
                        gate.ingest(row, observed)
                        if gate.invalid_reason:
                            raise RuntimeError(gate.invalid_reason)
                        last_receive = observed
                        row['observer_utc'] = utc()
                        row['observer_mono'] = observed
                        output.write(json.dumps(row, sort_keys=True) + '\n')
                        output.flush()
                        if gate.ready_mono is not None and phase == 'starting':
                            phase = 'ready'
                            save()
                            print(json.dumps({'phase': 'READY', 'run_id': args.run_id,
                                  'baseline': args.baseline, 'samples': gate.samples,
                                  'utc': utc()}), flush=True)
                        else:
                            save()
                        if gate.ended_mono is not None:
                            verdict, reason = gate.verdict(logger_exited=True)
                            phase = verdict.lower()
                            save()
                            print(json.dumps({'phase': verdict, 'reason': reason,
                                  'run_id': args.run_id, 'samples': gate.samples,
                                  'post_action_samples': gate.post_action_samples}), flush=True)
                            return 0 if verdict == 'VALID' else 2
            raise RuntimeError('probe_logger_deadline_without_end')
    except Exception as exc:
        gate.invalid_reason = str(exc)
        phase = 'invalid'
        save()
        print(json.dumps({'phase': 'INVALID', 'reason': gate.invalid_reason,
                          'run_id': args.run_id}), flush=True)
        return 2


def control(args, directory):
    state_path = directory / 'state.json'
    if not state_path.is_file():
        raise SystemExit('run state missing')
    state = json.loads(state_path.read_text(encoding='utf-8'))
    gate = ProbeGate(**state['gate'])
    alive = process_alive(state['pid'])
    if args.mode == 'arm':
        if state['phase'] != 'ready':
            raise SystemExit('INVALID: logger not in READY state')
        gate.arm(time.monotonic(), logger_alive=alive,
                 last_seen_age=time.monotonic() - state['last_seen_mono'])
        marker = directory / 'action.json'
        temp_marker = directory / ('action.' + str(os.getpid()) + '.tmp')
        with temp_marker.open('x', encoding='utf-8') as out:
            out.write(json.dumps({'run_id': args.run_id, 'action_utc': utc(),
                                  'action_mono': gate.action_mono}) + '\n')
        try:
            os.link(temp_marker, marker)
        finally:
            temp_marker.unlink(missing_ok=True)
        print(json.dumps({'phase': 'ARMED', 'run_id': args.run_id,
                          'utc': utc()}))
        return 0
    if not alive and state['phase'] in {'starting', 'ready', 'running'}:
        state['phase'] = 'invalid'
        state['gate']['invalid_reason'] = 'early_logger_exit_without_terminal_state'
        atomic_json(state_path, state)
    print(json.dumps({'phase': state['phase'].upper(), 'run_id': args.run_id,
                      'samples': state['gate']['samples'],
                      'baseline_streak': state['gate']['baseline_streak'],
                      'reason': state['gate']['invalid_reason'],
                      'logger_alive': alive}))
    return 0 if args.mode == 'status' or state['phase'] == 'valid' else 2


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('mode', choices=('run', 'arm', 'status', 'finalize'))
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--baseline', choices=('corp', 'quarantine'))
    parser.add_argument('--seconds', type=int, default=60)
    parser.add_argument('--port', type=int, default=32830)
    parser.add_argument('--password-file', type=Path,
                        default=Path('/root/nac-linux.rootpw'))
    parser.add_argument('--root', type=Path, default=ROOT)
    args = parser.parse_args()
    if not RUN_ID.fullmatch(args.run_id):
        parser.error('run-id must be 3..64 ASCII letters, digits, dash or underscore')
    directory = args.root / args.run_id
    if args.mode == 'run':
        if args.baseline is None:
            parser.error('run requires --baseline')
        return run(args, directory)
    return control(args, directory)


if __name__ == '__main__':
    sys.exit(main())
