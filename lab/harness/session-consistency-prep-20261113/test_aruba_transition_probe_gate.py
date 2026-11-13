"""Local validation of the readiness and invalid-timing safety gate."""
import unittest

from aruba_transition_probe_gate import ProbeGate


def row(run_id, corp='http_200', remediation='http_200'):
    return {'event': 'probe', 'run_id': run_id,
            'probes': {'corp': corp, 'remediation': remediation}}


class ProbeGateTests(unittest.TestCase):
    def test_corp_requires_three_good_baseline_samples(self):
        gate = ProbeGate('corp-001', 'corp', 60)
        gate.ingest({'event': 'probe_start', 'run_id': gate.run_id}, 0)
        gate.ingest(row(gate.run_id), 1)
        gate.ingest(row(gate.run_id, remediation='TimeoutError'), 2)
        gate.ingest(row(gate.run_id), 3)
        gate.ingest(row(gate.run_id), 4)
        self.assertIsNone(gate.ready_mono)
        with self.assertRaisesRegex(ValueError, 'baseline not ready'):
            gate.arm(4.1, logger_alive=True, last_seen_age=.1)
        gate.ingest(row(gate.run_id), 5)
        self.assertEqual(gate.ready_mono, 5)
        gate.arm(5.1, logger_alive=True, last_seen_age=.1)
        gate.ingest(row(gate.run_id, corp='TimeoutError'), 6)
        gate.ingest({'event': 'probe_end', 'run_id': gate.run_id}, 60)
        self.assertEqual(gate.verdict(logger_exited=True),
                         ('VALID', 'capture_window_complete'))

    def test_quarantine_requires_reachable_remediation(self):
        gate = ProbeGate('rest-001', 'quarantine', 60)
        gate.ingest({'event': 'probe_start', 'run_id': gate.run_id}, 0)
        for i in range(1, 4):
            gate.ingest(row(gate.run_id, corp='TimeoutError',
                            remediation='TimeoutError'), i)
        self.assertIsNone(gate.ready_mono)
        for i in range(4, 7):
            gate.ingest(row(gate.run_id, corp='TimeoutError'), i)
        self.assertEqual(gate.ready_mono, 6)

    def test_early_logger_exit_invalidates_timing(self):
        gate = ProbeGate('early-001', 'corp', 60)
        gate.ingest({'event': 'probe_start', 'run_id': gate.run_id}, 0)
        for i in range(1, 4):
            gate.ingest(row(gate.run_id), i)
        with self.assertRaisesRegex(ValueError, 'logger exited'):
            gate.arm(4, logger_alive=False, last_seen_age=1)
        gate.arm(4, logger_alive=True, last_seen_age=1)
        gate.ingest(row(gate.run_id), 5)
        self.assertEqual(gate.verdict(logger_exited=True),
                         ('INVALID', 'early_logger_exit_or_missing_end'))

    def test_no_action_or_short_run_is_invalid(self):
        gate = ProbeGate('short-001', 'corp', 60)
        gate.ingest({'event': 'probe_start', 'run_id': gate.run_id}, 0)
        for i in range(1, 4):
            gate.ingest(row(gate.run_id), i)
        gate.ingest({'event': 'probe_end', 'run_id': gate.run_id}, 60)
        self.assertEqual(gate.verdict(logger_exited=True),
                         ('INVALID', 'action_not_marked'))
        gate2 = ProbeGate('short-002', 'corp', 60)
        gate2.ingest({'event': 'probe_start', 'run_id': gate2.run_id}, 0)
        for i in range(1, 4):
            gate2.ingest(row(gate2.run_id), i)
        gate2.arm(3.1, logger_alive=True, last_seen_age=.1)
        gate2.ingest(row(gate2.run_id), 4)
        gate2.ingest({'event': 'probe_end', 'run_id': gate2.run_id}, 9)
        self.assertEqual(gate2.verdict(logger_exited=True),
                         ('INVALID', 'probe_ended_early'))

    def test_stale_stream_cannot_be_armed(self):
        gate = ProbeGate('stale-001', 'corp', 60)
        gate.ingest({'event': 'probe_start', 'run_id': gate.run_id}, 0)
        for i in range(1, 4):
            gate.ingest(row(gate.run_id), i)
        with self.assertRaisesRegex(ValueError, 'stale'):
            gate.arm(7, logger_alive=True, last_seen_age=4)


if __name__ == '__main__':
    unittest.main()
