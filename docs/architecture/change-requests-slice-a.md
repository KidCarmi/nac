# Review-only change requests: slice A as implemented

Status: draft implementation, in three stacked PRs:

| Milestone | Content | PR |
|---|---|---|
| M1 | Schema and repository | KidCarmi/nac#8 |
| M2 | Service, validation, API and threat model | KidCarmi/nac#9 |
| M3 | Frozen-input replay, API and console | this PR |

Scope comes from the implementation plan on PR #2
(`docs/architecture/implementation-plan-change-request-core.md`) as the owner
sequenced it. The user authorized slice A only. Nothing here is an accepted
ADR default, and nothing is merged or deployed.

## What a change request is

A change request is a **review-only proposal** for the policy rule set:
persisted, revisioned and validated, with reproducible historical replays.

- **It never changes the active policy, a session, a CoA or a switch.**
  Legacy publish and restore are unchanged, so a change request is not
  governance of policy changes. The API, the console and every record say
  `review_only`.
- **Out of scope:** approval, account management, activation, network
  dispatch, delegated tokens and MCP.

## Lifecycle

```
draft --validate(valid)--> validated --simulate--> simulated
  ^         |                  |                       |
  |         +--(invalid)-------+-----------------------+   (invalid validation -> draft)
  +------ edit content (new revision, based on the active version now) ------+
any non-terminal --cancel--> cancelled (terminal, immutable)
```

Every mutation:
- names the revision it is based on (`If-Match`);
- is a compare-and-set under a row lock;
- writes its audit record in the same transaction.

## How the plan corrections are met

| Plan correction | Implementation | Proof |
|---|---|---|
| 1. Frozen replay inputs | `change_request_input_sets` holds the recorded decision inputs and session overrides **copied**, the active version as an id plus digest, and the environment and catalog copied with their digests. The input-set digest is canonical. Simulations are immutable; a refresh creates a new input set. Each row carries missing and stale inputs, and the simulation carries coverage notes and the label | `TestChangeAPI_SimulationFrozenReproducibleAndStale`: a live Quarantine, a mapping change and a legacy publish do not change the stored simulation or its reproduction; the simulation is reported stale with reasons; the refresh copies the new override. The test fails when reproduction reads live cases. Unit: `TestReplay_ReproducibleAndOrdered` |
| 2. No manufactured `all` holder | Each revision stores the immutable `policy_versions` row it was based on. There are no activation records and no `replace` supersession | Schema; `base_drift` after a legacy publish (`TestChangeAPI_FromDraftAndDrift`) |
| 3. Review-only | `workflow` is CHECKed to `review_only`; the label is on every response and page | Schema test (`workflow='mandatory'` refused); walkthrough |
| 4. Exact bindings, conflicts, audit, threat model | `If-Match` on every mutation (`428` if missing, `409` with the current revision); audit in the same transaction; threat-model, trust-boundary and attack-surface entries shipped with the M2 and M3 endpoints | `TestChangeRepo_*` (audit rollback on a real database failure, one winner under concurrency); `TestChangeRoutes_*` |
| No side effects | The service holds only a `PolicyReader` with read methods; the package imports no enforcement, RADIUS, pilot or storage code | Reflection and import tests; `TestChangeAPI_NoSideEffects` compares 8 tables byte for byte across every route and checks that no CoA packet is sent (it fails when a Quarantine is injected) |

## Differences from the plan and the proposed contract, and why

- **Migration numbers are `0007` and `0008`.** `0006` already exists.
- **Read permission is `policy:read`.** The plan said `network:read`; this
  is consistent with the policy routes. The **input set** endpoint needs
  `network:read`, because it holds copied session evidence, like
  `/decisions/{id}`.
- **Validation and simulation need `policy:write`.** The proposed contract
  said `policy:read`, but both record a result and change the state.
- **`from_draft` replaces `restore_version`** for now.
- **The replay window is the preview's (30 days), up to 1000 sessions.** The
  newest decision per session is used.
- **"Canonical JSON":** object keys sorted, no whitespace, no HTML escaping,
  numbers as written. For the ASCII keys and string, integer and boolean
  values stored here, this is the RFC 8785 (JCS) serialization. It is not a
  general JCS implementation, because floats and non-ASCII key ordering are
  not handled.
- **Timestamps in an input set are truncated to microseconds**, so the digest
  survives a PostgreSQL round trip. The reproduction check caught this.

## Known limits, recorded rather than hidden

- **No retention policy for change-request history.** Copied inputs (MAC,
  certificate CN, override) stay with the immutable input set.
- **Reads are not audited.**
- **No per-operator quota.**
- **Replay covers only the latest recorded decision per session in the
  window.** It never re-observes inputs.
- **Approval and activation do not exist.** When mandatory approvals arrive,
  publish, restore and activation must converge on one enforced service
  boundary (plan, correction 3).

## Reproduce it

**One command, no switch or EVE.** Point it at a disposable PostgreSQL
database: the script drops that database's public schema.

```
DEMO_RESET=1 NAC_DB_DSN=postgres://nac:nac@localhost:5432/nac_demo?sslmode=disable \
  scripts/demo-change-requests.sh /tmp/cr-shots
```

**What it does:**
1. builds nacd;
2. resets the database;
3. starts nacd;
4. runs the console walkthrough (12 checks);
5. prints the resulting change request as `GET /api/v1/change-requests/{id}`
   returns it.

The screenshots go to the directory you pass.

**The automated proof**, in PostgreSQL tests:

```
NAC_TEST_DB_DSN=postgres://nac:nac@localhost:5432/nac_test?sslmode=disable \
  go test -tags=integration -race -count=1 -p 1 -run 'TestChange' ./test/integration/
go test -race ./internal/change ./internal/server
```

## Requirement-to-test map

Acceptance criteria from the owner's sequencing (PR #2) and the plan's
corrections. Each row names the test that fails if the requirement breaks.

| # | Requirement | Test(s) | What would make it fail |
|---|---|---|---|
| A1 | Revision conflicts are explicit | `TestChangeRepo_RevisionConflict`; `TestChangeRoutes_IfMatchIsRequiredAndPassedThrough`; `TestChangeAPI_Lifecycle` | A stale `If-Match` returns anything but a `409` naming the current revision; a missing `If-Match` returns anything but `428`; a refused edit writes a revision or an audit record |
| A2 | Concurrent mutations have one winner | `TestChangeRepo_ConcurrentMutationsOneWinner` | Two edits or cancels on the same revision both commit, or the audit count differs from the committed mutations |
| A3 | Terminal state is immutable | `TestChangeRepo_CancelledIsTerminal`; `TestChangeAPI_Lifecycle` | An edit, validation or cancel after a cancel succeeds |
| A4 | History cannot be rewritten | `TestChangeRepo_HistoryIsImmutable` | Raw-SQL UPDATE or DELETE of revisions, validations, input sets or simulations, DELETE of a change request, or `state='approved'` / `workflow='mandatory'` succeeds |
| A5 | Audit durability: mutation and audit commit together | `TestChangeRepo_AuditFailureRollsBackMutation` (mutation-checked: moving the audit after COMMIT fails it); `TestChangeAPI_Lifecycle`; `TestChangeAPI_SimulationFrozenReproducibleAndStale` (audit trail) | A mutation commits although its audit insert failed, or the audit trail misses or duplicates an action |
| A6 | Replay inputs are immutable (frozen input set) | `TestChangeAPI_SimulationFrozenReproducibleAndStale` (mutation-checked: reading live cases fails it); `TestReplay_ReproducibleAndOrdered` (copy) | A live override, mapping change or publish changes a stored simulation or its reproduction; mutating source cases changes the set digest |
| A7 | Canonical output is reproducible | `TestReplay_ReproducibleAndOrdered`; `TestCanonicalDigest_KeyOrderIndependent`; `…/reproduction` in the API test and the walkthrough | Repeated replays differ; key order changes the digest; a stored set fails to reproduce after the PostgreSQL round trip (this caught a real microsecond-truncation bug) |
| A8 | A refresh creates a new simulation, never an update | `TestChangeAPI_SimulationFrozenReproducibleAndStale` | The refresh reuses the simulation ID or input-set digest; the first set's copied override changes |
| A9 | Drift and staleness are reported, not hidden | `TestChangeAPI_FromDraftAndDrift`; `TestChangeAPI_SimulationFrozenReproducibleAndStale`; walkthrough (rebase marks the old simulation stale) | A mapping change, legacy publish or new revision leaves a validation or simulation shown as `current`, or the reasons are missing |
| A10 | Missing and stale inputs and coverage are recorded | `TestReplay_ReproducibleAndOrdered` (stale count); coverage notes in every simulation | Stale or missing evidence is not counted per row and in the summary |
| A11 | Historical replay is never labelled live verification | API test and walkthrough assert the label | A simulation without "not live-network verification" |
| A12 | One shared service behind API and UI | `TestChangeRoutes_*` (handlers only pass through; forbidden requests never reach the service); the console uses only `/api/v1` (walkthrough) | A handler holds workflow logic, or the console needs a private path |
| A13 | Change-request endpoints cannot mutate policy, sessions or switches | `TestChangeAPI_NoSideEffects` (8 tables byte-identical, no CoA packet; checked by injecting a Quarantine); `TestPolicyReaderCannotChangePolicy`; `TestNoImportOfEnforcementPaths`; schema CHECKs | Any route changes policy activations, versions, draft rules or settings, sessions, CoAs, switches or decisions, or sends a CoA |
| A14 | Review-only labelling | Schema CHECK on `workflow`; label assertions in the API test and the walkthrough | A record or page without `review_only` and the label |
| A15 | Bound to the active-policy version; no manufactured `all` holder | Schema (`base_version_id` FK to `policy_versions`, no activation tables); `TestChangeAPI_FromDraftAndDrift` (base drift and rebase) | A revision without its base version, or an activation or `replace` record written |
| A16 | Threat model ships with the endpoints | Review item: `docs/security/threat-model.md` (review-only table), `trust-boundaries.md`, `security/attack-surface.md` in the M2 and M3 diffs | Endpoints added without matching entries |

## Console

The Changes page (`#/changes`, `#/change/<id>`) and Policy's "Propose as
change request" use only the API above.

`scripts/ui-change-requests.js` drives it against a fresh nacd:
- propose from the draft;
- validate, then simulate, with rows and the label;
- reproduce;
- replace the content with the draft (a new revision; the old simulation is
  reported stale);
- cancel.

It also checks that the active policy and the endpoint's VLAN are unchanged.
Result: 12/12.
