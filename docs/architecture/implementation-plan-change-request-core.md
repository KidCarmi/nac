# Implementation plan: change-request core (draft → validate → replay → approve → audit)

**Status: proposed plan for the owner's review. Nothing here is
implemented or authorized.** It answers the owner's request on PR #2
(2026-11-13). The plan is the smallest slice of ADR-0008 that delivers a
persisted, reviewable and approvable change request through one shared
application service. **Activation and network dispatch stay disabled**
until their contracts and invariants are accepted and implemented.

## Scope

**In the slice:**
1. **Persisted change requests with revisions.** Create, read, list, edit
   with `If-Match: <revision>`, and cancel. Content is the same rule-set
   shape as today's draft, with `content_digest` from the existing
   `Digest()`.
2. **Recorded validation.** It reuses `policyver.Service.Validate`,
   stored with the content digest and an environment fingerprint (switch
   inventory, segment maps, input catalog).
3. **Deterministic simulation (historical replay).** It reuses
   `policyver.Service.Preview`'s replay of the latest recorded inputs per
   session. The result is labelled a prediction and never a measured
   outcome.
4. **Human approval request and approval.** The plan is resolved,
   digest-bound and human-only, with step-up and expiry. Approvals are
   invalidated before use exactly as ADR-0008 §4 states.
5. **Audit.** Every mutation and every approval state change is recorded
   through `audit.Recorder`, with actor and bindings.
6. **One application service** (`internal/change`). The HTTP handlers call
   it, and the console uses those same endpoints. A future MCP server would
   be another API client. No handler holds workflow logic.

**Explicitly out of the slice (disabled, not stubbed into behavior):**
- **No activation stage.** There is no `executions` operation, and an
  approval cannot publish or change the active policy.
- **No CoA dispatch**, no generation fence, and no stop operation.
- **No delegated (AI) tokens and no MCP server.** The principal model
  carries `kind`, and the service rejects delegated principals for
  approval (tested), but no delegated identities are issued.
- **No change to the existing publish and restore paths.** The current
  `POST /policy-draft/publish` and `/policy-versions/{n}/restore` behave as
  today. Because the slice changes no existing v1 operation, it does
  **not** need O3.
- No qualification records, eligibility or topology (ADR-0009); no
  Culvert, build-system or packaging work.

## Why this cut

- The slice delivers the review loop (what changes, what it would do,
  who approved it) without moving any network state. Its failure modes are
  bounded to its own tables.
- It reuses three proven pieces: validation, preview replay and audit. The
  new code is persistence, state transitions, binding and authorization.
- Keeping publish unchanged avoids a v1 compatibility break (O3) until
  activation exists, and an approval that can't execute can't be misused.

## Design

**Packages:**
- `internal/change` (new): the domain types (`ChangeRequest`, `Revision`,
  `Validation`, `Simulation`, `ApprovalRequest`, `Approval`) and the
  service. It depends on `policyver` (validate, replay), `audit`, `auth`,
  and a repository interface.
- `internal/storage/postgres`: migrations `0006_change_requests` and the
  repository implementation.
- `internal/server`: handlers that parse, authorize the route, and call the
  service. Each implemented operation moves from
  `api/openapi/proposed/change-workflow.yaml` into `api/openapi/openapi.yaml`
  in the same change (`TestOpenAPICoversAllRoutes`).
- `web/ui`: a change-request list and detail page, with validation,
  simulation, the resolved plan and the approval dialog. It calls the same
  API.

**Tables (append-mostly):**
- `change_requests`: id, title, intent, state, revision, base_version,
  content, content_digest, requested_by, follows_cr, timestamps
- `change_request_validations`
- `change_request_simulations`: rows plus summary
- `approval_requests`: resolved plan JSON and `plan_digest`
- `approvals`: bindings, state, `expires_at` and `invalid_reason`

Every state change is a compare-and-set on `(state, revision)` under a row
lock (ADR-0008 §2), so edit, cancel and approve races have one winner.

**State machine in the slice:** draft → validated → simulated →
approval_pending → approved, plus cancelled. `executing` and later states
exist in the type but are unreachable, because no operation enters them.
Content edits return the CR to `draft` and invalidate approvals.

**The resolved plan without activation.** In the slice, the plan states the
intended scope (`all` only: cohort activation is not built) and a
`replace` of the current `all` holder with its expected revision, as
ADR-0008 §5 requires. It has no apply targets. `plan_digest` is SHA-256
over the JCS canonical JSON. The approval binds it, so when activation
arrives, existing approvals can't silently gain an execution path; they
will need the then-defined plan.

**Determinism of the replay.** The same content, the same recorded inputs
and the same active version produce the same rows and the same summary.
Rows are ordered by session id, and timestamps are excluded from the
digest. The simulation stores the inputs' cut-off time, so a later
re-simulation is comparable.

**Approval rules implemented** (ADR-0008 §4, the parts that don't need
execution):
- only `operator` principals with `policy:approve`; delegated principals
  are rejected before roles are checked
- step-up with the current password
- every bound field equal to current state, or `409`
- the approver is not `requested_by`; self-approval through a delegate is
  O1
- expiry (O5)
- invalidation before use on content change, a scope-snapshot change
  (here: the `all` activation's revision moved), environment drift,
  approver disabled or permission lost, or revocation
- revocation by a human with `policy:approve`

**Audit events:** `change.create|edit|cancel`, `change.validate`,
`change.simulate`, `change.approval_request`,
`change.approval.create|revoke|invalidate`. Each records the actor, CR id,
revision, digests and the outcome.

## Proposed decisions this slice depends on

Only these. The others stay open and **do not block this slice**.

| ID | Needed because | Recommended choice for the slice |
|---|---|---|
| **O1** | Approval needs a separation-of-duties rule now | **No self-approval through a delegate**, with an explicit, recorded single-admin exception setting (off by default). It is moot for humans in this slice, because no delegates are issued; the setting is still stored and tested |
| **O5** | Approval needs an expiry and a step-up | **24 h expiry and a password step-up** for the slice. WebAuthn before any external customer pilot (unchanged) |
| **Operator management** (ADR-0008 §6, minimal) | A real approval needs a second human account | **In the slice:** admin-only create, disable and role assignment, with the new `approver` role and `policy:approve`, all audited |
| **New: CR vs. `/policy-draft`** | The console has one working draft today | **Keep `/policy-draft` unchanged.** The console gains "Propose as change request", which copies the draft into a new CR. Replacing the global draft waits for activation |

**Not needed by this slice:**
- O2 (MCP transport)
- O3 (publish compatibility; publish is unchanged)
- O4 (cohorts; scope is `all` only)
- Q1–Q5 (enforcement strategy)
- N1–N9 (evidence, scope overlap rules beyond `all`, stop, qualification
  validity, execution duration, evidence age)

They become dependencies of the activation and dispatch slices.

## Milestones (each one a PR with its own tests)

1. **Schema and repository:** migration `0006`, repository CRUD, the
   compare-and-set transitions, and repository tests on PostgreSQL.
2. **Service, CR and validation:** create, edit, cancel and validate;
   revision conflicts; immutability of terminal states; audit events.
3. **Deterministic replay:** the simulation over recorded inputs, a
   determinism test (the same inputs give byte-identical canonical
   output), and stale marking on drift.
4. **Approval:** the resolved plan and its JCS digest; the approval request
   and approval; invalidation; revocation; human-only and step-up checks;
   operator management and the `approver` role.
5. **API, OpenAPI and console:** the handlers; each implemented operation
   moved into `openapi.yaml`; the console pages; a console walkthrough
   script.
6. **Security documentation:** threat model, trust boundaries and the
   attack-surface update for the new endpoints and the approver role.

## Acceptance tests for the slice

- A delegated principal (synthetic, in tests) cannot approve, whatever its
  scopes.
- An approval with any bound field changed (content, scope revision,
  validation, simulation or plan digest) gets `409`.
- An approval is invalidated by a content edit, by a policy publish
  through the existing path (the `all` revision moved), by the approver
  being disabled, and by expiry.
- Edit, cancel and approve racing on one CR have exactly one winner.
- Replay determinism, and simulations marked stale after drift.
- No route reachable in the slice changes the active policy, a session or
  a switch. Tested by asserting that no activation or CoA write happens
  on any change-request path.
- Every mutation produces exactly one audit event, with actor and
  bindings.

## Risks

- **Two drafts, the global one and CRs, can confuse operators.**
  Mitigation: the console labels CRs as proposals, and approval states
  plainly that it "does not publish yet".
- **Approvals that cannot execute** might be mistaken for authorization.
  Mitigation: the approval record and the UI say "approved for review:
  activation not available in this version". Approvals expire in 24 h.
- **Scope creep into activation.** Mitigation: no `executions` route, no
  activation write in the service, and the acceptance test above.
