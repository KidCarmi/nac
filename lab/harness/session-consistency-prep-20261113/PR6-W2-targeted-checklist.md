# Targeted successor checklist: PR #6 and separate W2 fix

Prepared 2026-11-14 by ASTRA. **Preparation only.** The EVE lab remains stopped; rc4/rc5 evidence and pins are unchanged. This document does not qualify, deploy, or select a successor.

## Current reviewed scope and separation

- [Draft PR #6](https://github.com/KidCarmi/nac/pull/6) is at `9ef33c5662e274a1f5aae9a9bc5230bf97d871ab` on the rc5 branch. It has passing CI but no submitted review as checked on 2026-11-14. It is **not** a candidate. Its scope is a consistent `REPEATABLE READ READ ONLY` session detail/list snapshot and pure derivation of `last_coa`, timeline, explanation, liveness and `last_decision`. Its changed paths are `internal/storage/postgres/session_snapshot.go`, `pilot_repo.go`, `internal/pilot/sessions.go`, `service.go`, `switches.go`, `test/integration/session_snapshot_test.go`, the OpenAPI descriptions, architecture documentation and changelog. `web/ui`, RADIUS/CoA writers, schema, adapter and installer are unchanged. `switch.status` is explicitly outside the database snapshot.
- [PR #5 coordination](https://github.com/KidCarmi/nac/pull/5#issuecomment-6854136570) confirms OPUS's six deterministic PostgreSQL tests, the mutation check against READ COMMITTED, and the same response invariant without a second GET. These are OPUS's proof of the interleaving fix; repeated EVE runs cannot replace them.
- **W2 is separate and not yet a reviewed PR or candidate.** At rc5, `internal/pilot/coa.go` calls `InsertCoA`, then `SetSessionOverride`, then event/audit and `runCoA`. A failure after the committed insert can leave a `requested` CoA that blocks later actions until restart. W2 needs a reviewed writer-path change and its own failure-injection tests. Do not treat PR #6's read snapshot as a W2 fix.
- U3 generated AOS-CX wording is separate. Include its check only if a future assignment explicitly includes U3.

## Entry gate before any EVE execution

Record all of the following in the assignment and run manifest:

1. The **reviewed exact commit** and review outcome, plus the complete list of included fixes (PR #6, W2, U3, or other). A Git tag is **not required**.
2. SHA-256 of the exact `nacd` binary and the exact adapter artifact, or an explicit unchanged-adapter statement with the previously pinned adapter SHA-256. Verify installed artifacts against those hashes before testing.
3. Confirmed affected source/API/UI/configuration paths and the rerun list for that exact build. For W2, require the transaction and audit-durability contract, affected paths and deterministic failure-injection results from OPUS.
4. An **explicit lab assignment** authorizing the bounded checks. Passing CI, a draft PR, a build hash, or a proposed rerun list alone does not authorize deployment.

If any gate is absent, mark successor lab checks **NOT RUN** and leave the lab stopped.

## A. PR #6 read-model checks, if included in the assigned build

| ID | Targeted check | Pass evidence |
| --- | --- | --- |
| A1 | Review OPUS's deterministic PostgreSQL tests in `test/integration/session_snapshot_test.go`: Quarantine, Restore, CoA observed during re-authentication, session replacement, list Quarantine and intermediate decision. | Real writer commits mid-read; READ COMMITTED control sees the write; the original response is coherent without a second GET. Mutation to READ COMMITTED fails the first five cases. This is a prerequisite supplied by OPUS, not an EVE campaign. |
| A2 | On one assigned candidate, compare `GET /api/v1/sessions`, `GET /api/v1/sessions/{id}`, the console Sessions list and Session detail before, during and after one Quarantine/Restore pair. | Within each detail/list response, `last_coa` equals the newest `coa_requests` entry for the same session incarnation; timeline CoA references and states agree; the Why panel, requested/observed authorization, override, last decision, identity and policy data have no impossible mix. A new action may appear only on the next read. |
| A3 | Check source, observation time, freshness and unknown states against switch and endpoint evidence. | UI/API describe recorded NAC state accurately. `switch.status`, an ACK and an `observed` CoA are not shown as proof of current endpoint reachability. Legitimate `acked`/observation-pending states remain visible. |
| A4 | Use the prepared gated logger for exactly one Aruba Quarantine/Restore pair on the same endpoint, port, certificate and switch setup. Independently verify both TCP services first. | Each run has READY before action and final VALID; correlate product action, CoA request/ACK, switch role/session, address/route/DNS and fresh TCP probes. Report manual intervention. Discard timing from an INVALID capture. Do not turn this into a performance campaign. |

The [probe harness and local fixture results](README.md) are prepared but not installed into EVE. Existing rc4/rc5 RADIUS, admission and enforcement evidence remains identifiable and is carried only if the reviewed candidate's scope truly leaves those paths unchanged.

## B. W2 writer-path checks, only if separately reviewed and assigned

| ID | Targeted check | Pass evidence |
| --- | --- | --- |
| B1 | OPUS forces failure at override write, operator event and audit persistence boundaries, plus a concurrent session replacement/command case. | The command fails without a stuck `requested` CoA, unintended CoA packet or split override/audit state. A later valid command is not blocked. The implementation rereads under the session lock and commits the intended records atomically or documents an equivalent durable outbox contract. These deterministic tests belong to OPUS. |
| B2 | If W2 is in an explicitly assigned build, exercise one normal Quarantine/Restore pair through the UI with independent fresh TCP probes and CoA/switch evidence. | The normal writer path preserves access behavior and records a coherent request, override, event and audit outcome. A1–A4 do not substitute for B1. |

Before B1/B2, OPUS must name the reviewed W2 commit, changed paths, exact included behavior and hashes. If W2 is absent from the assigned candidate, classify B1/B2 **NOT RUN**, not PASS. No W2 PR or candidate was found in the repository at preparation time.

Arista management access remains **BLOCKED** pending a separately authorized path. Do not retry the rejected privileged-access expansion. Windows and physical-switch gaps remain in the existing evidence matrix; neither is silently cleared by these checks.
