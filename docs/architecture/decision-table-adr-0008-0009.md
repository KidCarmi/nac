# Open decisions: ADR-0008 and ADR-0009

**Status: proposed defaults for the owner's architecture/security review.**
Nothing here is accepted. Reconciled 2026-11-10 with the proposed
network-device driver ADR (PR #4).

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

## What this does not authorize

- **Implementation:** no implementation of ADR-0008, ADR-0009 or the PR #4
  driver framework.
- **Platforms and integrations:** no general driver framework, no broad
  vendor expansion, and no firewall integration.
- **Release:** no merge or tag.

After the owner's review and the candidate qualification, the next scope is
the smallest customer-AI slice:

draft → validate / dry-run → human approval → scoped execution → honest outcome.
