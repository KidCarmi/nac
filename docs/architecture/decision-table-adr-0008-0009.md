# Open decisions: ADR-0008 and ADR-0009

**Status: proposed defaults for the owner's architecture/security review.**
Nothing here is accepted. Reconciled 2026-11-10 with the proposed
network-device driver ADR (PR #4). Revised the same day for the owner's
review findings R1–R5 (contract defects, resolved below). The new choices
those fixes create are proposed decisions N5–N8.

## Invariants (not open)

- **Approval is human-only.** A delegated (AI) identity can never approve,
  through the API or MCP. The check rejects delegated principals before
  looking at roles or scopes (ADR-0008 §4, §6).
- **One path for every client.** The console, the API and MCP use the same
  authorization, validation, approval and execution services. MCP has no
  database, switch or secret access (ADR-0008 §1, §7).
- **Admission never fails open.** If the required restrictions can't be
  compiled for a switch, the admission is rejected, never an unconstrained
  Access-Accept (ADR-0009 §3).
- **Research is not qualification.** A source-backed recipe, for example
  one taken from PacketFence, is never executable or qualified by being
  recorded (ADR-0009 §2).

## Decisions

| ID | Question | Recommended default | Tradeoff |
|---|---|---|---|
| **O1** | May the human named in a CR's `on_behalf_of` approve that CR? | **No.** A per-installation single-admin exception, shown in the approval record | Single-admin pilots need a second human or a visible, recorded exception |
| **O2** | MCP transport | **stdio now.** Remote HTTP with OAuth 2.1 and per-client delegated identities later | No remote AI access until OAuth and per-client identities exist |
| **O3** | Publish and restore now require an approval: compatibility | **A documented security exception inside `/api/v1`**, with `info.version` 2.0.0. Today the only client is our console | Any external script that publishes directly breaks. The alternative, `/api/v2`, duplicates the policy write paths |
| **O4** | Cohorts by identity attributes (e.g. a device group) | **No: location scopes only** (switch, port) | No device-group canary. Attribute scopes could change between approval and authentication |
| **O5** | Approval expiry and step-up | **24 h and a password step-up now; WebAuthn required before external customer pilots** | A password step-up is weaker against a stolen session than WebAuthn |
| **Q1** | Fail-safe for an active session whose profile no strategy covers | **None by default.** Report an enforcement failure, keep the desired restriction for the next authentication, and run only an explicitly configured eligible action. (Admission: reject, decided) | The session keeps its previous access until it re-authenticates or an operator acts. No automatic substitution |
| **Q2** | What the default quarantine profile requires | `deny_routed` (corporate, `from_endpoint`, any protocol, IPv4 and IPv6); `deny_same_subnet_sessions` (`both`, tcp and udp); `block_established_flow_data`; `allow` DNS, DHCP and remediation | A narrower default than "full isolation". Stronger requirements must be chosen explicitly and need demonstrated strategies |
| **Q3** | Mixed strategies across switches in one CR scope | **Allowed**, with the strategy and its limits shown per target in the plan and the approval | Plans are heterogeneous; reviewers must read per-target limits |
| **Q4** | Same-subnet: sessions or packet isolation? | **`deny_same_subnet_sessions` by default.** `isolate_same_subnet_packets` only when a profile requires it | On Aruba 10.07 a peer's packets still reach the endpoint (no connection completes). A profile that requires isolation makes that role non-selectable, so admission is rejected on such switches |
| **Q5** | Established flows: block data or tear down? | **`block_established_flow_data`.** Termination not required by default | Data queued during quarantine may arrive after Restore. Termination has no demonstrated mechanism, and it would not guarantee that an application never resends |
| **N1** | Configuration evidence where NAC can't observe the switch (AOS-CX today) | **Lab only: an approved, versioned operator attestation with its age. Production: requires an observation adapter** | An attestation can go stale without anyone noticing; it is never shown as live verification |
| **N2** | Scope of the operator quarantine override | **Keep ADR-0006: one identity on one switch port.** A device-wide override is a separate future decision | The same certificate on another port is not automatically quarantined (a qualification case) |
| **N3** | Research catalog in the product | **Documentation only now.** The product lists only implemented recipes with their qualification. A planning view of source-backed entries later, never executable | Planning visibility waits for the product UI |
| **N4** | When the generation fence is required | **Before the CR workflow's queued execution ships.** Until then, operator session actions keep "one CoA in flight" (`409`) and refuse a policy-denied Restore (`422`) | No queued, AI-requested execution until the fence exists |
| **N5** | On stop, what happens to requests already sent? | **Operator Stop:** they finish only their byte-identical retransmissions within the existing bounded window; no first send happens after the stop. **Authority withdrawal** (approver disabled, approval revoked, evidence expired…) is a security cancellation: retries end at once, and a later Stop cannot resume them | Stop takes up to the retry window to settle. A withdrawal leaves more targets `uncertain`, which is the right cost when authority is gone |
| **N6** | Two activations at the same specificity for one unit | **Rejected (`scope-overlap`) unless the plan names the holder it supersedes.** No "newest wins" | Every overlapping canary needs an explicit retire/keep choice. In exchange, resolution is deterministic and reviewable |
| **N7** | Qualification record validity | **`valid_until` 90 days by default, and void sooner on any change of build, recipe revision or configuration fingerprint** | Periodic requalification costs lab time. Without expiry, old evidence would keep qualifying actions silently |
| **N8** | Maximum execution duration (`authority_expires_at`) | **The earlier of the approval expiry and 2 h from start** | A long `observe` stage needs a new plan. A queued execution can't act on hours-old authority |
| **N9** | Maximum age of **observed** configuration evidence (`valid_until`) | **24 h after `observed_at`**, and void sooner on any fingerprint change | A switch that isn't re-observed daily loses eligibility until it is. Stale observations would otherwise qualify actions |

## Review findings R1–R5 (owner review of `ed5935e`): resolutions

| ID | Finding | Resolution | Where |
|---|---|---|---|
| **R1** | The execution invalidated its own approval after activation | One start transaction consumes the approval into one immutable execution, pins the pre-activation snapshot and records the execution's authority. After `activate` commits, dispatch accepts only this execution's **expected post-activation state**; any other activation change stops it (`external_activation`). Version checks are not skipped. A restart never republishes or consumes twice | ADR-0008 §4 "Single use, owned by one execution", §5 "Execution start", "own transition", "Crash and restart"; OpenAPI `executeChangeRequest`, `Execution.expected_post_activation` |
| **R2** | Promotion and rollback left narrower activations shadowing the intended policy | Activation records with id and revision; no equal-specificity overlap (N6); partial supersession keeps the rest; widening and rollback plans list every narrower activation as `retire` or `keep`, with nothing retired implicitly; the resolved plan shows the post-change resolution of every unit; all of it is written atomically against the approved scope snapshot. The singular `base_version` binding is replaced by the scope snapshot | ADR-0008 §2, §4 "Scope snapshot", §5 "Activation records"; OpenAPI `ActivationScope`, `ScopeUnit`, `Activation`, `ScopeSnapshot`, `PlanInput.supersessions` (was `narrower_activations`; generalized for the R2 residual) |
| **R3** | The approval API could not carry the immutable resolved plan | `PlanInput` (client) is separate from `ResolvedPlan` (server, immutable, stored), whose per-target records bind incarnation, assignment, profile version, recipe revision, configuration and qualification ids, generation, strategy, limitations and impact. `plan_digest` = SHA-256 over JCS canonical JSON. The executor never re-plans under an unchanged digest. Stage grammar and non-empty, bounded, duplicate-free scopes and targets are enforced in the schema and before approval | ADR-0008 §4, §5 "The resolved plan"; OpenAPI `PlanInput`, `ResolvedPlan`, `ResolvedTarget`, `ApprovalRequest`, `requestApproval` `422` |
| **R4** | Qualification could not be matched to the requested traffic scope | Immutable qualification records with a structured tested scope (requirement, direction, protocols, families, destination class, flow state), platform build, recipe revision, configuration fingerprint, evidence digest, expiry and issuer. Coverage matches on every axis. Eligibility is per profile version and target, with the reason for each unmet requirement; the inventory shows only `readiness`. Attestations carry issuer, version, issue and expiry times. Allow entries are as explicit as requirements | ADR-0009 §2 "Qualification records", "Coverage matching", "Eligibility", §7; OpenAPI `QualificationRecord`, `TestedScope`, `EligibilityResult`, `Attestation`, `EnforcementCapability`, `getProfileEligibility` |
| **R5** | CR mutability and stop/partial-completion semantics were unfinished | Content can change only before execution; from `executing` on the CR is immutable (`cr-immutable`) and a later change is a new CR. Edit, cancel and execute race on one compare-and-set, with one winner. Delegated ownership covers edit, cancel, request, execute and stop. `POST /executions/{id}/stop` defines pending, sent and uncertain work (N5), and reports what remains in force: the committed activation stays, a rollback is a separate approved CR, and overrides are untouched | ADR-0008 §2 "Mutable states", "Immutable from execution on", "Ownership", §5 "Stopping an execution"; OpenAPI `stopExecution`, `cancelChangeRequest`, `putChangeRequestContent`, `Execution.stop`, `still_in_force` |

## Re-review residuals (owner re-review of `a43ac93`): resolutions

The re-review verified the R1–R5 fixes and named four residuals. This
revision addresses only those; the fixed portions are not reopened.

| ID | Residual | Resolution | Where |
|---|---|---|---|
| **R1** | Continuing authority after consumption omitted the approver and the evidence | Before the activation commit and before every first send, the executor re-checks the approver (enabled, holds `policy:approve`), the approval (not revoked), the executing principal, `authority_expires_at`, and **every bound qualification record and configuration evidence** (unexpired, not superseded). An identical fingerprint with expired evidence fails. Any failure **withdraws authority**: no new activation or send, retries end at once, and a later operator Stop can't reverse it. The execution's own committed activation stays in force (the only exception). Revoking a consumed approval is how a human withdraws it | ADR-0008 §5 "Continuing authority after consumption"; OpenAPI `revokeApproval`, `Execution.authority.state`/`withdrawn_reason`, `TargetOutcome.not_sent_reason` |
| **R2** | Same-specificity replacement had no input contract | `PlanInput.supersessions` with typed entries: `replace` (a same-specificity holder whose units are all inside the scope, including every `all` → `all`), `take_units` (only the listed units of a same-specificity cohort; the rest keep their version) and `retire`/`keep` (narrower). Each has an **expected revision**. An unnamed holder, a wrong revision, or units outside the scope get `422`. The resolved plan binds each supersession and its moved units | ADR-0008 §5 "Every supersession is a typed plan input"; OpenAPI `SupersessionInput`, `ResolvedPlan.activation.supersessions` |
| **R4** | Coverage lost service precision; configuration evidence was structurally optional | Tested scopes and requirements carry **services** (protocol and port ranges) and **destination prefixes**. Coverage needs service containment (TCP/8080 never covers 443) and prefix containment, plus an **equal** authentication method and topology: `single_endpoint` never covers `phone_and_pc`, and unknown is never covered. `ConfigurationEvidence` is a discriminated union: `observed` requires `observed_at`, `source` and `valid_until` (N9); `operator_attestation` requires its attestation record with expiry; `unavailable` never qualifies | ADR-0009 §2 "Coverage matching", §7; OpenAPI `Service`, `Prefix`, `TestedScope`, `TrafficRequirement`, `AccessProfile.allow`, `ConfigurationEvidence`, `QualificationRecord.topology`, `EligibilityResult` |
| **R4 (second re-review)** | One authenticated session was treated as proof of `single_endpoint`; prose allowed omitted ports | A session count is only a **lower bound**: it can prove sharing, never exclusivity. `single_endpoint` requires current configuration evidence that the port **enforces** a single host (single-host mode or a one-client limit), or a scoped, unexpired lab attestation. Otherwise the topology is `unknown`. LLDP absence or one FDB entry is never enough. The assessment, with its basis, evidence id and validity, is bound per target in the resolved plan. Ports are always explicit; every port is 1–65535 | ADR-0009 §2 "Port topology is evidence, not a session count", A-R4-13 to A-R4-19; ADR-0008 §5 resolved plan; OpenAPI `TopologyAssessment`, `ResolvedTarget.topology`, `EligibilityResult.target_topology`, `TrafficRequirement` |
| **R4 (third review)** | An admission setting was treated as physical exclusivity; the shared-topology branch didn't enforce provenance | Topology has two dimensions. **Admission** (`exclusive_single_client`, `multiple_clients_present`, `multiple_clients_allowed` or unknown) governs per-session actions. **Physical dependencies** (`no_dependency`, `dependency_present` or unknown) gate whole-port actions, and only `no_dependency` from fresh dependency evidence or a scoped lab attestation can authorize one. **A single-host or one-client setting alone never authorizes a whole-port action.** Every known value of both dimensions carries a basis-specific evidence reference and validity. A sessions lower bound needs an observation reference and at least 2 distinct clients, and never identifies a phone. Records carry `action_scope` and `tested_topology` | ADR-0009 §2 "Port topology is evidence… admission is not physical topology", A-R4-6/7 and A-R4-13 to A-R4-22; ADR-0008 §5 resolved plan and impact; OpenAPI `TopologyAssessment`, `AdmissionAssessment`, `PhysicalAssessment`, `QualificationRecord.action_scope`/`tested_topology`, `ResolvedTarget.impact` |
| **R5** | Stop did not fence the activation | The execution row is one fence. The activation transaction locks it and commits only if the execution is `running` and its authority is active and unexpired by the database clock. Stop, withdrawal and expiry move the state off `running` under the same lock. Exactly one ordering: if Stop wins, **no activation commits**, including for an activate-only plan; if activation wins, Stop lists it in `still_in_force` | ADR-0008 §5 "One execution fence"; OpenAPI `stopExecution`, `Execution.still_in_force` |

N7 (90-day qualification validity) and every other default in this table
remain **proposed, not accepted**.

**Validated (proposed schema, Draft 2020-12, `jsonschema` 4.26), after the residual revision:**
- every `$ref` resolves (55)
- all 43 component schemas pass the metaschema
- **30 accept/reject cases**:
  - the review's original counterexamples are still rejected
  - R2: `all` → `all` `replace` and a one-port `take_units` are accepted. A `take_units` without units, a supersession without an expected revision, an arbitrary extra property, and the old `narrower_activations` field are rejected
  - R4: attested evidence without its record, observed evidence without time, source or validity, unavailable evidence with a fingerprint, a tested scope without services or destinations, TCP without ports, ICMP with ports, and a free-text topology are rejected
- **12 checks of the coverage rule as written** (a reference script, not product code):
  - 8080 evidence does not cover 443 or "all TCP ports"
  - TCP does not cover UDP
  - one /24 does not cover another, and a /32 does not cover its /24
  - `single_endpoint` does not qualify `phone_and_pc`
  - unknown topology is never covered
  - expired or incomplete attestations don't qualify

**Examples and acceptance scenarios:**
- 52 focused positive and negative instances for the residuals, each
  labelled with its finding, acceptance scenario and expected result, are in
  [`api/openapi/proposed/examples/`](../../api/openapi/proposed/examples/README.md).
- The acceptance scenarios are A-R1-1 to A-R5-5 in ADR-0008 §5, and A-R4-1
  to A-R4-22 in ADR-0009 §2.
- After the third review: all 58 `$ref`s resolve, and all 46 component
  schemas pass the metaschema. All 52 examples behave as labelled, also
  with format checking; that includes the reviewer's three reproduced
  topology instances, which are now rejected. The coverage-rule script has
  21 checks, including A-R4-20 to A-R4-22: single-host plus a phone,
  per-session versus whole-port actions.
- **Next slice:** [implementation plan for the change-request
  core](implementation-plan-change-request-core.md). It covers draft,
  validation, replay, approval and audit, with activation and dispatch
  disabled, and lists the decisions it depends on (O1, O5, minimal
  operator management, CR vs. `/policy-draft`).

This checks the response and request contract only; nothing is implemented.

## What this does not authorize

- **Implementation:** no implementation of ADR-0008, ADR-0009 or the PR #4
  driver framework.
- **Platforms and integrations:** no general driver framework, no broad
  vendor expansion, and no firewall integration.
- **Release:** no merge or tag.

After the owner's review and the candidate qualification, the next scope is
the smallest customer-AI slice:

draft → validate / dry-run → human approval → scoped execution → honest outcome.
