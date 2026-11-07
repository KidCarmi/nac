# Business Logic & Domain Model

This document explains *what* the platform does and the rules that govern it,
independent of *how* it is implemented. It is the authoritative description of
the domain.

## 1. Problem statement

When a device connects to an enterprise network, the organization must decide:

1. **Should this device be allowed on the network at all?**
2. **If so, with what level of access** (which network segment / VLAN)?

These decisions must be **consistent**, **explainable** (auditable), and
**fast** (every device authentication triggers one). The NAC platform
centralizes the policy that answers these questions and exposes a decision
service that network enforcement points consult.

## 2. Domain entities

### Endpoint
A device observed on the network, uniquely identified by its **MAC address**
(its identity; immutable once created). Key attributes:

- `access_status` — current authorization state: `unknown` (default for newly
  observed devices), `authorized`, `quarantined`, `denied`.
- `posture` — compliance state reported by posture assessment: `unknown`,
  `compliant`, `at_risk`, `noncompliant`.
- `attributes` — free-form key/value metadata (e.g. owner, location) usable in
  policy conditions under the `attr.` namespace.

### Policy
An **ordered access-control rule**. Fields:

- `priority` — lower number = higher precedence (evaluated first).
- `enabled` — disabled policies are skipped entirely.
- `conditions` — zero or more predicates; **all** must match (logical AND).
- `action` — `allow`, `deny`, or `quarantine`.
- `segment` — the network segment assigned when the policy matches.

A policy with **no conditions** is a catch-all (always matches) — used for
default/fallback rules.

#### Conditions
Each condition references a **field** resolved from the endpoint and applies an
**operator** to comparison **values**:

Every input is **evidence**: a value plus the source that asserted it, when it
was observed, and a status of `present`, `missing` or `stale`. The full list of
fields and their allowed sources is `domain.InputCatalog`, served at
`GET /api/v1/policy-inputs`. Validation rejects any other field (except
`attr.<key>`), so a typo can't create a rule that silently never matches.

| Field key | Allowed source |
|-----------|----------------|
| `identity.*` | `eap-tls`: certificate validated by FreeRADIUS |
| `identity.bound`, `mac.matches_device` | `nac-binding`: operator identity binding |
| `mac`, `nas.switch`, `nas.port` | `nas`: switch-asserted; authenticates the switch, not the device |
| `ip`, `hostname`, `device_type`, `attr.<key>` | `endpoint-record`: operator inventory |
| `posture` | `posture`: endpoint API, with observation time; never from RADIUS |
| `session.override` | `operator`: explicit session quarantine |

Conditions are **three-valued**:

| Operator | Present evidence | Missing or stale evidence |
|----------|------------------|---------------------------|
| `eq` | equals the single value | insufficient evidence |
| `ne` | differs from the value | insufficient evidence |
| `in` | equals one of the values | insufficient evidence |
| `exists` | match | no match |
| `absent` | no match | match |

A rule applies only if **every** condition matches. A definite no-match makes
the rule `not_matched`. Otherwise, any insufficient-evidence condition makes it
`insufficient_evidence`: the rule is **skipped**, and the decision lists the
missing fields. Missing evidence therefore can never satisfy an allow rule. To
act on missing evidence on purpose, write an explicit rule such as
`posture absent → quarantine`. Posture `unknown` counts as missing. Unknown
operators **fail closed** (never match).

> Changed in the pilot: before, `ne` matched when the field was absent, so
> `posture ne noncompliant → allow` admitted devices that had no posture at all.

### Operator
An administrative user of the platform with a `role` (`admin`, `operator`,
`viewer`). Operators authenticate to manage the system. Credentials are stored
as Argon2id hashes and never serialized.

### Audit event
An **append-only** record of a security-relevant action (who, what, when,
outcome, correlation id). Never updated or deleted.

## 3. The decision rules

Given an endpoint and the set of **enabled** policies:

1. Sort policies by ascending `priority` (ties broken by name for determinism).
2. Evaluate in order. The **first** policy whose conditions all match wins.
3. The winning policy's `action` and `segment` form the decision.
4. If **no** policy matches, apply the **default action** (configured;
   secure default is `deny`).

The action maps to an access status: `allow → authorized`,
`quarantine → quarantined`, `deny → denied`.

Every decision is **deterministic** and **self-explaining**: it records the
matched policy id (if any) and a human-readable reason. The engine is pure —
the same inputs always produce the same output — which makes decisions
reproducible during audits and incident investigations.

### Worked example

Policies (enabled):

| Priority | Name | Conditions | Action | Segment |
|---------:|------|-----------|--------|---------|
| 10 | allow-compliant-laptops | `device_type eq laptop` **AND** `posture eq compliant` | allow | corp |
| 20 | quarantine-noncompliant | `posture eq noncompliant` | quarantine | remediation |
| 1000 | default-quarantine | *(none)* | quarantine | guest |

- A **compliant laptop** → matches #10 → **allow / corp**.
- A **noncompliant laptop** → #10 fails (posture), #20 matches → **quarantine / remediation**.
- An **unknown printer** → #10, #20 fail, #1000 catch-all → **quarantine / guest**.
- With no catch-all and no match → **default action** (`deny`).

## 4. Lifecycle rules & invariants

- A newly created endpoint starts as `access_status = unknown` — it is never
  implicitly trusted.
- An endpoint's MAC is immutable; "changing" it is a delete + create.
- Policy mutations (create/update/delete) **invalidate the policy cache** so
  decisions reflect changes promptly.
- Validation is enforced in the **service layer**, so every entry path (HTTP
  today, CLI/gRPC tomorrow) gets identical guarantees. The database additionally
  enforces enums via CHECK constraints (defense in depth).

## 5. Authorization model (RBAC)

| Capability | admin | operator | viewer |
|-----------|:-----:|:--------:|:------:|
| Read endpoints/policies/audit | ✓ | ✓ | ✓ |
| Write endpoints/policies | ✓ | ✓ | |
| Evaluate access | ✓ | ✓ | |
| Manage operators | ✓ | | |

Unknown roles grant nothing (fail closed). The matrix is defined statically in
`internal/auth/rbac.go` so the security model is reviewable in one place.

## 6. Abuse cases (and mitigations)

| Abuse case | Mitigation |
|-----------|------------|
| Attacker spoofs a MAC to inherit an authorized device's access | Policies should combine identity with posture; MAC alone is never sufficient for sensitive segments. Default-deny limits blast radius. |
| Flood of evaluation requests | Per-client rate limiting; cached hot path; bounded body size. |
| Operator privilege escalation | RBAC enforced per route; operator management is admin-only; all mutations audited. |
| Credential stuffing on login | Argon2id (slow by design), generic 401s, audit of failures, rate limiting. |
| Malformed/oversized policy to break the engine | Strict validation (operator/value-count rules); unknown operators fail closed; engine never panics on bad data. |

See the [Threat Model](../security/threat-model.md) for the full analysis.
