# RFC 0001: External posture-assessment integration

- **Status:** Draft
- **Author(s):** Platform / SRE
- **Created:** 2026-06-27
- **Tracking ADRs:** (filled in on acceptance)

## Summary

Define a standard way for `nacd` to ingest endpoint **posture** from external
assessment systems (MDM, EDR, vulnerability scanners) and keep each endpoint's
`posture` field current. The policy engine already supports `posture` in
conditions (`unknown|compliant|at_risk|noncompliant`); today that field is set
ad hoc by operators or supplied per-request in the evaluate body. This RFC
proposes a first-class, authenticated ingestion path with defined mapping,
freshness semantics, observability, and audit, so posture-driven policies can be
trusted in production.

## Motivation

Posture is one of the most useful conditions a NAC policy can express — "allow
only compliant endpoints onto the corporate segment; quarantine non-compliant
ones." Today there is no supported way to keep posture fresh:

- Operators can set `posture` manually, but that does not scale and goes stale.
- Callers can pass `posture` in the `POST /api/v1/access/evaluate` body, but
  that trusts the enforcement point to assert a security-relevant fact and gives
  no persistent record between evaluations.

Authoritative posture lives in external assessment systems (MDM/EDR). We need a
defined integration so that posture in the endpoint record reflects what those
systems actually report, with known freshness and a clear trust boundary —
otherwise posture-based policies are only as good as a hand-maintained field.
The status quo is insufficient for any deployment that wants posture to gate
access reliably.

## Detailed design

### Ingestion model

Support **two complementary modes**, both feeding the same internal
"apply posture update" operation:

1. **Push (webhook).** A new authenticated endpoint, `POST /api/v1/posture`,
   accepts posture reports from external systems (or a thin connector that
   normalizes a vendor webhook). Suited to event-driven sources (EDR raising an
   alert). Request body (illustrative):

   ```json
   {
     "endpoint": { "mac": "AA:BB:CC:DD:EE:FF" },
     "posture": "noncompliant",
     "source": "crowdstrike-edr",
     "assessed_at": "2026-06-27T12:00:00Z",
     "evidence": { "failed_checks": ["disk_encryption"] }
   }
   ```

2. **Poll (poller).** A scheduled reconciler that queries the source's API for
   the current posture of known endpoints and applies deltas. Suited to systems
   without reliable webhooks and as a backstop that corrects missed webhooks.

Both modes converge on one idempotent internal operation keyed by endpoint
identity (MAC, extensible to other identifiers), so push and poll cannot
conflict beyond last-writer-by-`assessed_at`.

### Mapping external posture → `posture` enum

Each integration declares a **mapping** from its native posture/compliance
output to the engine enum:

| Engine value | Typical source meaning |
|--------------|------------------------|
| `compliant` | All required checks pass |
| `at_risk` | Passing but with warnings / minor findings |
| `noncompliant` | Failing a required control |
| `unknown` | No data, source unreachable, or unmapped value |

Any value the mapping does not cover maps to `unknown` (fail-safe), which —
combined with default-deny — means missing/garbled posture data tends toward
*denying* access, not silently allowing it. Mappings are configuration, not
code, so a new source is onboarded without a release.

### Freshness / TTL of posture

Posture is time-sensitive. Each endpoint posture record carries `assessed_at`
and `source`. A configurable **TTL** (e.g. `NAC_POSTURE_TTL`, default a few
hours) defines when a posture is considered **stale**. On TTL expiry, posture
**decays to `unknown`** rather than retaining a possibly-false `compliant`. This
makes the failure mode conservative: a quiet integration causes endpoints to
become `unknown` (and, under default-deny, lose access) rather than coasting on
a stale `compliant`. The decay can be implemented by the poller and/or evaluated
lazily at decision time.

### How it updates endpoint records

- The apply operation **upserts** the endpoint's `posture` (and posture
  metadata: `source`, `assessed_at`) in PostgreSQL, the system of record.
- It uses **last-writer-wins by `assessed_at`** so an out-of-order or
  late-arriving stale report cannot overwrite a fresher one.
- Posture is endpoint state, not policy. Updating posture therefore does **not**
  invalidate the enabled-policy cache (`nac:policies:enabled`); that cache is
  unaffected by endpoint mutations. Decisions read current endpoint posture from
  the DB as they do today.
- The request-body `posture` override on `POST /api/v1/access/evaluate` remains
  supported for backward compatibility; precedence between a request-supplied
  posture and the stored posture is an open question (see below).

### Security of the integration credential

- Posture is security-relevant input, so the ingestion path is **authenticated**
  and must sit on the trusted side of the
  [trust boundary](../docs/security/trust-boundaries.md). It does **not** reuse
  the operator JWT or the existing `NAC_SERVICE_TOKEN`; it gets a **dedicated,
  separately-rotatable integration credential** per source (so one compromised
  connector can be revoked without disrupting evaluation or operator access).
- Credentials follow the same handling as other secrets: stored in the secrets
  manager, delivered via `nacd-secrets`, redacted in logs, rotated per the
  [Secret Rotation runbook](../runbooks/secret-rotation.md).
- Treat posture reports as untrusted input: validate enum/timestamp, rate-limit,
  and never let a malformed report flip an endpoint to a more-permissive posture
  by accident (unmapped → `unknown`, not `compliant`).

### Observability

- Metrics: ingestion volume and outcome (accepted/rejected/stale), per `source`;
  count of endpoints currently at each `posture`; count decayed to `unknown` on
  TTL expiry; poller run duration and error count.
- Logs: each apply emits a structured line with `request_id`/`trace_id`,
  endpoint id, old→new posture, `source`, and `assessed_at`.

### Audit

Every posture change is written to the audit log (the same surface as
`GET /api/v1/audit`), recording who/what changed posture (`source`/integration
identity), from/to values, and `assessed_at`, so a later access-decision
investigation can explain *why* an endpoint had the posture it did. This ties
directly into the [Access-Decision Incident runbook](../runbooks/access-incident.md).

## Drawbacks & risks

- **New trust input.** Posture now flows from external systems; a compromised or
  buggy connector could mislabel endpoints. Mitigated by per-source credentials,
  conservative unmapped→`unknown` mapping, and audit.
- **Operational coupling.** A posture source outage causes endpoints to decay to
  `unknown` and (under default-deny) lose access — correct fail-safe behavior,
  but a real availability consideration that must be communicated.
- **Added surface area.** A new authenticated endpoint and/or poller to operate,
  secure, and monitor.
- **Clock skew.** Last-writer-by-`assessed_at` depends on reasonable source
  clocks; badly skewed sources could mis-order updates.

## Alternatives

- **Keep request-body posture only.** No new surface, but trusts the enforcement
  point and keeps no durable, fresh record — rejected as insufficient.
- **Operator-only manual posture.** Status quo; does not scale and goes stale.
- **Push-only or poll-only.** Push alone misses dropped webhooks; poll alone
  adds latency to urgent EDR events. Supporting both is more robust.
- **External posture as a per-request lookup at decision time** (call the source
  synchronously during evaluate). Rejected: adds an external dependency to the
  hot evaluate path and risks latency/availability regressions; ingestion +
  cached endpoint state keeps decisions fast and local.

## Unresolved questions

- Precedence: when a request supplies `posture` *and* a stored posture exists,
  which wins? (Proposed: stored, unless explicitly overridden by a privileged
  caller — to be decided.)
- Default `NAC_POSTURE_TTL` value and whether it should be per-source.
- Endpoint identity beyond MAC (e.g. device certificate, asset id) for sources
  that do not report MAC.
- Whether decay-to-`unknown` is enforced eagerly (poller writes) or lazily
  (computed at decision time), or both.
- Standard connector packaging for common vendors vs. a generic webhook contract
  that customers adapt to.
