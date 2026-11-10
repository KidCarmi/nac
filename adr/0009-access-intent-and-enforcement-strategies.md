# 0009. Access intent is separate from the enforcement strategy

- **Status:** Proposed (contract for the owner-side architecture/security review). Reconciled 2026-11-10 with the proposed network-device driver ADR (PR #4). Neither is accepted; the owner retains the architecture/security review.
- **Date:** 2026-11-08
- **Deciders:** OPUS (backend), owner-side assistant (architecture/security review), repository owner
- **Relates to:** ADR-0006 (pilot architecture), ADR-0008 (change workflow)

## Context

In `pilot-rc10`, "quarantine" *is* VLAN 20.
- Policy returns a segment.
- Each switch maps the segment to a VLAN.
- CoA moves the session to that VLAN.

The lab measured what that costs:
- The **old** access is denied in about 1–2.5 s.
- The endpoint then has to get a new address:
  - Windows Server 2025 takes about 30–65 s.
  - Linux (systemd-networkd) waits for T2 or for lease expiry. That was
    minutes on a 1-hour lease.
  - Short DHCP timers failed the reverse direction (`e86242e`).
- On vEOS-lab, an accepted CoA that carries VLAN attributes also
  interrupts forwarding for up to about 58 s.

**Owner direction (2026-11-08):**
- Test per-session **role/ACL** quarantine that keeps the endpoint's VLAN
  and IP. First target: Aruba AOS-CX simulator, a capability test owned by
  ASTRA.
- Keep the VLAN path as a compatibility strategy.
- Model quarantine as *required access restrictions*, not as a VLAN.
- Never weaken security silently to keep connectivity.
- Expose intent, the selected mechanism, its limitations and the observed
  outcome identically in API, UI and MCP.

Vendor documentation is a reason to test, not evidence that our image
supports the feature:
- Aruba CX port-access roles carry per-session ingress IPv4/IPv6 policy,
  with VLAN optional.
- Cisco documents that a port bounce affects every host on the port.

## Decision

### 1. Policy decides an access intent, not a VLAN

A rule's outcome names an **access profile**: a stored, versioned
description of what the endpoint must and must not reach. Today's segments
become profiles: `corp` → full access, `quarantine` → the quarantine
profile.

A profile states **requirements**. Each requirement is a fact that must
hold for the endpoint. Every requirement names its **traffic direction**
(`from_endpoint`, `to_endpoint` or `both`), its **protocols** (`tcp`,
`udp`, `icmp`, `any`) and its **address families**. One that doesn't is
invalid; nothing is implied by a requirement's name.

| Requirement | Meaning | Example (quarantine) |
|---|---|---|
| `deny_routed` | New connections from the endpoint to these destinations cannot be established | corporate subnets; `from_endpoint`; tcp, udp, icmp; IPv4 and IPv6 |
| `deny_same_subnet_sessions` | No connection between the endpoint and a same-subnet peer can complete, in the stated direction(s) | `both`; tcp, udp |
| `isolate_same_subnet_packets` | Packets from same-subnet peers do not reach the endpoint at all (L2 packet isolation). A stronger claim than the line above | — (Q4) |
| `block_established_flow_data` | While restricted, no data passes on flows that existed before the restriction | tcp; `both` (Q5) |
| `terminate_established_flows` | Transport state of pre-existing flows is torn down | — (Q5) |
| `allow` | Services that stay reachable: remediation, DNS, DHCP, the NAC portal, by address, protocol and port | remediation `10.70.20.10/32` tcp/8080 |
| `address_families` | `ipv4`, `ipv6` | both |

**Blocking and teardown are different claims.**
- `block_established_flow_data` says nothing about what happens after the
  restriction ends.
- `terminate_established_flows` ends transport state. **It does not
  guarantee that application data is never sent later**: an application
  can reconnect and resend queued data after access returns.
- No requirement in this vocabulary claims permanent suppression of
  application data. **Ingress filtering, inbound packet isolation, blocking
  existing-flow data and transport teardown are four separate claims**, and
  each needs its own evidence.

### 2. Enforcement strategies are adapters with measured capabilities

A switch platform, version and configuration can offer strategies:

| Strategy | Mechanism | Changes address? |
|---|---|---|
| `vlan` | RADIUS VLAN in Access-Accept and CoA (today's path, lab-qualified on vEOS-lab 4.29.2F) | yes |
| `role_acl` | a per-session role or downloadable ACL, set at authentication and changed by CoA | no |

A **capability record** describes each strategy (a *recipe*) on each
platform scope. It has three **independent** dimensions (from the proposed
driver ADR, PR #4):

| Dimension | Values | Meaning |
|---|---|---|
| Research | `draft` / `source-backed` / `disputed` | Quality and provenance of the described behavior, e.g. vendor documentation or another product's source |
| Implementation | `absent` / `partial` / `implemented` | Whether our code contains the operation |
| Qualification, **per requirement and environment** | `unverified` / `demonstrated` / `unsupported` / `inconclusive` | What our lab evidence establishes, with the evidence reference |

- **No dimension implies another.** A `source-backed` recipe (for example
  one read from PacketFence) with `absent` implementation can be shown for
  planning. It is **never** executable, selectable or "qualified" as a
  result.
- **Offline fixtures and unit tests prove code behavior only.** They don't
  change qualification.
- **Nothing is inherited from vendor documentation.**
- **Selectable** requires all of:
  - `implemented`
  - `demonstrated` for every requirement of the profile in the matching
    platform scope
  - an evidence record that is current for that configuration (§7)
- **Scope** is vendor, family, model, OS/build, physical or virtual, the
  operation, the authentication method, the configuration fingerprint (§7)
  and the topology. One successful build does not extend to a version
  range.

Examples of what records must keep apart:
- **Gateway ACL vs same-subnet containment.** An ingress ACL enforced at the
  gateway does **not** demonstrate `deny_same_subnet_sessions` or
  `isolate_same_subnet_packets`.
- **New-flow blocking vs existing flows.** A role that blocks new flows does
  not demonstrate `block_established_flow_data`, and blocking data does not
  demonstrate `terminate_established_flows`.
- **Phone + PC on one port.** Shared-port behavior (does the phone keep its
  authorization?) is recorded per strategy.

Port bounce is **not** an enforcement strategy. It is a recovery action,
and an explicit last resort. It needs all of these:
- demonstrated platform support
- a recorded shared-port impact
- the device model's single-endpoint check (ADR-0010)
- its own approval in the change workflow (ADR-0008)

The records live in
[`docs/architecture/enforcement-capability-matrix.md`](../docs/architecture/enforcement-capability-matrix.md).
As of 2026-11-10:

| Recipe | Research | Implementation | Qualification (lab, exact build) |
|---|---|---|---|
| Arista vEOS-lab 4.29.2F `vlan` (Tunnel-\* in Access-Accept and CoA) | source-backed | implemented (pilot line) | `deny_routed` demonstrated via readdressing; see the matrix |
| Aruba AOS-CX Virtual 10.07.0004 local role (`Aruba-User-Role` in Access-Accept and CoA) | source-backed | implemented (`pilot-aruba-rc3`/`rc4`, KidCarmi/nac#3) | NAC-integrated evidence on exact `pilot-aruba-rc3` (`148e196`) and again on exact `pilot-aruba-rc4` (`15e89db`, console path): corp → quarantine → corp with VLAN and IP unchanged; `deny_routed` (IPv4 TCP; UDP from the endpoint); `block_established_flow_data` (one TCP flow); `deny_same_subnet_sessions` (TCP). **`isolate_same_subnet_packets` unsupported** as tested; **`terminate_established_flows` not demonstrated**. `pilot-aruba-rc4` is under qualification |
| Aruba AOS-CX `Filter-Id` in CoA (PacketFence's recipe) | source-backed | absent | unverified: **not executable** |

Execution of the Aruba recipe in the pilot is limited to the frozen
candidate and its lab. This ADR does not make it a production-selectable
strategy.

### 3. Selecting a strategy never weakens the intent

For each session, the backend chooses a strategy whose demonstrated
capabilities cover **every** requirement of the decided profile.

- **Order of preference among selectable strategies:**
  1. strategies that keep the address (`role_acl`)
  2. `vlan`
- **There is no universally "strongest available" strategy.** VLAN and role
  restrictions cover different requirements, so neither is a safe automatic
  substitute for the other.
- **Admission when the restriction cannot be compiled.** If no selectable
  strategy can express the required restrictions for this switch, for
  example because of a missing role mapping or an unqualified requirement:
  - The admission is **rejected** (Access-Reject).
  - It is never an unconstrained Access-Accept, and a broader cached
    assignment is never reused.
  - The same check applies at every re-authentication, not only to
    operator CoA. **The pilot already rejects a decision for a segment
    with no role mapping.**
- **Active session when no strategy covers the profile:**
  - The decision records `unmet_requirements` and **retains the desired
    restriction** for the next authentication.
  - It executes **only an explicitly configured, eligible fail-safe
    action**. If none exists, the outcome is an **enforcement failure**,
    reported in the session view, the simulation, the plan and the
    execution outcome.
  - A future Access-Reject does not disconnect an already authorized
    session. That is stated, not implied.
  - There is no fallback from a failed CoA to a port bounce. The pilot
    refuses a Restore that the policy would turn into a deny (`422`) and
    keeps the quarantine.
- **A strategy is selectable** only under §2's rule: implemented, and
  demonstrated for every requirement in a current configuration scope.
  Unknown means not selectable.

### 4. Same fields everywhere

The following carry `intent` (profile and version), `strategy`,
`strategy_limitations` and `unmet_requirements`:
- decisions
- simulations (ADR-0008 §3)
- plans and approvals (the approval binds the selected strategy)
- execution outcomes

Observed outcomes keep the ADR-0006 split:
- requested
- acknowledged
- observed on the switch
- endpoint verification (`not_observed` from NAC)

The console and the MCP tools read the same fields. No client derives them.

### 5. Recovery for VLAN transitions stays separate and bounded

When a necessary VLAN transition readdresses a managed endpoint, a
**coordinated endpoint recovery action** is a design option. It is not
built now.

- **The constraint:** an instruction sent only after the VLAN change may
  never arrive, because the endpoint has lost its management path.
- **So the action must be armed before the change, or triggered locally.**
  Either:
  - the endpoint is told "renew after the next re-authentication" while it
    is still reachable, or
  - it detects the change itself.
- **The opt-in reference hook** in `docs/pilot/endpoint-dhcp-hook/` is a
  heuristic, local-trigger experiment. It is not this action, and not a
  default.

### 6. Platform mapping (see §7 for targeting and drift)

**Arista EOS:** unchanged.
- `vlan` sends Tunnel attributes in Access-Accept and CoA, the
  lab-validated set.
- `role_acl` is not modelled until a capability record exists.

**Aruba AOS-CX:** `role_acl` (for example a user-role VSA, or downloadable
roles) is added **only** for requirements that ASTRA's image-specific
matrix demonstrates with real traffic (ADR-0012). That means:
- allowed → restricted → allowed, with continuous probes
- fresh and established flows
- IPv4 and IPv6
- same-subnet and routed destinations
- remediation reachability
- a phone + PC shared port

A reference RADIUS harness may establish what the platform can do. It is
labelled separately from an integrated NAC pass.

### 7. Targeting, configuration drift and override scope

**Exact session targeting.**
- Each recipe defines the minimum identifying attributes for each action
  and their matching semantics. The pilot's CoA uses User-Name,
  NAS-IP-Address, NAS-Port-Id and Calling-Station-Id, captured at request
  time; retries never re-target.
- An operation is session-scoped only when those attributes select the
  intended session with the qualified platform behavior.
- **Missing or ambiguous identifiers block the operation.** The selector is
  never broadened, for example to User-Name or MAC alone, to make a NAK go
  away.
- Requested selector scope and demonstrated impact scope are recorded
  separately.
- Support for an attribute in Access-Accept does not imply support in CoA
  or Disconnect.

**Role/ACL content and configuration drift.**
- A role name is not evidence of its contents. Capability evidence is bound
  to a **configuration fingerprint**. It covers the role and ACL
  definitions and referenced objects, the implicit action, the direction,
  VLAN membership and isolation, the authentication host mode, and the
  fallback and critical-authentication settings. Credentials and counters
  are excluded.
- If the fingerprint changes, or the evidence is stale or unavailable,
  coverage for that switch becomes **unknown**. The recipe is then not
  selectable, and unexecuted plans and approvals that bound the old
  fingerprint are **invalidated** (ADR-0008 §4).
- A role that keeps its name but changes its rules loses its qualified
  status.
- Where automatic observation is unavailable (today, AOS-CX: NAC has no
  observation adapter), an **explicitly approved, versioned operator
  attestation** can be recorded with its age and limits. It is not a live
  verification. Each requirement declares whether attestation is
  acceptable for it.
- A firmware or driver change triggers re-evaluation; qualification is not
  inherited.

**Override scope is preserved.**
- The operator quarantine override keeps the ADR-0006 scope: one identity
  (certificate issuer and serial) on one switch port and MAC, carried
  across re-authentication and session replacement.
- It is **not** reinterpreted as a global device quarantine. A device-wide
  override would be a separate identity-scope decision, with a plan
  targeting every eligible session. Moving to another port is a
  qualification case, not an assumed invariant.

## Consequences

### Positive
- Quarantine can avoid readdressing where a platform can enforce it per
  session. That removes the DHCP recovery problem rather than working
  around it.
- Security never degrades silently: a gap appears as `unmet_requirements`.
- New platforms add strategies and capability records, not new policy
  semantics.

### Negative / trade-offs
- Profiles need a requirement vocabulary that operators and the skill can
  state correctly. A wrong `allow` list is a security error.
- Capability records need lab evidence per platform, version and
  configuration before a strategy can be selected.
- The decision path gains a selection step. It must stay deterministic and
  auditable.
- Enforcing `terminate_established_flows` and `isolate_same_subnet_packets`
  may still require `vlan`, or a mechanism not yet demonstrated, on many
  platforms.

### Open questions (owner / owner-side review)

Recommended defaults and tradeoffs for each open item are in
[decision-table-adr-0008-0009](../docs/architecture/decision-table-adr-0008-0009.md).

- **Q1. Fail-safe** when no strategy covers a profile.
  - **Admission:** reject (§3). This is decided by this reconciliation, not
    left open.
  - **Active sessions: open.** Which explicitly configured fail-safe
    actions, if any, an installation may enable. Proposed: none by default,
    which means enforcement failure is reported and the desired restriction
    is retained.
- **Q2. Quarantine profile requirements.** Proposed default, each with
  direction and protocol:
  - `deny_routed`: all corporate prefixes, `from_endpoint`, any protocol,
    IPv4 and IPv6
  - `deny_same_subnet_sessions`: `both`, tcp and udp
  - `block_established_flow_data`: `both`
  - `allow`: DNS, DHCP and remediation

  `isolate_same_subnet_packets` and `terminate_established_flows` are not
  in the default (Q4, Q5). The owner confirms what the pilot's quarantine
  must guarantee.
- **Q3. Mixed strategies** across switches inside one CR scope. Proposed:
  allowed, with the strategy shown per target in the plan.

- **Q4. Same-subnet requirement.** The vocabulary in §1 now has both:
  - `deny_same_subnet_sessions`: no connection can complete
  - `isolate_same_subnet_packets`: inbound peer packets don't reach the
    endpoint

  On Aruba 10.07, an ingress-only role stopped peer TCP sessions in both
  directions, but inbound peer packets still reached the endpoint
  (`3ff53a9`, confirmed through NAC on `148e196`). **Open: which one the
  quarantine profile requires.** If it is packet isolation, the tested
  Aruba role cannot be selected for it, and an admission that requires it
  on that switch is rejected (§3).

- **Q5. Established flows.** The vocabulary in §1 now has both:
  - `block_established_flow_data`: no data passes while restricted
  - `terminate_established_flows`: transport state is torn down

  **Termination does not guarantee that queued application data is never
  sent**: an application can reconnect and resend after access returns.

  On Aruba 10.07, the role blocked data on an existing TCP flow (0 of 8
  payloads, no FIN or RST). The same socket resumed after Restore and
  delivered data queued during quarantine (`42be71f`; through NAC on
  `148e196`). **Open: which one quarantine requires.** Termination would
  need another, undemonstrated mechanism.

### Alternatives considered
- **Keep quarantine = VLAN and tune DHCP.** Measured: authoritative DHCP
  only moves recovery to T2, short timers failed in reverse, and the
  endpoint hook is heuristic. It stays as a compatibility path, not as the
  design.
- **Assume role/ACL from vendor documentation.** Rejected: the selected
  simulator image may not support every requirement, and a displayed role
  or a CoA-ACK is not enforcement.
- **Port bounce as the default recovery.** Rejected: it hits every host on
  a shared port, and per-session policy may avoid readdressing altogether.
