# Session read consistency

Status: proposed (draft PR, branched from `pilot-aruba-rc5`). It is not a
candidate. `pilot-aruba-rc4` (`0148bf8`) and `pilot-aruba-rc5` (`5ddc157`)
are unchanged.

## Problem

Up to rc5, `GET /api/v1/sessions/{id}` read the session row, then (in
separate statements on the pool, READ COMMITTED) the CoA requests and events
for `last_coa` and liveness, and then decisions, CoA requests and events
again for `decisions`, `coa_requests` and `timeline`. Each statement saw the
data committed when it started. So one response could combine different
committed moments:

- the session before a re-authentication (requested VLAN 10) with the CoA
  after it (`observed`). CI caught this on PR #5
  (`TestPilotWorkflow_EndToEnd`, 1 failure in 40 runs under `-race`);
- the session without an operator override, with that operator's Quarantine
  CoA in `last_coa`;
- `last_coa` from the first CoA read and `coa_requests` from the second.

`GET /api/v1/sessions` had the same issue per row. This is a read-model
defect. It is not evidence that the switch allowed prohibited traffic.

## Contract

| Response | Read from one snapshot | Outside the snapshot |
|---|---|---|
| Session detail | session row, switch row, device, binding, decisions, CoA requests, newest 1000 events | `switch.status` (FreeRADIUS client file, probe, verification): switch commissioning state, read after the snapshot |
| Session list | session rows (filter, limit 200) and, per row, its switch row, CoA requests and events | none |

Every derived field is built only from those records: `requested`,
`observed`, `last_coa`, `liveness`, `endpoint_reachability`,
`last_decision`, `timeline` and `explanation`. `last_coa` is the newest entry
of `coa_requests`, from the same list. `last_decision` is the decision that
the session row names, not the newest decision row (see writer ordering, W1).

A coherent snapshot is not fresh state. It is what NAC had recorded at that
moment. It proves nothing new about the switch or the endpoint, so freshness,
source, time and unknown states are still reported as before (`observed`,
`liveness`, `endpoint_reachability`). An action committed after the snapshot
shows up on the next read. `last_coa` stays tied to this session: it is a
CoA the session targeted or one its own re-authentication observed. It is
never a CoA from another session on the port.

## Implementation

- `postgres.PilotRepo.SessionSnapshot` and `SessionListSnapshot` run all
  their reads in one transaction:
  `BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY`.
  - PostgreSQL takes the snapshot at the first statement, and every later
    statement sees that same snapshot.
  - A plain READ COMMITTED transaction would not help, because each of its
    statements takes a new snapshot. The tests below show this.
  - The transaction holds only database reads. It does no network or file
    I/O, takes no row locks, never blocks writers and is never blocked by
    them. It ends before the response is built.
  - It is read-only, so it cannot hit a serialization failure.
- `pilot.Service.buildView` is a pure function of the loaded records and
  replaces `view`, which used to issue its own queries.
- There is no change to global isolation, no SERIALIZABLE, and no change to
  any writer.
- Cost:
  - same number of queries as before (detail: 6 reads, previously 9);
  - list: 1 + 2 per row + 1 per distinct switch, as before;
  - one connection is held for the duration of the reads.
- One behaviour change on errors: a database error reading the device or
  binding now fails the request (500) instead of silently omitting the
  field. Not-found still omits it.

## Tests (deterministic, PostgreSQL)

`test/integration/session_snapshot_test.go` uses `postgres.WithSnapshotHook`.
The hook runs a callback inside the read transaction, right after the
statement that fixes the snapshot. Each test works the same way:

1. The callback commits a real writer.
2. As a control, it runs a separate READ COMMITTED query, which **does** see
   the write. That proves the interleaving happened.
3. The test asserts on the response exactly as returned, with no second GET.

The hook can be set only from Go code, through a context key. No request
can set it.

| Test | Writer committed mid-read | Asserted on the response |
|---|---|---|
| `QuarantineCommittedMidRead` | operator Quarantine (`RequestCoA`) | no override, no `last_coa`, no CoA rows, no operator event, no override in the explanation |
| `RestoreCommittedMidRead` | operator Restore after an observed Quarantine | override `quarantine`, one CoA, `last_coa` is the observed Quarantine, requested VLAN 20 |
| `CoAObservedMidRead` (the CI case) | switch re-authentication: session update, then CoA correlated | `last_coa` and `coa_requests` both `acked`, requested VLAN 10, same `last_decision` and decision count as before |
| `ReplacementCommittedMidRead` | accounting Stop, new authentication, Start | old session still `active`, no `ended_at`, no Stop in the timeline, liveness basis `accounting Start, no Stop` |
| `ListQuarantineCommittedMidRead` | operator Quarantine during the list | row has no override and no `last_coa` |
| `DecisionCommittedBeforeSessionUpdate` | W1 intermediate state (decision row only) | `last_decision` is the session's decision, not shown in the explanation |

Every detail test also checks `coherent()`, which requires all of these:

- `last_coa` is present exactly when `coa_requests` is non-empty;
- `last_coa` equals the last entry of `coa_requests`;
- every timeline entry that references a CoA references one in
  `coa_requests`;
- there is one timeline CoA row per CoA, and its state matches.

Mutation check: with the snapshot changed to a plain READ COMMITTED
transaction, the first five tests fail, each on the mixed state it targets.
With REPEATABLE READ, all pass under `-race`.

The end-to-end tests that poll and retry still test eventual convergence.
They are not the proof of this fix.

## Writer ordering and transaction boundaries

A snapshot cannot repair an intermediate state that a writer has already
committed. The writers that matter here, as they are in rc5:

| | Writer | Statements (each commits on its own) | Intermediate state a snapshot can see | Assessment |
|---|---|---|---|---|
| W1 | RADIUS Authorize | `InsertDecision` → `UpdateSession` → decision event → `correlateCoA` (`UpdateCoAFrom` sent/acked → observed) → "observed" event | (a) a decision row the session does not name yet; (b) the session updated, CoA still `acked`; (c) CoA `observed`, its event not yet written | (a) **handled in this change**: `last_decision` follows the session row. (b) a legitimate "observation pending" state. The order guarantees that `observed` implies the session already carries that decision, which is why `CoAObservedMidRead` holds. (c) a timeline gap of one statement; acceptable. |
| W2 | Operator CoA (`RequestCoA`) | `GetSession` (no lock) → `InsertCoA` (tx: session row `FOR UPDATE`, in-flight check, insert) → `SetSessionOverride` → operator event → audit → `runCoA` goroutine | CoA `requested` while the override is unchanged | A short read-model window; nothing is sent until after the override commits. **Failure path, a writer defect, not fixed here:** if `SetSessionOverride` fails after `InsertCoA` committed, the CoA stays `requested`, nothing is sent, and the in-flight check refuses every further CoA on that session until nacd restarts (`FailInterruptedCoAs`). The decision inputs (`prevOverride`, prediction) are also read before the lock. |
| W3 | CoA exchange (`runCoA`) | `UpdateCoA` per state (sent, acked/nak/failed) | each state in turn | Correct: no transaction is held across the network exchange. |
| W4 | Accounting | `UpdateSession` (state/ended) → accounting event | ended, event not yet written | Acceptable; liveness says `ended` from the row. |
| W5 | Identity acquired by an identity-less session | `SetSessionOverride` (inherited) → … → `UpdateSession` (identity) | override present before identity | Safe order: never identity without the inherited override. |
| W6 | Observation poller | `UpdateSessionObservation` | none (one statement) | Correct. |

Recommended follow-up, as a separate reviewed change: make W2 one
transaction. That means re-reading the session under the row lock that
`InsertCoA` already takes, then inserting the CoA, setting the override and
writing the operator event in the same transaction. The audit record should
go in that same transaction or through a reliable outbox, which is the
audit-durability requirement ADR-0008 already sets for change requests. This
removes the stuck-`requested` failure path and the W2 window. It changes the
command path, so it needs its own tests and rerun list. It is not part of
this read-model change.

## Candidate and qualification impact

- **Candidate:** none yet. A candidate is staged only after review. It
  would be rc5 plus this change, built from a tagged commit with new pins.
  U3, the AOS-CX config wording on `claude/nac-u3-aruba-config-copy`,
  stays separate unless the owner batches it explicitly.
- **Affected-test rerun list for that candidate:**
  - every console/API view of a session: Sessions list, Session detail
    (Why panel, timeline, Requested/Observed, last CoA);
  - one Quarantine/Restore pair, checking that the UI and the API agree on
    each read;
  - no enforcement, RADIUS reply, CoA packet, adapter or installer
    behaviour changes, so P1, the RADIUS/EAP-TLS rows and the enforcement
    rows are carried, not rerun.
- **EVE:** no campaign is needed for this proof. The deciding interleaving
  is forced by the deterministic tests above; repeated lab runs cannot force
  it.
