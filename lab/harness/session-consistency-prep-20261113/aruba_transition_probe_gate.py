"""Pure state machine for a bounded Aruba transition probe.

READY is a pre-action gate. VALID means the logger covered the action window;
it is not a reachability verdict or a CoA timing measurement.
"""
from dataclasses import dataclass


@dataclass
class ProbeGate:
    run_id: str
    baseline: str
    seconds: int
    required_baseline: int = 3
    started_mono: float | None = None
    ready_mono: float | None = None
    last_mono: float | None = None
    ended_mono: float | None = None
    baseline_streak: int = 0
    samples: int = 0
    post_action_samples: int = 0
    action_mono: float | None = None
    invalid_reason: str | None = None

    def __post_init__(self):
        if self.baseline not in {'corp', 'quarantine'}:
            raise ValueError('baseline must be corp or quarantine')
        if not 10 <= self.seconds <= 300:
            raise ValueError('duration must be 10..300 seconds')

    def ingest(self, row: dict, observed_mono: float):
        if self.invalid_reason or self.ended_mono is not None:
            raise ValueError('row after invalid or completed run')
        if row.get('run_id') != self.run_id:
            self.invalid_reason = 'run_id_mismatch'
            return
        kind = row.get('event')
        if self.started_mono is None:
            if kind != 'probe_start':
                self.invalid_reason = 'missing_probe_start'
                return
            self.started_mono = observed_mono
        elif kind == 'probe_start':
            self.invalid_reason = 'duplicate_probe_start'
            return
        elif kind == 'probe_end':
            self.ended_mono = observed_mono
        elif kind == 'probe':
            probes = row.get('probes')
            if not isinstance(probes, dict):
                self.invalid_reason = 'malformed_probe_row'
                return
            self.samples += 1
            if self.action_mono is not None and observed_mono > self.action_mono:
                self.post_action_samples += 1
            if self.action_mono is None:
                if self._matches_baseline(probes):
                    self.baseline_streak += 1
                    if self.ready_mono is None and self.baseline_streak >= self.required_baseline:
                        self.ready_mono = observed_mono
                else:
                    self.baseline_streak = 0
        else:
            self.invalid_reason = 'unknown_event'
            return
        self.last_mono = observed_mono

    def _matches_baseline(self, probes: dict) -> bool:
        if probes.get('remediation') != 'http_200':
            return False
        if self.baseline == 'corp':
            return probes.get('corp') == 'http_200'
        return probes.get('corp') == 'TimeoutError'

    def arm(self, action_mono: float, *, logger_alive: bool, last_seen_age: float):
        if self.invalid_reason:
            raise ValueError('INVALID: ' + self.invalid_reason)
        if self.action_mono is not None:
            raise ValueError('action already marked')
        if self.ready_mono is None or self.baseline_streak < self.required_baseline:
            raise ValueError('baseline not ready')
        if self.ended_mono is not None or not logger_alive:
            raise ValueError('logger exited before action')
        if last_seen_age < 0 or last_seen_age > 3:
            raise ValueError('probe stream is stale')
        if action_mono < self.ready_mono:
            raise ValueError('action marker precedes readiness')
        self.action_mono = action_mono

    def verdict(self, *, logger_exited: bool) -> tuple[str, str]:
        if self.invalid_reason:
            return 'INVALID', self.invalid_reason
        if not logger_exited:
            return 'INVALID', 'logger_still_running_at_finalize'
        if self.started_mono is None or self.ready_mono is None:
            return 'INVALID', 'baseline_never_ready'
        if self.action_mono is None:
            return 'INVALID', 'action_not_marked'
        if self.ended_mono is None:
            return 'INVALID', 'early_logger_exit_or_missing_end'
        if self.ended_mono - self.started_mono < self.seconds - 2:
            return 'INVALID', 'probe_ended_early'
        if self.action_mono >= self.ended_mono or self.post_action_samples < 1:
            return 'INVALID', 'action_not_covered_by_probes'
        return 'VALID', 'capture_window_complete'
