# Session consistency successor: qualification preparation

**Status:** prepared locally on 2026-11-13. No successor candidate was deployed or tested. The EVE lab remains stopped. rc4 and rc5 evidence and pins are unchanged. Aruba rc5 remains `5ddc15732a7a49bc65df9a0338351cb9dbec55b0`; this directory is for a later reviewed and explicitly assigned candidate only.

## Probe capture gate

`aruba_rc5_run_probe_guest.py` is the EVE-side controller and `aruba_transition_probe_endpoint.py` is a future-run Linux endpoint logger. The filename of the controller retains the historical rc5 name for the operator command; it does **not** change or rerun rc5 evidence. `aruba_transition_probe_gate.py` holds the state machine. Files are staged here for review; none has been installed into the stopped lab.

The controller uses a new, unique run directory under `/mnt/nac-rc5/transition-probes/`, outside the historical evidence paths. It keeps a foreground serial logger running and flushes each probe row to `probes.jsonl`. It emits `READY` only after `probe_start` and three consecutive successful baseline rows. For a corp baseline, both services must return HTTP 200; for quarantine, the corp probe must time out while remediation returns HTTP 200. The endpoint must expose exactly `lo` and `ens3`, with an IPv4 address on `ens3`. Those are deliberately strict lab prerequisites.

Only after `READY`, an operator can run `arm`, which confirms a live process and a probe row no more than three seconds old, then writes a unique action marker. The next UI action must follow immediately. The final window is `VALID` only if the logger records an explicit `probe_end`, covers the requested duration (two-second tolerance), and records at least one post-action probe. Serial disconnect, stall, missing end marker, early process exit, stale baseline, or missing action marker makes the measurement `INVALID`. `VALID` means the capture window is intact. Product-to-ACK and traffic-change times still require the separate product, CoA, switch, and fresh-connection evidence; the gate never declares a NAC outcome.

For a future authorized run on EVE, put the three runtime scripts on the EVE controller and the endpoint logger on the isolated Linux endpoint, then use a **persistent terminal** for the controller. The example below is a command template, not an executed lab result:

```sh
python3 /root/nac-qualification/aruba_rc5_run_probe_guest.py run --run-id successor-q-001 --baseline corp --seconds 60
# In another terminal, after READY and immediately before the product UI action:
python3 /root/nac-qualification/aruba_rc5_run_probe_guest.py arm --run-id successor-q-001
# After the logger ends:
python3 /root/nac-qualification/aruba_rc5_run_probe_guest.py finalize --run-id successor-q-001
```

Use a new run ID and `--baseline quarantine` for Restore. Confirm each TCP service independently from its own segment before using the gate. Do not infer a transition time from `INVALID`, even if an ACK or an endpoint response appears in a partial log. The controller reads the existing root credential file on EVE and does not copy credentials into evidence.

## Local validation

No lab access was used for these tests. On Windows Python 3.12, `py_compile` succeeded for all five Python files; five pure state-machine tests passed; and two fake-serial loopback integration tests passed (exit code 0). The complete-window fixture emitted `READY → ARMED → VALID` with 22 rows and 19 post-action rows. The early-disconnect fixture emitted `READY → INVALID` with `serial_logger_disconnected_early`. The local fixture also exposed and fixed a Windows self-PID process-check interrupt; Linux EVE process checking is unchanged. These fixtures validate the harness behavior, not switch or NAC behavior.

Rerun local checks from this directory:

```sh
python -m py_compile aruba_rc5_run_probe_guest.py aruba_transition_probe_gate.py aruba_transition_probe_endpoint.py test_aruba_transition_probe_gate.py test_aruba_rc5_probe_integration.py
python test_aruba_transition_probe_gate.py -v
python -m unittest test_aruba_rc5_probe_integration -v
```

The fake-serial fixture binds loopback. A restricted execution sandbox may require permission for local sockets.

## Targeted successor regression checklist

**Entry gate:** OPUS identifies a reviewed successor commit, exact `nacd` binary hash and any adapter hash, affected source paths, and the deterministic PostgreSQL concurrency-test result. Record whether U3 Aruba wording is included. Do not use the staged test-only reread (`906cdea`) as proof of a product fix. No EVE deployment before a reviewed candidate is explicitly assigned.

| ID | Check | Expected evidence |
| --- | --- | --- |
| S1 | OPUS's deterministic database/API interleaving tests force a session read across a concurrent Quarantine, Restore, and session replacement. | One response remains internally coherent without a second GET; `last_coa`, `coa_requests`, timeline, explanation, session incarnation and policy data agree. OPUS reports writer ordering and transaction boundaries. This proof belongs to OPUS, not repeated EVE runs. |
| S2 | Compare UI session detail, raw `GET /sessions/{id}`, and session list row during and after one Quarantine/Restore pair. | A single detail response never mixes incompatible stored moments. Document any intentionally weaker list-row contract. Source, observation time, freshness, and unknown state remain visible; an `observed` CoA is never presented as endpoint reachability by itself. |
| S3 | Independently verify corp and remediation TCP services, then capture one Aruba Quarantine and Restore pair on the same endpoint, port, certificate and candidate. | `READY` before each action and final `VALID`; product action and CoA request/ACK, switch role/session, address/route/DNS, and continuous fresh TCP probes agree. Record automatic behavior and every manual intervention separately. Use no timing from an invalid capture. |
| S4 | If U3 generated-config wording is bundled, compare generated AOS-CX text and run only the related documented `nacd` smoke checks. | Wording names the qualified virtual build and does not claim physical verification or connection teardown. Exact included commit/path is recorded. |

At rc5, the identified product read paths are `internal/pilot/sessions.go` (`view`, `ListSessions`, `SessionDetail`) and the store contract in `internal/pilot/service.go`. OPUS should confirm the actual successor changes, storage/transaction implementation paths, and relevant API/console test paths before S2/S3. A consistent stored snapshot does not prove current switch state or endpoint reachability.

Arista access remains **BLOCKED** pending a separately authorized management path. No privileged-access change is retried. Windows, physical hardware and wider DHCP/shared-port/IPv6/UDP coverage are outside this bounded preparation.
