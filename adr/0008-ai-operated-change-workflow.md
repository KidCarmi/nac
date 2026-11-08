# 0008. AI-operated change workflow: change requests, digest-bound human approval, delegated identities, MCP

- **Status:** Proposed (contract for the owner-side architecture/security review; no code yet)
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
- `base_version`: the active policy version when the CR was created.
- `content`: the full proposed rule set and default action, the same shape as today's draft. `content_digest` uses the existing `Digest()`.
- `revision`: an integer. Every content change increments it. Writes carry `If-Match: <revision>` and get `409` on conflict, which is how concurrent edits are detected.
- `requested_by`: the principal that created it. `on_behalf_of` is set when that principal is a delegated identity.
- `state`, with these transitions, all server-side:

  ```
  draft ──validate──▶ validated ──simulate──▶ simulated ──request──▶ approval_pending
    ▲          │                                                       │ approve (human)
    └──edit────┴───────────── any content change ◀─────────────────────┤
                                                                       ▼
  cancelled ◀── cancel (any state before executing)            approved ──execute──▶ executing
                                                                                        │
                                       observed ◀── observe ── applied ◀────────────────┤
                                       failed   ◀───────────────────────────────────────┘
  ```

- Every content change returns the CR to `draft`. Validations and
  simulations stay recorded, but they become `stale` because their
  `content_digest` no longer matches. Any approval is invalidated.

Today's global draft (`/policy-draft`) becomes the console's working CR.
Existing endpoints stay readable. How their write paths change is decision 8.

### 3. Validation and simulation are recorded evidence, kept apart from lab qualification

- `POST /change-requests/{id}/validations` records a **validation result**:
  - static checks (rules, inputs, VLAN mapping)
  - errors and warnings
  - the `content_digest` and `base_version` it ran against
  - an `environment_fingerprint`: a hash of the switch inventory, segment-VLAN maps and the input catalog
- `POST /change-requests/{id}/simulations` records a **simulation result**:
  - the existing preview, which replays recorded inputs under the base version and under the CR
  - per-session predicted changes, insufficient-evidence and stale-input cases
  - the operator-override precedence that preview already applies

  It is labelled `kind: historical_replay` with the existing "prediction" label.
- **Neither is lab qualification.** Live lab results stay in ASTRA's
  evidence. The API never reports a simulation as a measured outcome.
- **Environment drift** makes a result `stale`. Drift means a later
  `environment_fingerprint` differs, or the active version differs from
  `base_version`. The CR then needs revalidation before it can be approved
  or executed.

### 4. Approval: created by an authenticated human, bound to exactly what was reviewed

An approval is a stored record. It is never a boolean that a client sends.

- **Who may approve.** A new permission, `policy:approve`, held by human
  roles: `admin` and a new `approver` role. A delegated identity can never
  hold it. The check rejects principals of kind `delegated` *before*
  looking at roles or scopes, so no scope can grant it by accident.
- **How.** `POST /change-requests/{id}/approvals` from a human session. The
  request must carry:
  - `current_password`: a step-up re-authentication, as for a password change
  - the `content_digest`, `base_version`, `validation_id`, `simulation_id` and `plan_digest` the human reviewed

  The server checks each one against current state. Any mismatch returns
  `409`, so a human can never approve something other than what they saw.
- **Binding.** An approval binds `{cr_id, content_digest, base_version,
  validation_id, simulation_id, environment_fingerprint, plan_digest,
  scope}` and has an expiry: `expires_at`, default 24 h, configurable.
- **Invalidation.** An approval becomes `invalid` when the CR content
  changes, when the active version moves off `base_version`, when the
  environment fingerprint changes, when it expires or is revoked, or when
  the approver loses `policy:approve` or is disabled. Execution re-checks
  every condition at the moment it starts. It does not rely on cached
  approval state.
- **Single use.** An approval is consumed by exactly one execution. Reusing
  it, or using it for another CR, plan or scope, returns `409`.
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

**Scoped activation.**
- An activation has a `scope`:
  - `all`, which is today's behavior
  - a **cohort**: an explicit set of `(switch_id, port)` pairs, or whole
    switches
- Activations stay append-only (ADR-0006).
- For each authentication, `radiusauth` resolves the version to evaluate
  by taking the most specific current activation that matches the
  request's switch and `NAS-Port-Id`:
  1. a port-level cohort
  2. a switch-level cohort
  3. `all`
- The decision record stores the resolved version and the activation that
  supplied it. The console and API can then show, per session, which
  version applied and why.
- An endpoint outside the cohort keeps the previous version, including when
  it re-authenticates. This is the out-of-cohort property the acceptance
  tests check.
- Operator quarantine overrides keep precedence over every version and
  every scope, as today.

**Scope is part of what is approved.** The approval's `scope` binding is the
exact cohort or `all`. To widen a canary, a new CR or plan stage targets the
wider scope, and that needs its own approval. To roll back a cohort, a new
scoped activation restores the previous version for that cohort only.

**Execution.** `POST /change-requests/{id}/executions` takes `{approval_id}`
and an `Idempotency-Key` header. It returns `202` and an execution record.
The plan reviewed at approval time, identified by `plan_digest`, has ordered
stages:

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

Each target records four outcomes separately:
- `requested`
- `acknowledged`: CoA-ACK
- `observed`: re-authentication returned the requested VLAN, and the switch reports it
- `verified`: an independent endpoint check. NAC never fills this in. It stays `not_observed`, as in ADR-0006.

Retries are safe:
- a repeated `Idempotency-Key` returns the same execution
- a stage that already completed is a no-op
- CoA keeps at-most-one-in-flight per session, with byte-identical,
  never-retargeted retries (ADR-0006)

**An execution never clears an operator quarantine override.** Restoring a
policy version leaves overrides in place, as today. Clearing one is a
separate, explicit session action.

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
  exact content, base, validation, simulation, plan and environment the human
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
- More state: CRs, validations, simulations, approvals, executions,
  delegated identities. Each table is append-mostly and audited.

### Open decisions (owner / owner-side review)
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
