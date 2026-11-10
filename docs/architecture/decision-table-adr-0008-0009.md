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
| **N5** | On stop, what happens to requests already sent? | **They finish only their byte-identical retransmissions within the existing bounded window.** No first send happens after the stop | Stop takes up to the retry window to settle. Aborting retries would not unsend the first packet and would leave more targets `uncertain` |
| **N6** | Two activations at the same specificity for one unit | **Rejected (`scope-overlap`) unless the plan names the holder it supersedes.** No "newest wins" | Every overlapping canary needs an explicit retire/keep choice. In exchange, resolution is deterministic and reviewable |
| **N7** | Qualification record validity | **`valid_until` 90 days by default, and void sooner on any change of build, recipe revision or configuration fingerprint** | Periodic requalification costs lab time. Without expiry, old evidence would keep qualifying actions silently |
| **N8** | Maximum execution duration (`authority_expires_at`) | **The earlier of the approval expiry and 2 h from start** | A long `observe` stage needs a new plan. A queued execution can't act on hours-old authority |

## Review findings R1–R5 (owner review of `ed5935e`): resolutions

| ID | Finding | Resolution | Where |
|---|---|---|---|
| **R1** | The execution invalidated its own approval after activation | One start transaction consumes the approval into one immutable execution, pins the pre-activation snapshot and records the execution's authority. After `activate` commits, dispatch accepts only this execution's **expected post-activation state**; any other activation change stops it (`external_activation`). Version checks are not skipped. A restart never republishes or consumes twice | ADR-0008 §4 "Single use, owned by one execution", §5 "Execution start", "own transition", "Crash and restart"; OpenAPI `executeChangeRequest`, `Execution.expected_post_activation` |
| **R2** | Promotion and rollback left narrower activations shadowing the intended policy | Activation records with id and revision; no equal-specificity overlap (N6); partial supersession keeps the rest; widening and rollback plans list every narrower activation as `retire` or `keep`, with nothing retired implicitly; the resolved plan shows the post-change resolution of every unit; all of it is written atomically against the approved scope snapshot. The singular `base_version` binding is replaced by the scope snapshot | ADR-0008 §2, §4 "Scope snapshot", §5 "Activation records"; OpenAPI `ActivationScope`, `ScopeUnit`, `Activation`, `ScopeSnapshot`, `PlanInput.narrower_activations` |
| **R3** | The approval API could not carry the immutable resolved plan | `PlanInput` (client) is separate from `ResolvedPlan` (server, immutable, stored), whose per-target records bind incarnation, assignment, profile version, recipe revision, configuration and qualification ids, generation, strategy, limitations and impact. `plan_digest` = SHA-256 over JCS canonical JSON. The executor never re-plans under an unchanged digest. Stage grammar and non-empty, bounded, duplicate-free scopes and targets are enforced in the schema and before approval | ADR-0008 §4, §5 "The resolved plan"; OpenAPI `PlanInput`, `ResolvedPlan`, `ResolvedTarget`, `ApprovalRequest`, `requestApproval` `422` |
| **R4** | Qualification could not be matched to the requested traffic scope | Immutable qualification records with a structured tested scope (requirement, direction, protocols, families, destination class, flow state), platform build, recipe revision, configuration fingerprint, evidence digest, expiry and issuer. Coverage matches on every axis. Eligibility is per profile version and target, with the reason for each unmet requirement; the inventory shows only `readiness`. Attestations carry issuer, version, issue and expiry times. Allow entries are as explicit as requirements | ADR-0009 §2 "Qualification records", "Coverage matching", "Eligibility", §7; OpenAPI `QualificationRecord`, `TestedScope`, `EligibilityResult`, `Attestation`, `EnforcementCapability`, `getProfileEligibility` |
| **R5** | CR mutability and stop/partial-completion semantics were unfinished | Content can change only before execution; from `executing` on the CR is immutable (`cr-immutable`) and a later change is a new CR. Edit, cancel and execute race on one compare-and-set, with one winner. Delegated ownership covers edit, cancel, request, execute and stop. `POST /executions/{id}/stop` defines pending, sent and uncertain work (N5), and reports what remains in force: the committed activation stays, a rollback is a separate approved CR, and overrides are untouched | ADR-0008 §2 "Mutable states", "Immutable from execution on", "Ownership", §5 "Stopping an execution"; OpenAPI `stopExecution`, `cancelChangeRequest`, `putChangeRequestContent`, `Execution.stop`, `still_in_force` |

**Validated (proposed schema, Draft 2020-12, `jsonschema` 4.26):**
- every `$ref` resolves
- all 40 component schemas pass the metaschema
- both counterexamples from the review are now **rejected**: an empty cohort with `apply → activate → activate`, and a capability with `platform: {}`, `selectable: true` and no evidence
- so are an empty `apply`, a duplicate target, targets on `activate`, `observe` before `apply`, an attestation without expiry, a qualification record without a tested scope or evidence, a direction-less allow entry, and a resolved target without bindings
- valid plans, inventory records and qualification records are accepted

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
