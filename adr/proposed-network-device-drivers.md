# Proposed ADR: Go network-device drivers and a research-first integration catalog

- Status: Proposed; design only, no implementation or device qualification
- Date: 2026-11-09
- Scope: KidCarmi/nac network-device integrations
- Relates to: ADR-0002, ADR-0006, proposed ADR-0008 and ADR-0009
- Number: assign on acceptance; 0010–0012 are already referenced for other work
- Owner request: study PacketFence integrations and prepare the Go architecture now, without running equipment tests

## Context

We need to add switch families without scattering vendor conditionals across policy, HTTP handlers and CoA workers. The repository already separates a pure decision engine from adapters. FreeRADIUS terminates EAP-TLS; nacd decides authorization. This ADR preserves that boundary.

At the inspected contract-branch snapshot `162e4eeaf99443927b431e7d7ab29cb9312b6a22`:
- `internal/radius` contains the packet codec and authenticated UDP exchange.
- `internal/radiusauth` normalizes FreeRADIUS input and orchestrates admission.
- `internal/pilot/coa.go` combines policy/override resolution, immutable targeting, persistence and execution.
- `internal/pilot/eapi.go` contains Arista transport, parsing and observation.
- `internal/secrets`, PostgreSQL storage and the versioned API already exist.

These are migration anchors, not a claim that concurrent Aruba work has not changed them. Reconcile against that work before implementation; do not replace it with a second adapter stack.

Our Aruba investigation also showed why source references are not qualification: missing global authentication configuration initially looked like a simulator limitation. Authentication, dynamic authorization and data-plane coverage must be described separately.

## Research: what PacketFence teaches us

Research is based on PacketFence v15.2.0 device documentation and source at devel commit `1041248cc6856d99f66dd6eed01f36bf2186e058`. The devel source is not represented as a released-version guarantee.

| Source observation | Consequence for this proposal |
|---|---|
| Network-device support uses a base switch class, vendor subclasses and declared capabilities. | Keep platform knowledge modular, but use small Go interfaces and composition instead of reproducing the inheritance tree. |
| The base class includes policy-to-role/VLAN/ACL mappings, SNMP reads/writes, topology and session helpers, and dynamic authorization. | Separate policy selection, protocol translation, transport and observation; do not create a Go interface with hundreds of methods. |
| NAS port identifiers can differ from physical interface indexes. | Preserve raw identifiers and use explicit, versioned port normalization. Never guess a physical port from a MAC alone. |
| Controller targeting can differ from the source of authentication. | Model NAS identity, management address and dynamic-authorization destination independently. |
| The Aruba CX module's Access-Accept role attribute is Aruba-User-Role, while its inspected CoA branch inserts Filter-Id. Our existing reference lab used Aruba-User-Role for CoA. | Store action-specific recipes. This is an observed difference, not a conclusion that either recipe is universally right or wrong. |
| Aruba CX emits Aruba-NAS-Filter-Rule for ACLs and explicit voice attributes; Arista uses different role/ACL attributes. | Local role references, RADIUS-carried ACL rules and voice authorization are separate capabilities, not one generic role string. |
| Cisco code has an explicit bounce-host-port action. | Session selection syntax does not establish session-only impact. Bounce is a separately gated port action. |
| Shared base code distinguishes CoA and Disconnect destination ports. | Configuration and transport must preserve that distinction; do not silently reuse one port for both. |

The developer guide includes legacy statements discouraging wired dynamic authorization while the inspected modules implement it. We use the guide for architectural history and check concrete source paths and vendor documentation for each recipe. We do not copy example configuration wholesale or infer current hardware support from a family name.

This is an original Go design informed by the sources, not a translation or copy of PacketFence implementation code. Source license headers are retained as research context; no PacketFence code is vendored.

## Decision

### 1. Separate catalog, implementation and qualification

A catalog entry may exist before a driver exists. This is the main deliverable enabled by this ADR.

Use three independent dimensions:

| Dimension | Values | Meaning |
|---|---|---|
| Research | draft / source-backed / disputed | Quality and provenance of the proposed behavior |
| Implementation | absent / partial / implemented | Whether our code contains that operation |
| Qualification, per requirement and environment | unverified / demonstrated / unsupported / inconclusive | What the evidence establishes |

A source-backed entry with absent implementation is visible for planning and documentation, but cannot be executed. Offline fixtures do not turn an integration into a hardware-qualified integration. Existing evidence stays in its existing matrix; this ADR promotes no capability.

A production-selectable operation requires an implemented driver, qualified coverage for the requested restrictions, matching device/configuration scope and authorized execution. Explicit experimental lab use is a separate deployment policy, visibly labelled and limited to approved lab targets. It never changes the catalog's qualification status.

Capabilities are scoped by vendor, family, model, OS/build, physical/virtual form, operation, authentication method, configuration fingerprint and topology. Do not automatically extend one successful build to an entire major-version range.

### 2. Keep policy out of drivers

The core decides desired access and persistent identity-scoped overrides. A strategy selector compares required restrictions with eligible capability records. A driver translates the resulting typed assignment into a vendor recipe.

Drivers cannot grant permissions, clear quarantine, approve changes, choose a weaker security profile, or write policy records. They receive resolved input; they do not call back into the policy engine.

If no qualified strategy covers the intent:
- reject new admissions when the policy requires denial;
- report unmet requirements for active sessions;
- retain the desired restriction for subsequent authentication;
- execute only an explicitly configured, eligible fail-safe action;
- report enforcement failure if no such action exists.

Returning Access-Reject for a future authentication does not disconnect an already-authorized session. There is no universally “strongest available” VLAN/ACL operation and no fallback from failed CoA to port bounce.

### 3. Distinguish four concerns

1. **Admission translation:** resolved assignment → ordered RADIUS reply attributes.
2. **Active-session change planning:** captured session + assignment → immutable CoA/Disconnect/action plan.
3. **Transport:** encode/send/verify protocol exchanges with credentials and deadlines.
4. **Observation:** retrieve and normalize reported session/port/device state with source and freshness.

Discovery is read-only metadata collection. It may suggest a driver, never silently enable it. Provisioning and configuration saving are separate from runtime enforcement.

Illustrative Go ports (contract sketch, not compiled production code; referenced domain types are to be defined):

```go
type AdmissionCompiler interface {
    CompileAdmission(AdmissionInput) (RadiusReplyPlan, error)
}

type ChangePlanner interface {
    PlanChange(ChangeInput) (DeviceActionPlan, error)
}

type DeviceObserver interface {
    Observe(context.Context, DeviceRef, ObservationQuery) (Observation, error)
}

type DeviceExecutor interface {
    Execute(context.Context, AuthorizedAction) (ExecutionReceipt, error)
}

type DriverDescriptor struct {
    ID              string
    Revision        string
    PlatformRules   []PlatformSelector
    DeclaredRecipes []RecipeDescriptor
}
```

Small optional interfaces are registered explicitly. No-op methods must not pretend to support absent capabilities. Use typed unsupported-operation errors.

`AuthorizedAction` is created inside the backend after permission, approval and precondition checks; API clients cannot construct a trusted authorization object by supplying JSON. Drivers are compiled, trusted code. No runtime Go plugins, user-uploaded scripts or arbitrary shell tools.

### 4. Go package placement and migration

Proposed packages, not existing directories:

| Package | Responsibility |
|---|---|
| `internal/domain` | Neutral device/session/assignment/plan/evidence types and ports; no vendor imports |
| `internal/networkdevice` | Registry, capability selection, planning, execution orchestration and reconciliation |
| `internal/networkdevice/drivers/aristaeos` | EOS-specific admission, CoA recipe and observation decoding |
| `internal/networkdevice/drivers/arubacx` | CX-specific role/VLAN/ACL recipes and observation decoding |
| `internal/networkdevice/transports` | Narrow HTTPS/SNMP/SSH transport adapters when actually needed |
| `internal/radius` | Reuse existing codec/exchange; extend only for concrete missing protocol needs |

Preserve inward dependencies from ADR-0002. If existing pilot types prevent immediate extraction, use a temporary wrapper in `internal/pilot`; do not migrate unrelated packages.

FreeRADIUS remains the EAP and RADIUS admission server. Go produces validated reply plans for the existing integration. Do not create a second EAP stack or parallel CoA sender.

MCP and UI call the existing backend API. Neither exposes transport credentials, raw RADIUS injection, freeform CLI nor vendor-specific authorization bypasses.

### 5. Represent assignment and protocol details precisely

An assignment may contain a VLAN, local role reference or ACL policy reference, but each combination must be explicitly supported by the recipe. Reject conflicting role/VLAN instructions.

Keep these distinct:
- local role name versus downloaded role;
- referenced ACL versus RADIUS-delivered rules versus provisioned switch configuration;
- Access-Accept versus CoA versus Disconnect versus reauthentication versus bounce;
- data domain versus voice domain;
- NAS-Port numeric value, NAS-Port-Id text, ifIndex, ifName and physical port.

Represent RADIUS attributes as an ordered list of typed values, including vendor ID/type and repeated attributes. A map of string to one string loses ACL entries and ordering. Bound values, counts and encoded packet size; reject untranslatable ACL predicates rather than dropping them. Reply values remain literal with FreeRADIUS expansion disabled.

Vendor codecs own on-wire MAC formatting. Internally retain a canonical MAC plus the original source value. Outer EAP User-Name and MAC are correlation inputs, not replacements for validated certificate identity.

### 6. Immutable targeting and operation lifecycle

A plan binds:
- operation ID, policy/profile version and desired-state generation;
- device ID, driver/recipe revision and configuration fingerprint;
- authenticated NAS identity and configured control destination;
- session key/generation, observed start time, raw NAS port and available accounting identifiers;
- selected assignment, attribute plan digest and expected impact scope;
- expected address change, reauthentication and interruption characteristics;
- evidence references, expiration and approval binding where required.

Management, RADIUS source and CoA addresses need not be equal. Resolve destinations through registered configuration and authorized NAS/controller relationships, never an arbitrary endpoint-supplied NAS-IP-Address.

Persist desired state and the pending action atomically before dispatch. Serialize changes for the same target, use compare-and-swap generations, and preserve application-level idempotency keys. Scope locking to the actual impact: a port-wide operation conflicts with operations for every session on that port.

A lost ACK is an uncertain outcome, not proof of failure to apply. Retries resend the exact request bytes to the same target within a bounded retry window. After restart or an expired window, reconcile first; do not blindly replay an old action against a replacement session. A stale target requires a new plan and authorization, not automatic retargeting.

Keep request, delivery and verification separate:
- planned / blocked / authorized / dispatched;
- ACK / NAK / timeout / transport failure;
- observed matching / mismatching / stale / unavailable;
- endpoint traffic verified / failed / not observed.

ACK does not imply matching role or permitted/denied traffic. A timeout does not clear quarantine. Restore recomputes the current authorized profile; it is not a blind rollback to an old VLAN.

### 7. Connections and credential boundaries

| Channel | Initial design position |
|---|---|
| RADIUS admission/accounting | Existing FreeRADIUS integration; registered NAS attribution; accounting freshness and reboot/session reconciliation |
| RADIUS CoA/Disconnect | Existing Go transport; distinct action recipes and configured ports; verified responses and bounded attempts |
| HTTPS API / Arista eAPI | Read-only observation first, per-device trust and credentials; mutation capability separately declared |
| SNMP | Plan SNMPv3 authPriv for read-only inventory/FDB/LLDP; SET/traps are separate capabilities and future work |
| SSH/CLI | Optional future last-mile adapter using host-key verification and fixed command builders, not generic remote shell |
| NETCONF/RESTCONF | Future transport extension only when a concrete platform requires it |
| Firewall API | Future distinct enforcement-point adapter; transit scope, IP-binding freshness and session handling differ from switch isolation |

Secrets remain references to the existing sealed-secret service, decrypted only at execution. Catalogs, dry-runs, audit records and MCP outputs contain no secrets. Do not persist shared secrets in durable RADIUS action payloads.

Per-device endpoint allowlists, TLS/SSH verification, redirect restrictions, bounded responses and credential separation apply. An authentication failure must not trigger insecure TLS, SNMPv2c or weaker credentials automatically. Existing lab exceptions are not global defaults.

Use deadlines, cancellation, per-device concurrency limits and bounded queues. Rate-limit polling and mutations separately; expose degraded observation without blocking every admission on a management API call.

### 8. Catalog schema and initial backlog

A catalog record should include schema version, stable driver ID, recipe ID/revision, platform selectors, source provenance, prerequisites, input requirements, effects, limitations, transport/credential requirements and independent implementation/qualification status.

Example, intentionally non-executable:

```yaml
schema_version: 1
driver_id: cisco.iosxe
recipe_id: role-change
research_status: draft
implementation_status: absent
qualification: unverified
execution_enabled: false
platform:
  vendor: Cisco
  family: IOS-XE
  models: []
  firmware_ranges: []
action:
  kind: coa
  target_scope: session
  effect_scope: unknown
  changes_address: unknown
  requires_reauthentication: unknown
sources:
  - kind: upstream_implementation
    repository: inverse-inc/packetfence
    revision: 1041248cc6856d99f66dd6eed01f36bf2186e058
    path: lib/pf/Switch/Cisco.pm
required_followup:
  - Pin exact Catalyst model and firmware
  - Verify authorization and CoA attributes independently
  - Establish shared-port and address-family coverage
```

This is a schema illustration, not a claim that this generic recipe is suitable for any Cisco device. Reject unknown enum values; unknown effect scope blocks runtime selection.

| Family | Research scope now | Planned path, not a support claim |
|---|---|---|
| Arista EOS | Module and current NAC implementation inspected | Preserve current VLAN/CoA and eAPI path; role/ACL extensions need separate evidence |
| Aruba AOS-CX | Module inspected; reference-lab evidence exists separately | Align ongoing local-role integration; do not replace its demonstrated CoA recipe with PacketFence's different recipe without evidence |
| Cisco Catalyst IOS/IOS-XE | Cisco base module inspected; model coverage not established | Per-model RADIUS/ACL/reauth recipes, read-only inventory; bounce separately gated |
| Juniper EX/Junos | PacketFence guide reviewed at family level | Separate legacy/ELS/version profiles; exact module and vendor-source review still required |
| FortiSwitch | PacketFence guide reviewed at family level | Distinguish standalone and controller-managed deployment before designing target/control paths |
| Wireless controllers/APs | Architectural extension point only | Explicit AP/controller relationship and bridged/tunneled scope; no wired capability inheritance |
| Firewall integrations | Architectural extension point only | Separate contract for identity/IP mapping and transit enforcement; not DHCP renewal or L2-isolation proof |

No fabricated catalog entries for every PacketFence-supported vendor. Add families when a concrete deployment needs them. A compatibility screen must distinguish “planned integration” from “supported and qualified.”

### 9. Dry-run and operational semantics

Dry-run is local plan construction with no socket connection, SNMP query, RADIUS packet, configuration push or credential use. It returns:
- selected recipe or why none is eligible;
- redacted attributes and resolved target identity;
- requirements satisfied, unmet and unknown;
- shared-port impact and known address/interruption effects;
- preconditions, required evidence and approval status.

A read-only live probe is a different, explicitly requested operation. Historical replay is not equipment validation.

Onboarding must separate management reachability, global authenticator enablement, port authenticator state, RADIUS source/VRF configuration, EAPOL/session establishment and dynamic authorization. Show missing evidence, not a single green “connected” badge.

Observe authentication state and active role independently of LLDP/FDB inventory. LLDP phone identification is useful topology evidence, not authenticated identity or proof that a port has only one endpoint. Cache port mappings with device boot/configuration generations and invalidate them on change.

Emit phase timing with defined clocks: API receipt, plan authorization, queue wait, dispatch, ACK, switch observation and optional external traffic evidence. Do not compare post-ACK lab bounds with end-to-end product latency. No universal vendor latency promise is introduced.

### 10. What is authorized now and later

**Now:** research, this ADR and source references only. No new Go implementation, equipment contact, scans, RADIUS traffic, EVE runs, configuration pushes or product support promotion were performed for this task. Existing independently authorized Aruba work continues separately.

**After design acceptance:** implement the minimal ports around existing Arista/Aruba code, using offline fixtures and mocks. Do not build all transport packages in advance.

**Before qualification:** ASTRA runs a separately authorized exact-candidate matrix: authentication, wrong identity, stale target, duplicate/NAK/timeout, session replacement/restart, IPv4/IPv6/UDP, new/established flows, DHCP renewals and shared voice/data ports. Unit tests and fake servers establish code behavior only.

**Before production enablement:** record vendor/model/firmware and configuration scope, observed restrictions, recovery behavior and limitations. Do not require a new lab merely to add a disabled research catalog entry.

## Consequences and alternatives

Benefits: vendor additions are localized; speculative research can be retained safely; API/UI/MCP share the same truthful capability model; future Culvert distribution does not own NAC policy.

Costs: explicit recipes and evidence require maintenance; model/version matching is more work than a vendor boolean; operation recovery needs durable state and reconciliation.

Rejected:
- Translating PacketFence's inheritance tree directly to one giant Go Switch interface.
- Generic arbitrary RADIUS/CLI templates accessible to operators or AI.
- Treating all vendors as interchangeable standard RADIUS targets.
- Shipping a long list of unqualified integrations as supported.
- Calling every control action “disconnect” or treating bounce as automatic recovery.
- Building a full orchestration/plugin framework before the current two drivers need it.

Open design details for implementation review: exact package extraction boundaries with ongoing Aruba work; persistence schema for recipes and evidence; future approved per-site qualification override policy. None blocks saving this proposed ADR.

## Sources and traceability

Reviewed 2026-11-09. PacketFence source links are pinned; online manuals can change.

1. [PacketFence network-device guide, v15.2.0](https://www.packetfence.org/doc/PacketFence_Network_Devices_Configuration_Guide.html) — device families and configuration prerequisites; not our support matrix.
2. [Supporting new network hardware](https://github.com/inverse-inc/packetfence/blob/1041248cc6856d99f66dd6eed01f36bf2186e058/docs/developer/supporting_new_network_hardware.asciidoc) — extension architecture, port translation and controller relationships; includes legacy advice.
3. [Base switch module](https://github.com/inverse-inc/packetfence/blob/1041248cc6856d99f66dd6eed01f36bf2186e058/lib/pf/Switch.pm) — mappings, dynamic authorization, transport helpers and capability-related behavior.
4. [Aruba CX module](https://github.com/inverse-inc/packetfence/blob/1041248cc6856d99f66dd6eed01f36bf2186e058/lib/pf/Switch/Aruba/ArubaOS_CX_10_x.pm) — separate admission/CoA role handling, ACL and voice handling.
5. [Arista module](https://github.com/inverse-inc/packetfence/blob/1041248cc6856d99f66dd6eed01f36bf2186e058/lib/pf/Switch/Arista/AristaSwitch.pm) — role/ACL attributes and port normalization.
6. [Cisco base module](https://github.com/inverse-inc/packetfence/blob/1041248cc6856d99f66dd6eed01f36bf2186e058/lib/pf/Switch/Cisco.pm) — vendor action recipes including bounce-host-port.
7. [NAC contract snapshot](https://github.com/KidCarmi/nac/tree/162e4eeaf99443927b431e7d7ab29cb9312b6a22) — ADRs and existing Go integration anchors.
8. [Owner-approved bounded Aruba increment](https://github.com/KidCarmi/nac/pull/1#issuecomment-6747504651) — separate implementation scope; this ADR does not supersede that work.

Research limitation: representative integration architecture and four source modules were inspected, not the entire PacketFence driver catalog. No new platform or firmware compatibility has been validated.
