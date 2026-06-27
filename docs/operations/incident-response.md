# Incident Response

This document defines how the team responds to operational incidents affecting
the NAC platform: how incidents are classified, who does what, the lifecycle
from detection to postmortem, and how to use the platform's correlation
identifiers to investigate quickly. It complements the recovery mechanics in
[disaster-recovery.md](disaster-recovery.md) and the diagnostic detail in
[observability.md](observability.md).

## Severity levels

| Severity | Definition | Examples |
|----------|------------|----------|
| **SEV1** | Platform down, or **mass mis-authorization** | Evaluate path returning errors fleet-wide; `/readyz` red across replicas (database unreachable); a policy or engine fault causing widespread wrong ALLOW/DENY decisions |
| **SEV2** | Major degradation, limited blast radius, or at-risk redundancy | Elevated 5xx or latency on the evaluate path; Redis down forcing all reads to the database; replicas degraded but service up; sustained auth failures |
| **SEV3** | Minor or cosmetic; no material user impact | Single transient error spike that self-recovered; non-urgent metric anomaly; degraded non-critical functionality |

SEV1 explicitly includes **mass mis-authorization** — incorrectly allowing or
denying network access at scale — not just total unavailability. A platform that
is up but making systematically wrong access decisions is a SEV1, because the
security control it exists to enforce has failed.

## Roles

| Role | Responsibility |
|------|----------------|
| **Incident Commander (IC)** | Owns the incident: drives the lifecycle, makes decisions, delegates. Single point of accountability. |
| **Operations / Responder** | Performs investigation and mitigation actions (scaling, restore, failover, rollback). |
| **Communications Lead** | Owns internal and external updates; frees the IC to focus on resolution. |
| **Scribe** | Maintains the timeline of facts, actions, and decisions for the postmortem. |

For a small incident, one person may hold several roles; for a SEV1, separate
them so no single person is both commanding and head-down debugging.

## Incident lifecycle

```
detect -> triage -> mitigate -> resolve -> postmortem
```

### 1. Detect

Incidents surface from alerts on the platform's metrics (see
[observability.md](observability.md)) or from user reports. Key signals:

- `nac_http_requests_total{status="5xx"}` rising on `route="/api/v1/access/evaluate"`.
- `nac_http_request_duration_seconds` p99 climbing on the evaluate route.
- `nac_access_decisions_total{action}` shifting unexpectedly (a sudden swing in
  the allow/deny mix can indicate mis-authorization — a SEV1 candidate).
- `nac_auth_failures_total{reason}` spiking (credential, token, or RBAC issues).
- `/readyz` failing (database unreachable); `/healthz` distinguishes a live but
  unready process from a dead one.

### 2. Triage

- **Assign a severity** using the table above and **name an IC**.
- **Establish blast radius:** one route or fleet-wide? One replica or all? Is the
  database reachable (`/readyz`)? Is Redis up?
- **Open the incident record** and start the timeline.

### 3. Mitigate

Stabilise first; root cause can wait. Typical mitigations:

- **Database unreachable:** follow the failover steps in
  [disaster-recovery.md](disaster-recovery.md). `/readyz` is the authoritative
  signal for recovery.
- **Redis down:** confirm the fail-open behaviour is holding (evaluations
  continue against the database) and watch DB read load; Redis loss is a
  degradation, not an outage.
- **Bad policy change (mis-authorization):** revert the offending policy
  mutation; the mutation invalidates the Redis cache so the corrected enabled
  set takes effect on the next evaluation. For data-level corruption, consider
  PITR to just before the change (see
  [disaster-recovery.md](disaster-recovery.md)).
- **Bad release:** roll back the deployment per the deployment runbook (see
  [release-process.md](release-process.md)).
- **Single-replica fault:** let the Deployment/HPA replace it; the PDB keeps
  `minAvailable: 2` during the churn.

### 4. Resolve

Declare resolved only when the user-facing symptom is gone and the signals have
returned to baseline. Confirm `/readyz` is green across replicas, the evaluate
error rate and latency are normal, and the decision mix matches expectations.
Capture the final timeline entry.

### 5. Postmortem

Every SEV1 and SEV2 gets a **blameless** postmortem (template below). SEV3 gets a
postmortem at the IC's discretion.

## Investigating from request_id / trace_id

The platform's correlation model (see [observability.md](observability.md)) is
the fastest route from a symptom to the relevant records. Every request carries
W3C Trace Context: the inbound `traceparent` is parsed (or generated when
absent), the **span id is used as the `request_id`**, and that value is echoed in
the `X-Request-Id` response header, in structured logs, and in audit records.

A typical investigation:

1. Obtain the `request_id` / `trace_id` from the affected client's
   `X-Request-Id` response header or from an alert.
2. Pivot into the JSON logs (`log/slog`, on stderr) filtering on that
   `request_id`/`trace_id` to reconstruct exactly what the request did.
3. Cross-reference the `audit_events` row written for the decision or mutation,
   which carries the same `request_id`, to see the recorded outcome.

This single identifier ties together the response the client saw, the log lines
the server emitted, and the durable audit record — which is precisely what a
mis-authorization investigation needs.

## Communication

- **Internal:** the Communications Lead posts regular status updates on a
  predictable cadence (more frequent for SEV1), summarising impact, current
  actions, and next update time.
- **External/stakeholder:** for SEV1/SEV2 with user impact, send concise,
  factual updates — impact, mitigation in progress, and expected next update —
  without speculating on cause.
- **Handoffs:** if an incident spans shifts, the IC role is formally handed off
  and recorded in the timeline.

## Blameless postmortem template

Conduct postmortems **blamelessly**: focus on systems and contributing factors,
not individuals. People act reasonably given the information and tools they had;
the goal is to improve the system.

```markdown
# Postmortem: <short title>

- Date / duration:
- Severity: SEV1 | SEV2 | SEV3
- Incident Commander:
- Authors:

## Summary
One paragraph: what happened and the impact.

## Impact
Who/what was affected, for how long, and how it was measured
(e.g. evaluate error rate, decisions affected, duration).

## Timeline (UTC)
- HH:MM  detection (alert / report, with request_id/trace_id if known)
- HH:MM  triage and severity assignment
- HH:MM  mitigation actions
- HH:MM  resolution confirmed

## Root cause
The contributing factors and how they combined. Avoid single "human error".

## Detection
How we found out, and how detection could be faster next time.

## Resolution
What actually fixed it.

## What went well / what went poorly

## Action items
| Action | Owner | Due | Tracking |
|--------|-------|-----|----------|
```

## Related documents

- [observability.md](observability.md) — metrics, logs, and the correlation
  model.
- [disaster-recovery.md](disaster-recovery.md) — recovery and failover
  procedures referenced above.
- [release-process.md](release-process.md) — rollback and the deployment
  runbook.
