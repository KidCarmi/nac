# Implementation plan: change-request core (review-only)

**Status: proposed plan, revised for the owner's sequencing on PR #2
(2026-11-13).** The owner allocated a non-executing draft implementation of
slice A (milestones 1–3). That is bounded development scope. It is not
acceptance of the proposed ADR-0008/0009 defaults, and it is not permission
to merge or deploy. The topology and qualification contracts (re-review of
`b915488`) are still under separate review and are not treated as accepted
here.

This plan delivers **review-only change requests**: persisted, revisioned
and validated proposals, each with a reproducible historical replay,
through one shared application service. Activation, CoA dispatch, delegated
tokens and approval execution stay unavailable.

## Review-only, stated plainly

- The legacy `POST /policy-draft/publish` and
  `/policy-versions/{n}/restore` are unchanged in this slice. Policy
  changes can therefore still bypass change requests.
- A change request is a **reviewed proposal**. It is not governance of
  policy changes. The API, the console and every record call the workflow
  "review-only" and never "required approval".
- When mandatory approvals arrive (a later, separately reviewed step), every
  path that changes the active policy must converge on **one enforced
  service boundary**: publish, restore, and change-request activation. No
  path may be left that changes policy without it. This is a requirement on
  that later step, recorded here so that this slice is not mistaken for it.

## Slices

| Slice | Content | Status |
|---|---|---|
| **A** (allocated) | M1 schema/repository, M2 service + validation + API, M3 frozen-input replay + API/console. Threat-model updates ship with each milestone's endpoints | draft implementation after the plan corrections below |
| **B** (later, own review) | Approval request and approval, operator management, `approver` role | needs O1, O5 and the operator-management decision |
| **C** (later) | Activation, CoA dispatch, stop, delegated principals, MCP | needs ADR-0008 §5 acceptance, N1–N9, O2–O4 |

Slice A does not wait for O1, O5 or operator management.

## Slice A scope

**In:**
1. **Persisted change requests with revisions.**
   - create, read, list, edit and cancel;
   - edits use `If-Match: <revision>`; a stale revision is a `409` naming
     the current revision;
   - content has the same rule-set shape as today's draft, and
     `content_digest` comes from the existing `Digest()`;
   - each revision is stored immutably.
2. **Recorded validation.**
   - reuses `policyver.Service.Validate`;
   - stored with the revision, `content_digest` and the
     environment-snapshot digest (below).
3. **Deterministic historical replay over a frozen input set** (below).
4. **Audit** of every mutation, in the same database transaction as the
   mutation (below).
5. **One application service** (`internal/change`). The handlers call it,
   and the console uses the same endpoints. No handler holds workflow logic.

**Out (unavailable, not stubbed into behaviour):**
- approval, approval request and revocation (slice B);
- operator management and the `approver` role;
- activation and execution, CoA dispatch, stop and delegated-token
  issuance;
- MCP;
- any vendor or driver framework;
- ADR-0009 qualification and topology;
- Culvert and packaging work.

The state machine exposes only `draft → validated → simulated`, plus
`cancelled`. Later states are not in the slice-A schema's CHECK
constraint, so a slice-A database cannot hold them.

## Correction 1: replay runs on a frozen input set, not a cutoff time

A cutoff timestamp does not freeze the inputs, because the rows it selects
can change: `RecentCases` reads the session's *current* override from the
mutable session row, and switch segment maps can be edited. So a simulation
persists either the exact inputs or immutable references plus digests:

| Input | Stored as | Why |
|---|---|---|
| Recorded decision inputs | Decision id (`auth_decisions` rows are append-only) **and** a digest of the inputs JSON | Immutable reference; the digest detects tampering or migration drift |
| Session operator override at simulation time | **Copied value** per row | Comes from a mutable session row |
| Active policy | `policy_versions.id`, number and `digest` (immutable rows) | The baseline the replay compares against |
| Environment: switch inventory and segment→VLAN/role maps | **Copied canonical content** plus its digest | Mutable tables |
| Input catalog (fields, freshness limits) | Catalog version plus digest | Defines what "missing/stale" means |
| CR content | Revision and `content_digest` | What is being simulated |

- **Input-set digest.** The digest is SHA-256 over the JCS canonical form of
  all of the above. It identifies the simulation's inputs.
- **Reproduction.** Re-running a stored simulation against its own frozen
  input set must reproduce byte-identical canonical output. Rows are ordered
  by session id, and timestamps are excluded from the output digest. This
  is an acceptance test.
- **Refresh.** Refreshing the inputs creates a **new** simulation with a
  new input-set digest. A simulation is never updated in place.
- **Missing, stale and coverage.**
  - Each row records which inputs were missing or older than the catalog's
    freshness limits.
  - The summary records coverage: sessions replayed, sessions skipped and
    why, and the window.
  - A simulation whose environment or active-policy digest no longer
    matches the current state is shown as **stale** (drift), with what
    drifted.
- **Labelling.** Every simulation is labelled "historical replay of
  recorded inputs: a prediction, not live-network verification". It is
  never presented as a measured outcome, and it says nothing about switch
  state or endpoint reachability.

## Correction 2: bind to the active-policy version, with no manufactured `all` holder

- There are no activation records yet. The slice does not invent an `all`
  activation holder and does not write a `replace` supersession.
- A proposal binds to the **existing immutable active policy version**
  (`policy_versions.id` and `digest`) that it was based on. That binding is
  its `base_version`.
- If the active version moves, for example through a legacy publish, the
  CR's validation and simulations are stale, and the CR shows the drift.
  Nothing is invalidated silently.
- **Later migration (slice C), to be defined explicitly then:**
  - The active version becomes the initial `all` activation record.
  - Existing CRs keep their `base_version` binding.
  - Any slice-B approval stays bound to its review-only plan. Activation
    support must **never** make a review-only approval executable;
    executing will require a new approval over the then-defined execution
    plan.

## Correction 3: review-only labelling

See "Review-only, stated plainly" above. In the API, records carry
`workflow: review_only`, and the console shows it on every CR page.

## Correction 4: exact bindings, explicit conflicts, durable audit

- **Exact bindings.** Every operation that depends on a revision or digest
  takes it as input and fails explicitly when it does not match:
  - edit needs `If-Match` on the revision;
  - validate and simulate run on an explicit revision;
  - a mismatch returns `409` with the current value.
  - Slice B's approval will bind the exact revision, `content_digest`,
    validation, simulation input-set digest and plan digest (ADR-0008 §4).
- **Durable audit.** Today `audit.Recorder` is best-effort: a failed write
  is logged, and the action still succeeds. For change requests, each
  mutation and its audit record are written **in the same database
  transaction**. If the audit write fails, the mutation fails. A reliable
  outbox is the alternative if audit moves to an external sink. Acceptance
  test: a forced audit failure leaves no mutation behind.
- **Threat model with the endpoints.** Each milestone that adds endpoints
  updates `docs/security/threat-model.md` and the trust-boundary and
  attack-surface notes in the same PR. This is no longer deferred to a
  final milestone.

## Design

**Packages:**
- **`internal/change`** (new): domain types (`ChangeRequest`, `Revision`,
  `Validation`, `Simulation`, `InputSet`) and the service. It depends on
  `policyver` (validate, replay), a repository interface, and the
  transactional audit writer.
- **`internal/storage/postgres`**: migration `0006_change_requests` and the
  repository.
- **`internal/server`**: handlers that parse, authorize the route and call
  the service. Each implemented operation moves from the proposed contract
  into `api/openapi/openapi.yaml` in the same change
  (`TestOpenAPICoversAllRoutes`).
- **`web/ui`**: a list and a detail page (revision, validation,
  simulation, staleness, coverage), using the same API, with the
  review-only label.

**Tables** (append-mostly; revisions, validations and simulations are
immutable once written):
- `change_requests`: id, title, intent, state, current revision,
  `base_version_id`/`base_version_digest`, requested_by, workflow, and
  timestamps.
- `change_request_revisions`: CR id, revision, content, `content_digest`,
  author and time.
- `change_request_validations`: revision, `content_digest`, environment
  digest and result.
- `change_request_input_sets`: the frozen inputs and their digest.
- `change_request_simulations`: revision, input set, rows, summary,
  coverage and output digest.

Every state change is a compare-and-set on `(state, revision)` under a row
lock (ADR-0008 §2), so concurrent edit, cancel, validate and simulate have
one winner.

**Authorization.** Read with `network:read`; create, edit, validate,
simulate and cancel with `policy:write`. These are the existing
permissions, and there is no new role in slice A.

## Proposed decisions

- **Slice A depends on none of O1, O5 or operator management.**
- **CR vs. `/policy-draft`** still applies: `/policy-draft` is unchanged,
  and the console gains "Propose as change request", which copies the
  current draft into a new CR.
- **Slice B** needs:
  - O1 (no self-approval through a delegate);
  - O5 (expiry and step-up);
  - minimal operator management. The recommendations in the decision
    table stand; they are not accepted.

## Milestones (slice A; each one a PR with its own tests)

1. **Schema and repository.**
   - Migration `0006` and repository CRUD.
   - Immutable revision rows and compare-and-set transitions.
   - The transactional audit writer.
   - Repository tests on PostgreSQL: revision conflict, concurrent
     transitions, and audit atomicity.
2. **Service, validation and API.**
   - Create, edit, cancel and validate, with handlers and OpenAPI.
   - Revision conflicts and immutability of terminal states.
   - Threat-model update for these endpoints.
3. **Frozen-input replay, API and console.**
   - Input-set capture, simulation, reproduction from the stored input set,
     and drift/stale marking.
   - The console pages and a walkthrough script.
   - Threat-model update.

Slice B (approval) and slice C (activation) are separate later steps, each
with its own review.

## Acceptance (slice A)

- **Revision conflicts.**
  - A stale `If-Match` returns `409` with the current revision.
  - Concurrent edit, cancel and simulate on one CR have exactly one winner.
- **Immutable replay inputs.**
  - Changing a session override, a segment map or the active version after
    a simulation does not change that stored simulation.
  - Re-running it from its own input set gives byte-identical canonical
    output.
- **Drift and staleness.**
  - After a legacy publish or a segment-map change, the CR's validation and
    simulation show as stale, naming what drifted.
  - Refreshing creates a new simulation.
  - Missing and stale inputs and the coverage are recorded.
- **Audit durability.**
  - Every mutation has exactly one audit record, with actor, CR id,
    revision and digests.
  - A forced audit failure rolls back the mutation.
- **Shared service.** API handlers and console actions go through
  `internal/change`. A test asserts that the handlers contain no state
  transitions.
- **No side effects.** No change-request endpoint changes the active
  policy, a session, a CoA or a switch. Tested by comparing
  `policy_activations`, `radius_sessions`, `coa_requests` and `switches`
  before and after exercising every CR route, and by a static check that
  `internal/change` does not import the publish, CoA or switch packages.
- **Labelling.**
  - API records and console pages say "review-only".
  - Simulations say "historical replay … not live-network verification".

## Risks

- **Two drafts (the global draft and CRs) can confuse operators.**
  Mitigation: CRs are labelled review-only proposals.
- **A reviewed CR can be mistaken for authorization, while legacy publish
  still bypasses it.** Mitigation: the review-only label everywhere. The
  convergence requirement is recorded above for the mandatory-approval
  step.
- **Scope creep into approval or activation.** Mitigation: there are no
  approval, execution or dispatch routes, the state CHECK constraint
  excludes later states, and the import check above enforces it.
