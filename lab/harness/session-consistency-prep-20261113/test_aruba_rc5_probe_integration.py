"""Local socket fixture for the EVE serial probe controller; no lab access."""
import argparse
import json
from pathlib import Path
import socket
import tempfile
import threading
import time
import unittest

from aruba_rc5_run_probe_guest import control, run


class FakeSerial:
    def __init__(self, run_id, *, early=False):
        self.run_id = run_id
        self.early = early
        self.listener = socket.socket()
        self.listener.bind(('127.0.0.1', 0))
        self.listener.listen(1)
        self.port = self.listener.getsockname()[1]
        self.thread = threading.Thread(target=self.serve, daemon=True)
        self.thread.start()

    def serve(self):
        with self.listener:
            conn, _ = self.listener.accept()
            with conn:
                conn.settimeout(5)
                conn.sendall(b'root@nac-endpoint:~# \n')
                pending = bytearray()
                while b'aruba_transition_probe_endpoint.py' not in pending:
                    pending.extend(conn.recv(4096))
                def send(row):
                    conn.sendall((json.dumps(row) + '\n').encode())
                send({'event': 'probe_start', 'run_id': self.run_id})
                for _ in range(3):
                    time.sleep(.25)
                    send({'event': 'probe', 'run_id': self.run_id,
                          'probes': {'corp': 'http_200', 'remediation': 'http_200'}})
                if self.early:
                    return
                started = time.monotonic()
                while time.monotonic() - started < 8.5:
                    time.sleep(.45)
                    send({'event': 'probe', 'run_id': self.run_id,
                          'probes': {'corp': 'http_200', 'remediation': 'http_200'}})
                send({'event': 'probe_end', 'run_id': self.run_id})


class ControllerTests(unittest.TestCase):
    def args(self, root, port, pw, run_id):
        return argparse.Namespace(mode='run', run_id=run_id, baseline='corp',
                seconds=10, port=port, password_file=pw, root=root)

    def wait_phase(self, state_path, wanted, timeout=5):
        limit = time.monotonic() + timeout
        while time.monotonic() < limit:
            if state_path.exists():
                state = json.loads(state_path.read_text())
                if state['phase'] == wanted:
                    return state
            time.sleep(.05)
        self.fail(f'never reached {wanted}')

    def test_ready_arm_and_complete_window(self):
        with tempfile.TemporaryDirectory(dir=Path(__file__).parent) as temp:
            root = Path(temp)
            pw = root / 'password'
            pw.write_text('unused')
            fixture = FakeSerial('local-valid')
            args = self.args(root, fixture.port, pw, 'local-valid')
            result = []
            worker = threading.Thread(target=lambda: result.append(run(args, root / args.run_id)))
            worker.start()
            self.wait_phase(root / args.run_id / 'state.json', 'ready')
            args.mode = 'arm'
            self.assertEqual(control(args, root / args.run_id), 0)
            worker.join(15)
            self.assertFalse(worker.is_alive())
            self.assertEqual(result, [0])
            args.mode = 'finalize'
            self.assertEqual(control(args, root / args.run_id), 0)

    def test_early_serial_exit_is_invalid(self):
        with tempfile.TemporaryDirectory(dir=Path(__file__).parent) as temp:
            root = Path(temp)
            pw = root / 'password'
            pw.write_text('unused')
            fixture = FakeSerial('local-early', early=True)
            args = self.args(root, fixture.port, pw, 'local-early')
            result = []
            worker = threading.Thread(target=lambda: result.append(run(args, root / args.run_id)))
            worker.start()
            worker.join(10)
            self.assertFalse(worker.is_alive())
            self.assertEqual(result, [2])
            state = json.loads((root / args.run_id / 'state.json').read_text())
            self.assertEqual(state['phase'], 'invalid')
            self.assertIn('disconnected_early', state['gate']['invalid_reason'])
            args.mode = 'finalize'
            self.assertEqual(control(args, root / args.run_id), 2)


if __name__ == '__main__':
    unittest.main()
