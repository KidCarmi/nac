# Access-Decision Incident

A device is being **wrongly allowed, denied, or quarantined**. This runbook
diagnoses and corrects it. Access decisions in `nacd` are **deterministic and
explainable** — there is no randomness or hidden state — so a wrong decision is
almost always a **policy-configuration issue** (priority, conditions, enabled
flag, default action) or a stale endpoint record, not a code bug.

Related: [Business Logic](../docs/architecture/business-logic.md),
[Administrator Guide](../docs/guides/administrator.md),
[Incident Response](../docs/operations/incident-response.md),
[Dependency Outage](dependency-outage.md).

## Decision model (you must know this to diagnose)

- Policies are evaluated **first-match-wins by ascending `priority`** (lowest
  priority number is considered first).
- A policy matches only if **all** its conditions are true (conditions are
  AND-ed). The engine matches against endpoint fields/attributes including
  `posture` (`unknown|compliant|at_risk|noncompliant`).
- Only **enabled** policies participate.
- If **no** policy matches, the engine applies the **default action**
  (`NAC_DEFAULT_ACTION`, which defaults to **deny** — fail closed).
- **Unknown operators fail closed** (a condition the engine cannot evaluate does
  not silently pass).

So a decision is fully explained by: the first enabled policy whose conditions
all matched, or the default action if none did.

## Severity

- A single device wrong: **SEV3**, unless it is high-impact.
- A class of devices or a whole segment wrong (mass mis-authorization):
  **SEV1** — the security control has failed at scale. Engage the IC.

## Step 1 — Reproduce and read the explanation

Re-run the exact decision for the affected MAC against the live service. The
evaluate path uses the **static service token** (Bearer), not a JWT.

```bash
curl -s -X POST http://<host>/api/v1/access/evaluate \
  -H "Authorization: Bearer $NAC_SERVICE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"mac":"AA:BB:CC:DD:EE:FF"}' | jq '{action, matched_policy_id, reason}'
```

The response is self-explaining:

- `action` — what was decided (`allow`/`deny`/`quarantine`).
- `matched_policy_id` — which policy won, or absent/null if the **default
  action** was applied (no policy matched).
- `reason` — human-readable explanation of the match.

If the request body can carry posture/attributes, reproduce with the **same
inputs** the enforcement point sent. If `posture` was supplied in the request,
include it — a different posture changes which conditions match.

## Step 2 — Diagnose

### Case A — a policy matched but it is the wrong one

`matched_policy_id` points at a policy you did not expect to win. Inspect policy
ordering and conditions:

```bash
curl -s http://<host>/api/v1/policies \
  -H "Authorization: Bearer <OPERATOR_JWT>" \
  | jq 'sort_by(.priority) | .[] | {id, priority, enabled, action, conditions}'
```

Look for:
- A **lower-priority-number** (earlier) policy that matches too broadly and
  shadows the intended one (first-match-wins).
- The intended policy **disabled**, or **higher** in priority number so an
  earlier rule wins first.
- Conditions that are too loose/tight given the endpoint's actual attributes.

### Case B — no policy matched → default action applied

`matched_policy_id` is null/absent. The decision came from `NAC_DEFAULT_ACTION`
(default **deny**). Either an intended policy's conditions did not match the
endpoint, or it is disabled, or there simply is no rule for this device.

```bash
kubectl -n nac get configmap nacd-config -o yaml | grep -i NAC_DEFAULT_ACTION
```

### Case C — endpoint record is stale/wrong

The policy logic is correct but the **endpoint's** attributes (notably
`posture`) are wrong, so the wrong conditions matched.

```bash
curl -s "http://<host>/api/v1/endpoints?mac=AA:BB:CC:DD:EE:FF" \
  -H "Authorization: Bearer <OPERATOR_JWT>" \
  | jq '{mac, posture, attributes}'
```

If `posture` is stale (e.g. still `unknown` or `at_risk` after remediation),
the fix is to correct the endpoint, not the policy. Posture is set by
operators/integrations or supplied in the evaluate request.

### Step 3 (corroborate) — audit log

Every decision is recorded. Use the audit log to see what actually happened and
confirm your reproduction matches production:

```bash
curl -s "http://<host>/api/v1/audit?mac=AA:BB:CC:DD:EE:FF" \
  -H "Authorization: Bearer <OPERATOR_JWT>" | jq '.[0:10]'
```

Correlate with logs via `request_id`/`trace_id` if you need the full request
context for a specific decision.

## Step 4 — Remediation

Pick the fix that matches the diagnosis. **Policy mutations automatically
invalidate the Redis cache** (key `nac:policies:enabled`), so changes take
effect immediately — no manual cache flush.

```bash
# Fix the offending policy (correct conditions / enable / disable):
curl -s -X PATCH http://<host>/api/v1/policies/<id> \
  -H "Authorization: Bearer <OPERATOR_JWT>" -H 'Content-Type: application/json' \
  -d '{"enabled": true}'

# Or adjust priority so the correct policy wins (lower number = earlier).
curl -s -X PATCH http://<host>/api/v1/policies/<id> \
  -H "Authorization: Bearer <OPERATOR_JWT>" -H 'Content-Type: application/json' \
  -d '{"priority": 10}'

# Or correct the endpoint record (e.g. fix posture):
curl -s -X PATCH "http://<host>/api/v1/endpoints/<id>" \
  -H "Authorization: Bearer <OPERATOR_JWT>" -H 'Content-Type: application/json' \
  -d '{"posture": "compliant"}'
```

For a **mass** incident where you cannot immediately craft the right rule, lean
on the safe default: default-deny means the failure mode is "device can't get
on," not "untrusted device admitted." Prefer temporarily **disabling** an
over-broad allow policy over leaving wrong allows in place. Use priority
adjustments to insert a corrective rule ahead of the offending one.

## Step 5 — Verify

Re-run the exact evaluate from Step 1 and confirm the decision is now correct:

```bash
curl -s -X POST http://<host>/api/v1/access/evaluate \
  -H "Authorization: Bearer $NAC_SERVICE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"mac":"AA:BB:CC:DD:EE:FF"}' | jq '{action, matched_policy_id, reason}'
```

Confirm `action` is what you expect and `matched_policy_id` points at the
intended policy (or default action for the intended fall-through). Spot-check a
few representative devices for a class-wide change, and watch
`nac_access_decisions_total{action}` for the allow/deny/quarantine mix
returning to expected levels.

## Escalation

- Mass mis-authorization (class of devices / segment wrong): **SEV1** — engage
  the IC per [Incident Response](../docs/operations/incident-response.md).
- Single device, low impact: **SEV3** — fix via the steps above and close.
- If decisions are *erroring* (not merely wrong) or `/readyz` is red, this is a
  service/dependency problem, not a policy one — go to
  [Service Degraded](service-degraded.md) / [Dependency Outage](dependency-outage.md).
