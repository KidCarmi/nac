# 0009. Access intent is separate from the enforcement strategy

- **Status:** Proposed (contract for the owner-side architecture/security review; no code yet)
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
hold for the endpoint:

| Requirement | Example (quarantine) |
|---|---|
| `deny_routed` | corporate subnets, IPv4 and IPv6 |
| `deny_same_subnet_peers` | true when other hosts in the endpoint's own subnet must be unreachable (L2 containment) |
| `allow` | remediation services (patch server, DNS, DHCP, the NAC portal) by address, port and protocol |
| `cut_established_flows` | whether flows that already existed must stop |
| `address_families` | `ipv4`, `ipv6` |

### 2. Enforcement strategies are adapters with measured capabilities

A switch platform, version and configuration can offer strategies:

| Strategy | Mechanism | Changes address? |
|---|---|---|
| `vlan` | RADIUS VLAN in Access-Accept and CoA (today's path, lab-qualified on vEOS-lab 4.29.2F) | yes |
| `role_acl` | a per-session role or downloadable ACL, set at authentication and changed by CoA | no |

A **capability record** describes each strategy on each platform. It
lists, per requirement, whether that requirement is `demonstrated`,
`unverified` or `unsupported`, and points to the evidence record. It
inherits nothing from vendor documentation.

Examples of what records must keep apart:
- **Gateway ACL vs same-subnet containment.** An ingress ACL enforced at the
  gateway does **not** demonstrate `deny_same_subnet_peers`.
- **New-flow blocking vs flow cutting.** A role that blocks new flows does
  not demonstrate `cut_established_flows`.
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
As of 2026-11-08:
- Arista vEOS-lab `vlan` is the only NAC-integrated strategy with
  demonstrated entries.
- Aruba AOS-CX simulator 10.07.0004 `role_acl` is a **partly demonstrated
  platform capability**, shown with a reference RADIUS server (`a8c8207`).
  Per-session IPv4 quarantine kept the address, blocked corp and
  endpoint-initiated same-subnet traffic, and kept remediation reachable.
  A direct RADIUS CoA then changed the role both ways on the same IP and
  VLAN, with no re-authentication in the captured window (`d3cf745`). It is
  **not NAC-integrated** and not selectable.

### 3. Selecting a strategy never weakens the intent

For each session, the backend chooses a strategy whose demonstrated
capabilities cover **every** requirement of the decided profile.

- **Order of preference:**
  1. strategies that keep the address (`role_acl`)
  2. `vlan`
  3. otherwise per installation configuration
- **If no strategy covers the profile:**
  - The decision records `unmet_requirements`.
  - It applies the installation's configured fail-safe, by default the
    strongest available strategy (usually `vlan`) or deny.
  - It never applies a weaker strategy as if it met the intent.
  - The gap shows in the session view, the simulation, the plan and the
    execution outcome.
- **A strategy is selectable** only when its capability record exists for
  that switch's platform, version and configuration. Unknown means not
  selectable.

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

### 6. Platform mapping

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
- Enforcing `cut_established_flows` and `deny_same_subnet_peers` may still
  require `vlan` on many platforms.

### Open questions (owner / owner-side review)
- **Q1. Default fail-safe** when no strategy covers a profile. Proposed:
  strongest available strategy, else deny, and the gap is always reported.
- **Q2. Quarantine profile requirements.** Proposed default:
  `deny_routed: all corporate`, `deny_same_subnet_peers: true`,
  `cut_established_flows: true`, `allow: DNS, DHCP, remediation`. The owner
  confirms what the pilot's quarantine must guarantee.
- **Q3. Mixed strategies** across switches inside one CR scope. Proposed:
  allowed, with the strategy shown per target in the plan.

- **Q4. Same-subnet requirement granularity.** On Aruba 10.07, an
  ingress-only role stopped peer TCP sessions from completing in both
  directions. Inbound peer packets still reached the endpoint (`3ff53a9`).
  Proposed: split `deny_same_subnet_peers` into:
  - `deny_same_subnet_sessions`: no connection in either direction can
    complete
  - `isolate_same_subnet_packets`: inbound peer packets must not reach the
    endpoint

  The owner states which one quarantine requires. If it is packet
  isolation, Aruba `role_acl` as tested cannot be selected for it, and the
  `vlan` strategy (or an egress policy, once demonstrated) stays the fail-safe.

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
