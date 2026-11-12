# 0008. AI-operated change workflow: change requests, digest-bound human approval, delegated identities, MCP

- **Status:** Proposed (contract for the owner-side architecture/security review; no code yet). Reconciled 2026-11-10 with the proposed network-device driver ADR (PR #4): approval invalidation on configuration drift, dispatch-time authorization and generation fencing. Not accepted; the owner retains the review.
- **Date:** 2026-11-07
- **Deciders:** OPUS (backend), owner-side assistant (architecture/security review), repository owner. The GitHub `@codex review` bot is an additional automated reviewer.

## Context

The owner set the next increment's priorities:
1. usable network enforcement
2. a complete, safe policy workflow
3. operation through the customer's own AI via MCP

The customer's AI is the primary operator. Humans keep full equivalent
capability in the console and act as approvers and fallback.

`pilot-rc10` (`b39649f`) stays the tested baseline. What it already has:

| Workflow step | `pilot-rc10` | Gap |
|---|---|---|
| Discover capabilities | Input catalog (`GET /policy-inputs`), switch status facts | No platform-capability model; Arista EOS is implied everywhere |
| Draft | Rule CRUD on **one global workspace** (`/policies`, `/policy-draft`) | No per-change draft, no persisted workflow, no owner of a change |
| Validate | Draft validation against switch VLAN maps and the input catalog | Not recorded; no environment fingerprint; no drift revalidation |
| Dry-run | `GET /policy-draft/preview`: replay over recorded inputs, labelled a prediction | Not recorded; not bound to anything |
| Approval | **None.** Publish and restore need `policy:write`, the same permission that edits the draft, plus `expected_digest` | Everything |
| Staged publication | Atomic, global activation; applies at next authentication; CoA is a separate manual action | No plan, no cohort, no stop-on-failure |
| Observe outcome | CoA state machine (requested → sent → acked → observed), eAPI observation, `endpoint_reachability: not_observed` | Nothing ties a publication to its outcomes |
| Identities | One bootstrap admin; roles admin/operator/viewer; per-operator token revocation (ADR-0007) | No operator management, no delegated (AI) identities, no scopes |

This ADR fixes the contract that the backend, the console, the MCP server
and the owner-side review work against. It is deliberately the safety core: approval
and identity rules must exist before an AI can drive changes.

Out of scope for this increment:
- Docker, build, distribution, OVA and lifecycle work
- Culvert integration
- a proprietary model or chat product

Separate ADRs will cover the device model (identity strength, shared-port
dependencies, platform capabilities), MAB and Aruba. They are listed at the
end.

## Decision

### 1. One workflow, one set of services, for every operator

Change requests (CRs) carry every policy change. The console, the
`/api/v1` HTTP API and the MCP server all call the same backend services. No
path publishes, restores or executes outside a CR.

All authorization, validation, approval and execution rules are enforced in
nacd. Clients, including skills and AI models, are never trusted for them.

NAC decisions on the RADIUS path (`/radius/*`, ADR-0006) do not depend on
this workflow or on any AI. Enforcement continues unchanged when no AI is
connected.

### 2. Change request: a persisted, owned unit of work

A CR is stored in PostgreSQL. It survives a chat ending, a client
disconnect, an operator change and a restart. It has:

- `id`, `title`, `intent` (free text from the requester, stored as data, never interpreted).
- `kind`: `policy_change` in this increment. `session_action` (quarantine or restore proposed by an AI) is reserved; until then those actions stay human-only, as in rc10.
- `base_version`: the policy version the content was drafted from (what the
  console diffs against). It is **not** an approval binding: a scope can
  resolve to several versions at once, so approvals bind a per-unit scope
  snapshot instead (section 4, R2).
- `content`: the full proposed rule set and default action, the same shape as today's draft. `content_digest` uses the existing `Digest()`.
- `revision`: an integer. Every content change increments it. Writes carry `If-Match: <revision>` and get `409` on conflict, which is how concurrent edits are detected.
- `requested_by`: the principal that created it. `on_behalf_of` is set when that principal is a delegated identity.
- `state`, with these transitions, all server-side:

  ```
  draft ──validate──▶ validated ──simulate──▶ simulated ──request──▶ approval_pending
    ▲          │                                                       │ approve (human)
    └──edit────┴───────────── any content change ◀─────────────────────┤
                                                                       ▼
  cancelled ◀── cancel (only before an execution exists)       approved ──execute──▶ executing
                                                                                        │
                    observed ◀── observe ── applied ◀───────────────────────────────────┤
                    stopped  ◀── stop (section 5) ──────────────────────────────────────┤
                    failed   ◀──────────────────────────────────────────────────────────┘
  ```

- **Mutable states.** Content can change only in `draft`, `validated`,
  `simulated`, `approval_pending` and `approved`. Every content change
  returns the CR to `draft`. Validations and simulations stay recorded but
  become `stale`, because their `content_digest` no longer matches. Every
  approval is invalidated.
- **Immutable from execution on (R5).** In `executing`, `applied`,
  `observed`, `stopped`, `failed` and `cancelled`, a content write, a new
  approval request and a new execution return `409`
  (`/problems/cr-immutable`). A change after execution started is a **new
  CR**, which may reference the old one (`follows_cr`). The execution keeps
  an immutable snapshot of what it runs (section 5).
- **One atomic winner.** Edit, cancel and execute each change the CR's
  `state` with a compare-and-set on `(state, revision)` under the CR row
  lock. When they race, exactly one wins; the others get `409` and change
  nothing. Cancel after an execution exists returns `409`: stopping is a
  separate operation on the execution.
- **Ownership (R5).** A delegated identity may edit, cancel, request
  approval for and execute only CRs whose `requested_by` is itself, and stop
  only executions it started. A human with `policy:write` may do any of
  these on any CR. No principal can approve its own CR (section 4).

Today's global draft (`/policy-draft`) becomes the console's working CR.
Existing endpoints stay readable. How their write paths change is decision 8.

### 3. Validation and simulation are recorded evidence, kept apart from lab qualification

- `POST /change-requests/{id}/validations` records a **validation result**:
  - static checks (rules, inputs, VLAN mapping)
  - errors and warnings
  - the `content_digest` and the scope snapshot it ran against
  - an `environment_fingerprint`: a hash of the switch inventory, segment-VLAN maps and the input catalog
- `POST /change-requests/{id}/simulations` records a **simulation result**:
  - the existing preview, which replays recorded inputs under the version each session currently resolves to, and under the CR
  - per-session predicted changes, insufficient-evidence and stale-input cases
  - the operator-override precedence that preview already applies

  It is labelled `kind: historical_replay` with the existing "prediction" label.
- **Neither is lab qualification.** Live lab results stay in ASTRA's
  evidence. The API never reports a simulation as a measured outcome.
- **Environment drift** makes a result `stale`. Drift means a later
  `environment_fingerprint` differs, or the scope snapshot (section 4)
  changed. The CR then needs revalidation before it can be approved or
  executed.

### 4. Approval: created by an authenticated human, bound to exactly what was reviewed

An approval is a stored record. It is never a boolean that a client sends.

- **Who may approve.** A new permission, `policy:approve`, held by human
  roles: `admin` and a new `approver` role. A delegated identity can never
  hold it. The check rejects principals of kind `delegated` *before*
  looking at roles or scopes, so no scope can grant it by accident.
- **How.** `POST /change-requests/{id}/approvals` from a human session. The
  request must carry:
  - `current_password`: a step-up re-authentication, as for a password change
  - the `content_digest`, `scope_snapshot_digest`, `validation_id`, `simulation_id` and `plan_digest` the human reviewed

  The server checks each one against current state. Any mismatch returns
  `409`, so a human can never approve something other than what they saw.
- **What the human approves is the resolved plan (R3).** At approval-request
  time the server resolves the client's plan input into an immutable
  **resolved plan** (section 5), stores it, and returns it with its
  `plan_digest`. The console shows that resolved plan, not the input, before
  the human approves.
- **Binding.** An approval binds `{cr_id, cr_revision, content_digest,
  validation_id, simulation_id, environment_fingerprint,
  scope_snapshot_digest, plan_digest}`. The resolved plan already contains
  the scope and every per-target binding, so `plan_digest` covers them. The
  approval has an expiry: `expires_at`, default 24 h, configurable.
- **Scope snapshot (R2).** For every scope unit the plan touches, the
  snapshot records the activation that currently resolves it (`activation_id`,
  `revision`, `version`). It also lists every current narrower activation
  inside the scope. Its digest replaces the old singular `base_version`
  binding, which was ambiguous for a scope that resolves to several versions.
- **Invalidation before use.** Until it is consumed, an approval becomes
  `invalid` when:
  - the CR content changes
  - the scope snapshot changes: an activation for any touched unit is
    created, revised or superseded
  - the environment fingerprint changes. The fingerprint includes each
    target switch's **configuration fingerprint** (role and ACL contents,
    implicit action, direction, host mode, fallback settings; ADR-0009 §7),
    the capability record and the driver/recipe revision. A role that keeps
    its name but changes its rules invalidates the approval.
  - a target's desired-state generation moves, for example because an
    operator quarantined a session the plan would restore
  - it expires or is revoked
  - the approver loses `policy:approve` or is disabled

  Execution re-checks every condition when it starts. It does not rely on
  cached approval state.
- **Single use, owned by one execution (R1).** The execution start
  transaction (section 5) consumes the approval and records the execution
  that owns it. Reusing it, or using it for another CR, plan or scope,
  returns `409`. **After consumption, the approval applies only to its
  owning execution**. Its dispatch checks follow that execution's expected
  transition rule (section 5), not the "snapshot unchanged" rule above, so
  the execution's own activation is never mistaken for drift, though an
  unrelated activation still is. **Consumption does not freeze the
  authority:** the approver, the approval, the executing principal, expiry
  and every bound piece of evidence are re-checked before the activation
  commit and before each first send (section 5, "Continuing authority after
  consumption").
- **Separation of duties.** The approver must not be `requested_by`.
  Whether the human named in `on_behalf_of` may approve their own AI's CR
  is open decision **O1**. Single-admin installs need an answer.
- **Audit.** `change.approval.create|revoke|invalidate`, with actor and
  bindings.

### 5. Scoped activation and staged execution

**A true canary needs cohort-aware policy selection, not just a targeted
CoA.** If a version were activated globally and only some sessions were
re-authenticated, every other endpoint would get the new policy at its next
authentication anyway. So staged publication is defined on the decision
path.

**Activation records (R2).**
- An activation has an `activation_id`, a `revision`, a `version`, a
  `state` (`current` or `superseded`) and a `scope` made of **scope units**:
  - `all`: exactly one current `all` activation always exists (today's
    behavior)
  - a switch unit: `(switch_id)`
  - a port unit: `(switch_id, nas_port_id)`

  A cohort activation holds one or more units of one kind. Revisions are
  append-only (ADR-0006): changing an activation's units writes a new
  revision, and the history stays readable.
- **Resolution is deterministic.** For each authentication, `radiusauth`
  takes the current activation that holds the request's port unit, else
  its switch unit, else `all`.
- **No equal-specificity overlap.** At most one current activation holds
  any given port unit, and at most one holds any switch unit. Exactly one
  current activation holds `all`. There is no "newest wins".
- **Every supersession is a typed plan input (R2 residual).** The plan
  input's `supersessions` list names each current activation the plan
  touches, with its **expected revision**, and one action:
  - **`replace`**: a same-specificity holder whose units are **all** inside
    the new scope is superseded whole. Every publication to `all` replaces
    the current `all` activation this way, so `all` → `all` is a
    `replace` of the named holder and revision.
  - **`take_units`**: a same-specificity cohort gives up **only the listed
    units**. It gets a new revision without them, and its other units keep
    their version. Example: take port P from a cohort holding P, Q and R;
    Q and R are unchanged.
  - **`retire`** or **`keep`**: for a narrower activation inside the new
    scope (below).

  Before approval, the server checks that every unit of the new scope held
  at the same specificity is covered by a `replace` or `take_units` entry
  for its current holder and revision. A unit with an unnamed holder, a
  wrong revision, or a `take_units` list outside the new scope is rejected
  (`422`, `/problems/scope-overlap`). The resolved plan binds every
  supersession with the units it moves, so the approval covers it.
- **Promotion and rollback name exactly what they retire.** A plan that
  widens a scope (port → switch → `all`) or rolls one back does not retire
  any narrower activation implicitly.
  - The plan's `supersessions` list each current narrower activation inside
    the new scope as `retire` or `keep`. The console pre-fills `retire`
    for narrower activations of the version being promoted, or rolled back
    from.
  - The resolved plan shows the **post-change resolution of every unit**:
    which units the retirements move, and which narrower activations keep
    shadowing the new one.
  - An unlisted narrower activation is kept. A new, unrelated canary is
    therefore never retired by accident.
  - Example: port P is pinned to canary v2, and v2 is promoted to `all`
    with P retired. Rolling `all` back to v1 later resolves P to v1. Had P
    been kept, both plans would show that P stays on v2.
- **Atomic.** The new activation, every `replace`, `take_units` and
  `retire` are written in one transaction under the activation lock. That
  transaction succeeds only if every touched activation still has its
  expected revision. On any mismatch nothing is written and the execution
  stops (`external_change`). Two concurrent scope changes cannot both
  apply. A concurrent holder created after approval is never superseded,
  because it isn't named; the plan stops instead.
- The decision record stores the resolved version and the activation (id
  and revision) that supplied it. The console and API can then show, per
  session, which version applied and why.
- An endpoint outside the cohort keeps the previous version, including when
  it re-authenticates. This is the out-of-cohort property the acceptance
  tests check.
- Operator quarantine overrides keep precedence over every version and
  every scope, as today.

**Scope is part of what is approved.** The resolved plan, and therefore the
approval, carries the exact scope units, the retirements and the
post-change resolution. To widen a canary, a new plan targets the wider
scope, and that needs its own approval. To roll back, a new plan restores
the previous version for the stated units only.

**The resolved plan (R3).** The client sends a **plan input**: scope,
stages, apply targets (session ids), retire/keep choices, deadlines. The
server resolves it into an immutable **resolved plan** when approval is
requested, and stores it durably.
- **Activation part:** the new activation's units and version, the
  retirements and partial revisions, the scope snapshot, and the
  post-change resolution of every touched unit.
- **One record per apply target.** Each one carries the target's session
  id and **incarnation** (session row, Acct-Session-Id and last
  authentication). It also carries:
  - switch, port and MAC
  - the identity and the assignment: profile and version, segment, VLAN
    or role
  - the recipe id and revision
  - the configuration fingerprint and its evidence ids
  - the qualification record ids (ADR-0009 §2)
  - the port's **topology assessment**, with its basis, evidence id and
    validity. A session count never establishes `single_endpoint`
    (ADR-0009 §2)
  - the desired-state generation
  - the selected strategy and its limitations
  - the impact: shared-port dependents, the expected interruption, and
    whether the address changes
- **Canonical digest.** `plan_digest` is SHA-256 over the RFC 8785 (JCS)
  canonical JSON of the whole resolved plan, `plan_digest` excluded. It
  includes the CR id and revision, `content_digest`, and the validation
  and simulation ids.
- **The executor runs only the stored resolved plan.** It never recomputes
  an assignment, a recipe or a target under an unchanged digest. If live
  state no longer matches a target's binding, that target is `superseded`
  or the execution stops; it is never silently re-planned.
- **Grammar, checked before approval** (`422` otherwise):
  - stages are exactly `activate`, optionally followed by `apply`, then
    optionally by `observe`
  - only `apply` has targets
  - `apply` has 1 to `max_targets` targets (an installation limit), each
    session at most once, all inside the scope
  - a cohort scope has at least one unit

**Execution start (R1).** `POST /change-requests/{id}/executions` takes
`{approval_id}` and an `Idempotency-Key` header. It returns `202` and an
execution record. One transaction does all of this, or nothing:
1. It checks that the approval is `valid`, its bindings match current
   state, and the CR is `approved`.
2. It creates the execution with an **immutable copy of the resolved plan**,
   the **pre-activation snapshot** (the approved scope snapshot), and its
   **execution authority**: the executing principal, the approval, and
   `authority_expires_at` (the earlier of the approval expiry and a maximum
   execution duration).
3. It marks the approval `consumed` by this execution, and moves the CR to
   `executing`.

The `Idempotency-Key` is unique per principal. The same key with the same
CR, approval and body returns the same execution. The same key with a
different CR, approval or body returns `409`
(`/problems/idempotency-key-reused`).

**The execution's own transition is the only accepted state change (R1).**
When the `activate` stage commits, the execution records its **expected
post-activation state**: the activation id and revision it wrote, and the
revisions of everything it retired or revised. From then on, every
dispatch check requires the live resolution of each scope unit to equal
that expected state; before the commit, it must equal the pre-activation
snapshot. Any other change stops further dispatch with reason
`external_activation`: an unrelated activation, a retirement or a rollback
between stages. Version checks are **not** skipped after activation. They
compare against what this execution itself committed.

**Continuing authority after consumption (R1 residual).** Consuming the
approval moves the "snapshot unchanged" rule to the expected-transition
rule above. **It does not freeze the authority or the evidence.** Before
the activation commit, and before each target's first send, the executor
re-checks that:
- **the approver** is still enabled and still holds `policy:approve`
- **the approval** has not been revoked. Revoking a consumed approval
  withdraws its execution's authority.
- **the executing principal** is enabled, its token is not revoked, and a
  delegate's human is still enabled
- **the execution authority** has not expired (`authority_expires_at`)
- **the evidence** bound in the resolved plan is still current: each
  qualification record is unexpired and not superseded, and each target's
  configuration evidence is unexpired, with an unchanged fingerprint.
  **An identical fingerprint is not enough when its evidence expired**
  (ADR-0009 §2, §7).

Any failure **withdraws the execution's authority**, a security
cancellation recorded with its reason (`approver_disabled`,
`approver_lost_permission`, `approval_revoked`, `principal_revoked`,
`authority_expired`, `evidence_expired`, `evidence_superseded`).
- After a withdrawal, nothing new is activated or sent. The execution's
  own already-committed activation stays in force and is reported; that is
  the only exception, as above.
- **A withdrawal also ends retries at once.** A request already sent is not
  retransmitted, and it ends `uncertain` unless an answer arrived. This is
  stricter than an operator Stop (N5): without authority, NAC sends
  nothing more.
- **A withdrawal is final.** An operator Stop afterwards only records the
  stop; it cannot resume retries or reinstate authority.

**Crash and restart (R1).** The activation row carries `created_by_execution`
and the stage, unique per execution. After a restart the executor reads
the execution ledger:
- an activation that already exists means the stage is done, and it is not
  republished
- the approval was consumed in the start transaction, so it cannot be
  consumed twice
- a repeated request with the same key returns the same execution

**Stages.** The resolved plan, identified by `plan_digest`, runs its stages in
this order:

1. **activate(scope)**: write the CR content as a new immutable version.
   This reuses `PublishWorkspace`, with base-version and digest checks under
   the activation lock. It records an activation for the approved scope,
   which takes effect at the next authentication *of endpoints in that
   scope*.
2. **apply** (optional): re-authenticate sessions *inside the scope* now,
   through the existing CoA state machine. Targets outside the scope are
   rejected when the plan is validated. Before execution the plan shows,
   per target:
   - the predicted segment and VLAN, from the simulation
   - shared-port dependents: other active sessions on the same port
   - the documented vEOS-lab interruption (an ACKed, VLAN-bearing CoA)

   Stop-on-failure is the default.
3. **observe**: wait for re-authentication and eAPI observation, up to a
   deadline set in the plan.
4. **promote** (a separate, later plan with its own approval): activate the
   same version for a wider scope or for `all`.

Each target records these outcomes separately:
- `not_sent`: never sent, with the reason (`stopped`, `superseded`,
  `external_activation`, `authority_expired`)
- `requested`
- `acknowledged`: CoA-ACK, NAK or timeout
- `observed`: on the VLAN path, re-authentication returned the requested
  VLAN and the switch reports it. On the Aruba role path there is no
  re-authentication and no NAC observation, so it is `unavailable` and
  `acked` is final.
- `verified`: an independent endpoint check. NAC never fills this in. It stays `not_observed`, as in ADR-0006.
- `superseded` or `uncertain`, when the generation fence or a lost ACK
  applies

Retries are safe:
- a repeated `Idempotency-Key` returns the same execution
- a stage that already completed is a no-op
- CoA keeps at-most-one-in-flight per session, with byte-identical,
  never-retargeted retries (ADR-0006)

**Dispatch-time authorization and generation fencing.** Authorization when
the plan is created is not enough, because an execution can wait in a
queue.
- Immediately before each target's **first send**, the executor re-checks:
  - the execution's state (not `stopping` or `stopped`) and that its
    authority is active: the continuing-authority checks above,
    including the approver and the bound evidence
  - the principal's permissions and token revocation
  - the expected-transition rule above (section 4 for the approval itself)
  - the target's **desired-state generation** and its override state: a
    newer quarantine supersedes
  - the target's incarnation (the session is current and not replaced)
  - the configuration fingerprint and the driver/recipe revision bound in
    the resolved plan
- It then atomically claims the target with the expected generation.
- **A newer Quarantine supersedes an older queued Restore.** The Restore
  sees a moved generation and is recorded `superseded`, never sent.
  Retries and reconciliation also stop once superseded; they never revive
  an obsolete assignment.
- A single-use approval is consumed once per execution, not once per
  packet. A worker restart does not create a second execution.
- **A lost ACK is an uncertain outcome, not a failure.** Retries resend the
  exact bytes within a bounded window. After a restart or an expired
  window, the executor reconciles first; it does not replay against a
  replacement session.
- No exactly-once network effect is claimed. Partial completion across
  targets is explicit, and no automatic compensation removes a quarantine.
- **Today's pilot:**
  - Operator session actions allow one CoA in flight per session (`409`).
  - A Restore that the policy would turn into a deny is refused (`422`)
    and keeps the quarantine.
  - Retries never re-target.

  The generation fence is a requirement for the CR workflow's queued
  stages. It is not yet implemented.

**An execution never clears an operator quarantine override.** Restoring a
policy version leaves overrides in place, as today. Clearing one is a
separate, explicit session action.

**One execution fence for Stop, authority and activation (R5 residual).**
The execution row is the fence.
- **The activation transaction** locks the execution row (`SELECT … FOR
  UPDATE`). It commits the activation only if, under that lock:
  - the execution is `running`
  - its authority is active (the continuing-authority checks above)
  - `authority_expires_at` has not passed by the database clock
- **Stop and authority withdrawal** take the same row lock. They move the
  execution off `running` with a compare-and-set.
- **So exactly one ordering is observed:**
  - **Stop (or a withdrawal or expiry) wins:** no activation is committed.
    The `activate` stage ends `not_started` and `still_in_force` is empty.
    That includes an activate-only plan: a worker never publishes policy
    just because no CoA was due.
  - **The activation wins:** it is committed, and the Stop that follows
    lists it in `still_in_force`.
- **First sends** take the same lock briefly when they claim a target. A
  Stop or withdrawal that commits first prevents the send.

**Stopping an execution (R5).** `POST /executions/{id}/stop`, with a
reason, may be called by a human with `policy:write` or by the delegated
identity that started the execution. A stop only reduces sends. It works
like this:
- It moves the execution to `stopping` under the execution fence above.
  No activation commits and no target gets a **first send** after that.
- **Pending** targets become `not_sent (stopped)`.
- **Sent** targets still waiting for an answer finish only their in-progress,
  byte-identical retransmissions within the existing bounded window. They
  end as `ack`, `nak`, `timeout` or `uncertain`. The first packet may
  already have acted, so abandoning the retries would only add
  uncertainty (N5). **This applies only while authority stays active.** A
  withdrawal ends retries at once (continuing authority, above).
- **Uncertain** targets are reconciled read-only. Nothing new is sent.
- The execution becomes `stopped` when no request is in flight.

The stop report states, per target, what was and was not sent. It also
states **what remains in force**:
- **A committed activation stays active** for later authentications in its
  scope. The report lists its units and version. Stopping is not a
  rollback.
- **A rollback is a separate CR with its own approval.**
- **Operator overrides are untouched.**

**Acceptance scenarios for the execution contract** (contract tests for any
implementation; the schema examples are in
`api/openapi/proposed/examples/`):

| ID | Scenario | Required outcome |
|---|---|---|
| A-R1-1 | Consume the approval, activate, then apply | The apply proceeds: the execution's own activation is not drift |
| A-R1-2 | Consume the approval, then an unrelated activation lands before apply | No further send; reason `external_activation` |
| A-R1-3 | Consume the approval, then disable the approver (or remove `policy:approve`) before apply | Authority withdrawn (`approver_disabled` / `approver_lost_permission`); no new activation or send; the own committed activation alone stays and is reported |
| A-R1-4 | Consume the approval, then let a bound qualification record or configuration evidence expire (fingerprint unchanged) before apply | Authority withdrawn (`evidence_expired`); no new mutation |
| A-R1-5 | Revoke a consumed approval while a sent request awaits its answer | Retries end at once; the target ends `uncertain` unless an answer arrived |
| A-R1-6 | Operator Stop after a withdrawal | Recorded only; retries are not resumed; authority stays withdrawn |
| A-R1-7 | Crash and restart after the activate stage | No second activation; the approval is not consumed again |
| A-R2-1 | Publish to `all` with a `replace` of the current `all` holder and its revision | Accepted; the holder is superseded |
| A-R2-2 | Publish to `all` without naming the current `all` holder | `422 scope-overlap` before approval |
| A-R2-3 | Take port P from a cohort holding P, Q and R (`take_units` [P]) | P moves to the new activation; the cohort gets a new revision holding Q and R at their old version |
| A-R2-4 | A concurrent holder appears, or the named holder's revision changes, between approval and the activation commit | Nothing is written; the execution stops (`external_change`) |
| A-R2-5 | Promote port → switch → `all`, then roll `all` back | Each plan lists every narrower activation as `retire` or `keep`; the post-change resolution shown at approval is what results |
| A-R5-1 | Stop races an **activate-only** execution; Stop wins | No activation is committed; `activate` is `not_started`; `still_in_force` is empty |
| A-R5-2 | Stop races an activate-only execution; the activation wins | The activation is committed and listed in `still_in_force` |
| A-R5-3 | Stop races activate → apply after the activation committed | No first send after the Stop; the activation is listed in `still_in_force` |
| A-R5-4 | `authority_expires_at` passes, or a withdrawal commits, before the activation | No activation is committed |
| A-R5-5 | Edit or cancel races execute | Exactly one wins; the losers get `409` and change nothing |

### 6. Delegated identities for AI clients

- **What it is.** A *delegated identity* is a principal of kind
  `delegated`, created by a human admin. It names the AI client and the
  human it acts `on_behalf_of`. It holds a fixed scope set and never a role.
- **Scopes in this increment:**
  - `inventory:read`: switches, sessions, devices, capabilities
  - `explain:read`: session explanations and decision traces
  - `change:draft`: create and edit its own CRs
  - `change:simulate`: validate and simulate
  - `change:request`: request approval
  - `change:execute`: execute a CR that a human approved
  - `execution:read`
- **Never available to a delegated identity:**
  - `policy:approve`
  - operator or identity management
  - switch create, update or delete, and secret reveal
  - direct session quarantine, restore or re-authenticate
  - the RADIUS service routes
- **Tokens.** Short-lived bearer tokens with `kind: delegated`, plus the
  revocation version from ADR-0007, extended to delegated principals. A
  human can revoke an identity, or all its tokens, at once. Every request
  re-checks the identity record, its scopes and its human principal: a
  disabled human disables their delegates.
- **Audit.** Every mutation records the delegated actor and `on_behalf_of`.
- **Operator management (minimal).** A real approver needs a human account
  separate from the requesting identity. This increment therefore adds
  human operator management: create, disable, assign a role. It is
  admin-only and audited.

### 7. MCP server: a thin, narrow client of the same API

- **Shape.** `nac-mcp` is a separate process. It holds a delegated token
  and calls `/api/v1`. It has no database access and no switch access, so
  every rule above applies to it unchanged.
- **Transport.** stdio first: the customer runs it next to their own AI
  client. Remote streamable HTTP with OAuth is open decision **O2**.
- **Tools in this increment.** Each maps to one or two API calls:

  | Tool | API |
  |---|---|
  | `get_capabilities` | `GET /capabilities` |
  | `list_switches`, `list_sessions`, `get_session_explanation` | existing read endpoints |
  | `list_devices`, `list_bindings` | existing read endpoints |
  | `create_change_request`, `update_change_request` | `POST /change-requests`, `PUT …/content` |
  | `validate_change_request`, `simulate_change_request` | `POST …/validations`, `POST …/simulations` |
  | `request_approval` | `POST …/approval-requests` (creates a pending request; never an approval) |
  | `execute_approved_change` | `POST …/executions` |
  | `get_change_request`, `get_execution` | read endpoints |

- **Never exposed:** a shell, arbitrary switch CLI, secret reveal,
  approval, or identity management.
- **Untrusted data.** Hostnames, device descriptions, certificate fields,
  RADIUS reply messages and logs all come from the network. Tool results
  return them as quoted data fields, never as instructions. No server-side
  logic makes an authorization or scope decision from such content.
- **Human takeover.** A CR started over MCP shows in the console with its
  validations, simulations, pending approval and execution. A human can
  continue, approve, cancel or execute it from the same persisted state.

### 8. Existing write paths

Today `POST /policy-draft/publish` and `POST /policy-versions/{n}/restore`
change policy with `policy:write` alone. From this increment both require an
`approval_id` that satisfies section 4. Without one they return `409` with
`type: approval-required`. A restore is a CR whose content is the target
version.

This changes the meaning of existing v1 operations. The integration-boundary
rule says that needs `/api/v2`. Open decision **O3** records the proposed
exception and its alternative.

### 9. Skills guide; the server enforces

The first skill is **office with managed PCs, phones and printers**. It ships
as files: instructions, a question list and proposal templates.

It does this:
- inspects capabilities and inventory first
- asks only what is still unknown
- states its assumptions and exceptions
- drives the CR workflow up to `request_approval`

The organization type selects questions and proposals. It never grants
access automatically. Nothing in a skill is a security control.

## Consequences

### Positive
- No path to a policy change without a human approval that is bound to the
  exact content, scope snapshot, validation, simulation, resolved plan and environment the human
  saw.
- An AI's work and a human's work are the same records. Takeover, audit and
  retry need no special cases.
- The MCP server holds no privilege the API would not grant to its delegated
  identity.
- It reuses rc10's validation, preview (now simulation), publication, CoA
  state machine and revocation code.

### Negative / trade-offs
- Every policy change needs a second step, including on single-admin
  installs, depending on O1.
- Changing the meaning of publish and restore is a v1 compatibility break
  (O3).
- Scoped activation changes the decision path: the active version is
  resolved per switch and port. That needs careful tests for resolution
  order, rollback per scope, and the interaction with overrides and outage
  imports.
- More state: CRs, validations, simulations, resolved plans, approvals,
  executions, activation revisions, qualification records, delegated
  identities. Each table is append-mostly and audited.
- Every widening or rollback plan needs an explicit retire/keep choice for
  each narrower activation (N6). That costs a step, and buys deterministic,
  reviewable resolution.

### Open decisions (owner / owner-side review)

Recommended defaults and tradeoffs for each open item are in
[decision-table-adr-0008-0009](../docs/architecture/decision-table-adr-0008-0009.md).
The same table records how the owner's review findings R1–R5 are resolved,
and the decisions those fixes add (N5–N8: stop and in-flight retries,
same-specificity overlap, qualification validity, maximum execution
duration).

- **O1. Self-approval through a delegate.** May the human named in a CR's
  `on_behalf_of` approve it? Proposed: **no by default**, with an explicit
  per-installation setting for single-admin pilots, shown in the approval
  record.
- **O2. MCP transport.** Proposed: stdio now. Remote HTTP with OAuth 2.1 and
  per-client delegated identities later.
- **O3. Compatibility.** Proposed: treat the approval requirement on
  publish and restore as a documented security exception inside `/api/v1`,
  with `info.version` 2.0.0. Today the only client of those operations is
  our own console. The alternative is `/api/v2` for the policy write paths.
- **O4. Cohort granularity.** Decided: scoped activation is in this
  increment (section 5). Still open: whether a cohort may also be defined by
  identity attributes, such as a device group. Proposed: no. Location
  scopes (switch, port) are deterministic and can be checked against
  observation; attribute scopes can change between approval and
  authentication.
- **O5. Approval expiry and step-up.** Proposed: 24 h expiry and password
  step-up. Is something stronger, such as WebAuthn, required before
  external pilots?

### Alternatives considered
- **An `approved: true` flag or an approval token supplied by the AI.**
  Rejected: the client could assert it.
- **Approval bound to the CR id only.** Rejected: it would survive content
  edits, rebases and environment drift.
- **MCP server with direct database or switch access.** Rejected: it would
  duplicate and bypass backend enforcement.
- **A general "run CLI" tool for flexibility.** Rejected: unbounded scope,
  with no validation or approval.

### Follow-up ADRs (not decided here)
- **0009. Access intent and enforcement strategies** (Proposed). Quarantine
  becomes a set of required access restrictions. VLAN and per-session
  role/ACL are adapter strategies with measured capability records.
  Selection never weakens the intent silently.
- **0010. Device model.** Covers:
  - identity strength: `certificate_bound`, `certificate_unbound`, `mac_profile` (spoofable), `none`
  - freshness
  - shared-port dependencies, with no automatic port bounce for a PC's DHCP problem
- **0011. Restricted MAB path.** Needs the owner-side reviewer's threat model:
  - an explicit lower-assurance path on named ports
  - never the access an EAP-TLS identity gets
  - a failed certificate never falls back to MAC access
  - no-supplicant cases kept distinct from invalid credentials
- **0012. Aruba AOS-CX integration.** Only against ASTRA's demonstrated
  capability matrix, starting with the per-session role/ACL capability test.
