# User Manual

This manual is for **operators** and **viewers** who use the NAC platform's REST
API day to day: authenticating, managing endpoints, authoring policies,
understanding how access is evaluated, reading the audit log, and interpreting
errors and pagination. It is task-oriented and full of runnable `curl`
examples.

If you administer or deploy the platform, see the
[Administrator Guide](administrator.md). If you build it, see the
[Developer Guide](developer.md).

Throughout, the base URL is assumed to be `http://localhost:8080`:

```bash
BASE=http://localhost:8080
```

---

## 1. Roles and what you can do

Your role is fixed by an administrator and determines what the API allows.
Authorization is **fail-closed**: anything not granted is denied.

| Capability | `admin` | `operator` | `viewer` |
|------------|:------:|:----------:|:--------:|
| Read endpoints / policies | yes | yes | yes |
| Write (create/update/delete) endpoints / policies | yes | yes | — |
| Evaluate access | yes | yes | — |
| Read audit log | yes | yes | yes |
| Manage operators | yes | — | — |

An **unknown role gets nothing**. If you receive `403 Forbidden`, your role
lacks the permission for that action.

> The `POST /api/v1/access/evaluate` endpoint is **not** used by human operators
> — it is called by network enforcement points with a service token. It is
> described in §5 so you understand the system, but you authenticate to it
> differently (see that section).

---

## 2. Authenticate

Exchange your email and password for a JWT bearer token:

```bash
TOKEN=$(curl -sS -X POST "$BASE/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"your-password"}' \
  | jq -r .token)
```

The response is:

```json
{ "token": "<jwt>", "expires_at": "2026-06-27T13:00:00Z", "role": "operator" }
```

Send the token on every operator request:

```bash
curl -sS "$BASE/api/v1/endpoints" -H "Authorization: Bearer $TOKEN"
```

A bad email or password returns a **generic `401`** with no indication of which
was wrong — this prevents user enumeration. When `expires_at` passes, log in
again.

---

## 3. Manage endpoints

An **endpoint** is a network device keyed by its MAC address.

| Field | Values |
|-------|--------|
| `mac` | required; normalized to lowercase colon form; **immutable** after creation |
| `ip`, `hostname`, `device_type` | free-form strings |
| `access_status` | `unknown` \| `authorized` \| `quarantined` \| `denied` |
| `posture` | `unknown` \| `compliant` \| `at_risk` \| `noncompliant` |
| `attributes` | a map of custom key/values |

### 3.1 Create

```bash
curl -sS -X POST "$BASE/api/v1/endpoints" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "mac": "AA:BB:CC:11:22:33",
    "ip": "10.0.12.50",
    "hostname": "laptop-jdoe",
    "device_type": "laptop",
    "posture": "compliant",
    "attributes": { "owner": "jdoe", "department": "eng" }
  }'
```

Only `mac` is required; the rest are optional.

### 3.2 List, filter, and search

```bash
# Plain paginated list
curl -sS "$BASE/api/v1/endpoints?limit=50&offset=0" -H "Authorization: Bearer $TOKEN"

# Filter by access_status
curl -sS "$BASE/api/v1/endpoints?access_status=quarantined" -H "Authorization: Bearer $TOKEN"

# Prefix-search by MAC or hostname
curl -sS "$BASE/api/v1/endpoints?q=laptop" -H "Authorization: Bearer $TOKEN"
```

`q` matches a **prefix** of the MAC or hostname. Pagination rules are in §7.

### 3.3 Get, update, delete

```bash
curl -sS "$BASE/api/v1/endpoints/{id}" -H "Authorization: Bearer $TOKEN"

# Update — note that MAC cannot be changed
curl -sS -X PUT "$BASE/api/v1/endpoints/{id}" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{ "posture": "at_risk", "hostname": "laptop-jdoe" }'

curl -sS -X DELETE "$BASE/api/v1/endpoints/{id}" -H "Authorization: Bearer $TOKEN"
```

Attempting to change `mac` on update is rejected. Write operations require the
`operator` or `admin` role.

---

## 4. Author policies

A **policy** is an ordered rule the engine evaluates to decide what to do with a
device. Understanding four concepts lets you author correctly.

### 4.1 Priority

`priority` is an integer **>= 0** where **lower means higher precedence**.
Policies are evaluated in ascending priority order, and the **first match
wins**.

### 4.2 Conditions and operators

Each policy has zero or more **conditions**, and **all of them must be true**
for the policy to match (logical AND). A policy with **no conditions is a
catch-all** that always matches.

A condition is `{ "field", "operator", "values" }`.

Resolvable **fields**:

| Field | Source |
|-------|--------|
| `mac`, `ip`, `hostname`, `device_type`, `posture` | endpoint attributes |
| `attr.<key>` | custom attribute (e.g. `attr.owner`) |

**Operators**:

| Operator | Meaning |
|----------|---------|
| `eq` | field equals one of `values` |
| `ne` | field does not equal any of `values` |
| `in` | field is one of `values` |
| `exists` | field is present |

> **Unknown operators fail closed** — a condition with an unrecognized operator
> never matches, so it can only make a policy more restrictive, never less.

### 4.3 Actions and segments

`action` is one of `allow` | `deny` | `quarantine`. `segment` names the network
segment the device is placed into when the policy matches (e.g. `corp`,
`iot`, `remediation`).

### 4.4 Create a policy

```bash
curl -sS -X POST "$BASE/api/v1/policies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "name": "compliant-laptops-allow",
    "description": "Authorize laptops reporting a compliant posture.",
    "priority": 10,
    "enabled": true,
    "action": "allow",
    "segment": "corp",
    "conditions": [
      { "field": "device_type", "operator": "eq", "values": ["laptop"] },
      { "field": "posture",     "operator": "eq", "values": ["compliant"] }
    ]
  }'
```

`name` and `action` are required; `description`, `priority`, `enabled`,
`conditions`, and `segment` are optional. List/get/update/delete mirror the
endpoint endpoints:

```bash
curl -sS "$BASE/api/v1/policies?limit=50&offset=0" -H "Authorization: Bearer $TOKEN"
curl -sS "$BASE/api/v1/policies/{id}"               -H "Authorization: Bearer $TOKEN"
curl -sS -X PUT "$BASE/api/v1/policies/{id}"  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{ "enabled": false }'
curl -sS -X DELETE "$BASE/api/v1/policies/{id}" -H "Authorization: Bearer $TOKEN"
```

### 4.5 Worked evaluation example

Suppose three policies exist:

| Priority | Name | Conditions | Action | Segment |
|---------:|------|------------|--------|---------|
| 10 | `compliant-laptops-allow` | `device_type eq laptop` AND `posture eq compliant` | `allow` | `corp` |
| 50 | `at-risk-quarantine` | `posture eq at_risk` | `quarantine` | `remediation` |
| 1000 | `default-quarantine` | *(none — catch-all)* | `quarantine` | `remediation` |

Now a device arrives: `device_type=laptop`, `posture=compliant`.

1. Priority 10 is checked first. Both conditions are true → **match**.
2. Evaluation stops (first match wins). Result: `action=allow`, `segment=corp`.

A second device arrives: `device_type=laptop`, `posture=at_risk`.

1. Priority 10: posture is not `compliant` → no match.
2. Priority 50: `posture eq at_risk` is true → **match**. Result:
   `action=quarantine`, `segment=remediation`.

A third device arrives with no attributes that match anything:

1. Priorities 10 and 50 do not match.
2. Priority 1000 has no conditions → catch-all **match** → `quarantine`.

Without the catch-all, the engine is **default-deny**: an unmatched device is
denied. The catch-all lets you choose to *quarantine* unknowns instead of
denying them. See [ADR 0003](../../adr/0003-default-deny-policy-engine.md) and
[architecture/business-logic.md](../architecture/business-logic.md).

---

## 5. How access evaluation works (enforcement points)

When a device joins the network, a network enforcement point (switch, wireless
controller, RADIUS bridge) calls `POST /api/v1/access/evaluate` to get a
verdict. **This call uses a static service token, not your JWT**, and is
normally made by infrastructure rather than a human:

```bash
SERVICE_TOKEN='<service-token>'
curl -sS -X POST "$BASE/api/v1/access/evaluate" \
  -H "Authorization: Bearer $SERVICE_TOKEN" -H 'Content-Type: application/json' \
  -d '{ "mac": "AA:BB:CC:11:22:33", "ip": "10.0.12.50", "posture": "compliant" }'
```

The response is an `AccessDecision` you can use to understand a verdict:

```json
{
  "endpoint": { "...": "..." },
  "status": "authorized",
  "action": "allow",
  "segment": "corp",
  "matched_policy_id": "<uuid>",
  "reason": "matched policy compliant-laptops-allow",
  "evaluated_at": "2026-06-27T12:00:00Z"
}
```

`matched_policy_id` and `reason` make every decision explainable — useful when
debugging why a device landed in a particular segment.

---

## 6. Read the audit log

Every state-changing action is recorded in an append-only audit log. All roles
(`admin`, `operator`, `viewer`) can read it:

```bash
curl -sS "$BASE/api/v1/audit?limit=50&offset=0" -H "Authorization: Bearer $TOKEN"
```

Each event carries a correlation id matching the originating request, so you can
trace an action back to its API call. See
[security/audit-logging.md](../security/audit-logging.md).

---

## 7. Pagination

List endpoints (`/endpoints`, `/policies`, `/audit`) accept:

| Param | Default | Max | Notes |
|-------|---------|-----|-------|
| `limit` | 50 | 200 | Number of items returned |
| `offset` | 0 | — | Must be `>= 0` |

```bash
# Second page of 100
curl -sS "$BASE/api/v1/endpoints?limit=100&offset=100" -H "Authorization: Bearer $TOKEN"
```

Out-of-range values (e.g. `limit` above 200 or a negative `offset`) are rejected
with a `422` validation error (see §8).

---

## 8. Interpreting errors (RFC 7807)

Every error is `application/problem+json` per **RFC 7807**, with a consistent
shape:

```json
{
  "type": "https://docs.nac/errors/validation",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "one or more fields are invalid",
  "instance": "<request-id>",
  "errors": { "mac": "is required", "priority": "must be >= 0" }
}
```

| Field | Meaning |
|-------|---------|
| `type` | Stable URI categorizing the error |
| `title` | Short, human-readable summary |
| `status` | HTTP status code |
| `detail` | Human-readable explanation of *this* occurrence |
| `instance` | The **request id** — quote this when reporting issues |
| `errors` | Present on `422`: a **field → message** map |

Common statuses:

| Status | Meaning | Typical cause |
|--------|---------|---------------|
| `401` | Unauthorized | Missing/expired/invalid token (login is generic) |
| `403` | Forbidden | Your role lacks the permission |
| `404` | Not Found | Unknown id |
| `409` | Conflict | Duplicate (e.g. a MAC that already exists) |
| `422` | Unprocessable Entity | Field validation failed — inspect `errors` |

Example: creating an endpoint without a MAC returns a `422` whose `errors` map
points at the offending field:

```bash
curl -sS -X POST "$BASE/api/v1/endpoints" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{}' | jq '{title, status, instance, errors}'
```

When you report a problem to an administrator, include the `instance` value — it
lets them find the exact log line and trace.

---

## 9. Reference

- The live API contract: `GET /openapi.yaml` (or
  [`api/openapi/openapi.yaml`](../../api/openapi/openapi.yaml)).
- [Administrator Guide](administrator.md) · [Developer Guide](developer.md)
- [Business Logic & Domain Model](../architecture/business-logic.md)
