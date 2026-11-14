# Operator CoA start: atomic command persistence (W2)

Status: proposed (draft PR, branched from `pilot-aruba-rc5`). It is not a
candidate. `pilot-aruba-rc4` (`0148bf8`) and `pilot-aruba-rc5` (`5ddc157`)
are unchanged. It is independent of the session read-consistency change
(PR #6). Both touch `internal/pilot` but different functions.

## Defect

In rc5, `RequestCoA` (Quarantine, Restore, Reauthenticate) wrote its
command in separate commits:

1. `InsertCoA`: its own transaction. It locks the session row, refuses a
   second in-flight CoA, inserts the CoA as `requested` and **commits**.
2. `SetSessionOverride`: a separate statement.
3. The operator timeline event, also a separate statement.
4. The audit record, then a goroutine that sends the CoA.

| Failure | Effect in rc5 |
|---|---|
| Step 2 or 3 fails (database or connection error) | The CoA stays `requested` forever and is never sent. The in-flight check refuses **every** later CoA on the session ("already in progress") until nacd restarts and `FailInterruptedCoAs` clears it. |
| A competing change commits between the unlocked read and step 1 | The CoA's requested segment and the override come from a stale read. Example: a Reauthenticate prepared before another operator's Quarantine completed records and sends corp VLAN 10. |
| A reader between steps 1 and 2 | Sees a Quarantine CoA without the override (the PR #6 report, W2). |

## Fix

`postgres.PilotRepo.StartCoA` runs **one transaction**:

1. **Lock and re-check.**
   - `SELECT state, override, last_decision_id ... FOR UPDATE`, then
     compare with the session the action was prepared from.
   - Any difference → `ErrSessionChanged` (a `409 Conflict` class), and
     nothing is written.
2. **In-flight check.** If a CoA is `requested` or `sent` →
   `ErrConflict`, as before.
3. Insert the CoA (`requested`).
4. Write the operator override, if the action changes it (override
   columns only, as before).
5. Insert the operator timeline event.
6. `COMMIT`.

How `RequestCoA` uses it:

- **Preparing.** It prepares the action from an unlocked read: it validates
  the session, predicts the target from the active policy, and builds the
  target and the event. Prediction reads through other pool connections,
  so it cannot run while the row is locked.
- **Retry.** If `StartCoA` reports `ErrSessionChanged`, it prepares again
  from the new state, up to 3 times, and otherwise returns `409`.
- **Sending.** Only after `StartCoA` returns nil, that is after `COMMIT`,
  does it write the audit record and start the goroutine that sends the
  CoA. A failed start therefore sends nothing, and a CoA is never sent
  for a state NAC has not committed.

**Unchanged:**
- the CoA exchange (`runCoA`, retries, never re-targeting);
- the attributes sent;
- the RADIUS path;
- the schema;
- the API contract, except that a lost race now returns `409` instead of
  recording a stale action;
- `FailInterruptedCoAs`, still needed for a crash *after* commit, in the
  middle of the exchange.

**Still best-effort, as before:** the audit record, which comes after the
commit. Making operator audit transactional, or moving it to an outbox, is
the same requirement as the change-request audit (slice A). It is not
widened here.

## Tests (PostgreSQL, deterministic)

`postgres.WithCoAStartHook(ctx, fn)` calls `fn(stage)` at these points:
- `prepared` (before the transaction);
- `locked`;
- `coa_inserted`;
- `override_set`;
- `event_inserted` (immediately before `COMMIT`).

A non-nil return aborts the start like a database error. Only Go code can
set the hook; nothing in nacd does, and no request can.

| Test | What is injected | Asserted |
|---|---|---|
| `TestCoAStart_FailureLeavesNothingAndNeverBlocks` | An error at each in-transaction stage, and a **real** connection kill (`pg_terminate_backend` on the open transaction) after the CoA insert and before `COMMIT` | After each failure there is no CoA, no override, no operator event and no CoA packet. Then a normal Quarantine starts, is sent, and does not stay `requested`. |
| `TestCoAStart_NothingSentOrVisibleBeforeCommit` | A pause immediately before `COMMIT` | No packet has reached the fake switch, and another connection sees neither the CoA nor the override. The packet is sent after commit. |
| `TestCoAStart_StalePreparationIsRedone` | Between preparation and the lock, a competing Quarantine commits and the switch completes it | The re-check catches it (the lock is taken twice). The Reauthenticate is recorded with segment `quarantine` VLAN 20, not the stale corp VLAN 10. Both actions are committed whole. |
| `TestCoAStart_ConcurrentActionsOneWinner` | 8 concurrent Quarantine/Reauthenticate requests | Exactly 1 winner and 7 `ErrConflict`. Exactly one CoA, one operator event, and the override matches the winner's kind. |

**Mutation checks.**
- Restoring rc5's two-commit order makes the failure test fail with
  `partial start committed: {override: coas:1 opEvents:0}`, which is exactly
  the stuck state.
- Removing the re-check under the lock makes the stale test fail.

## Candidate and qualification impact

**Candidate.** None is staged. If the owner wants this batched, the
candidate is identified by the exact commit and the `nacd` (and, if
changed, adapter) SHA-256 after review; no tag is created. Batching with
PR #6 or U3 is the owner's call.

**Affected-test rerun list for such a candidate:**
- **Session actions:** one Quarantine, Restore and Reauthenticate each, on
  the Arista VLAN path and the Aruba role path. Check that the CoA, the
  override, the timeline and what the switch received agree.
- **Concurrency:** a second action while one is in flight → `409`, and the
  session is not blocked afterwards.

**Carried, not rerun:** RADIUS authorization, EAP-TLS, the adapter,
installer and enforcement rows. The packet contents and the exchange are
unchanged.
