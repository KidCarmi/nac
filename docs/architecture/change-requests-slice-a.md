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
