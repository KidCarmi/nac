# Service Degraded / Down

`nacd` is unhealthy: elevated 5xx, rising latency, pods crashlooping, or
readiness failing. This runbook triages the cause and restores service.
`nacd` is stateless — most recovery is "find the bad input (config/dependency/
release) and remove it," not data repair.

Related: [Observability](../docs/operations/observability.md),
[Metrics](../docs/operations/metrics.md), [Logging](../docs/operations/logging.md),
[Dependency Outage](dependency-outage.md), [Deployment & Rollout](deployment.md),
[Incident Response](../docs/operations/incident-response.md).

## Symptoms

- `nac_http_requests_total{status="5xx"}` rising.
- `nac_http_request_duration_seconds` p95/p99 climbing on the evaluate route.
- Pods `CrashLoopBackOff`, `Error`, or never reaching `Ready`.
- `/readyz` returning 503; load balancer pulling pods out of rotation.
- Bursts of HTTP 429 (rate limiting).

## First checks (always)

```bash
kubectl -n nac get pods -l app=nacd -o wide
kubectl -n nac get deploy nacd
kubectl -n nac logs deploy/nacd --tail=150               # JSON; grab request_id/trace_id
curl -s -o /dev/null -w 'healthz=%{http_code}\n' http://<host>/healthz   # liveness, no deps
curl -s -o /dev/null -w 'readyz=%{http_code}\n'  http://<host>/readyz    # readiness, pings DB
curl -s http://<host>/metrics | grep -E 'nac_http_requests_total|nac_http_request_duration_seconds|nac_auth_failures_total'
```

Note the split: `/healthz` is liveness with **no** dependency checks; `/readyz`
pings the database. Comparing them localizes the fault fast (see tree below).

## Diagnosis tree

### 1. Pods crashloop immediately at startup → config error

New/all pods never reach `Ready`; logs show an aggregated configuration error
before the server binds (e.g. missing `NAC_DB_DSN`, `NAC_JWT_SIGNING_KEY` shorter
than 32 bytes, `NAC_SERVICE_TOKEN` shorter than 24 chars in a non-dev build).

```bash
kubectl -n nac logs <pod> | head -40        # look for the aggregated startup config error
kubectl -n nac describe pod <pod>           # env wiring, mounted Secret/ConfigMap
```

Cause is almost always a bad `nacd-config` ConfigMap or `nacd-secrets` Secret
(or a secrets-manager sync failure). Fix the config/secret and restart. See
[Configuration](../docs/operations/configuration.md) and
[Secret Rotation](secret-rotation.md).

### 2. `/healthz` 200 but `/readyz` 503 → database unreachable

Liveness passes, readiness fails because the DB ping fails. This is a dependency
outage, not a code fault. Logs show DB connection errors with `request_id`.

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/readyz   # 503
kubectl -n nac logs deploy/nacd --tail=80 | grep -i -E 'db|postgres|dial|connection'
```

Go to [Dependency Outage](dependency-outage.md) (PostgreSQL section). Redis
loss does **not** cause this — Redis fails open to the DB.

### 3. Pods come up, then crash under traffic → panic loop

Logs show the recoverer catching panics (stack trace lines tied to a
`request_id`/`trace_id`), often correlated with a specific route or payload.

```bash
kubectl -n nac logs deploy/nacd --tail=200 | grep -i -E 'panic|recovered'
```

If it started right after a release, **roll back** (see below). Capture the
`request_id`/`trace_id` and the triggering request for the bug report.

### 4. Spike of HTTP 429 → rate limiting

```bash
curl -s http://<host>/metrics | grep 'nac_http_requests_total' | grep 'status="4xx"'
kubectl -n nac logs deploy/nacd --tail=200 | grep -i 'rate'
```

Often a client misconfiguration or retry storm hammering the evaluate path.
Identify the source (request_id/trace_id, client IP at the edge), throttle or
fix the caller. If load is legitimate, scale (below).

### 5. High latency, low error rate → degraded dependency

Latency up but 5xx flat and decision error rate low usually means Redis is down
and every read falls through to PostgreSQL (correct, but slower). Confirm via
[Dependency Outage](dependency-outage.md) (Redis section).

## Remediation

```bash
# Restart pods to clear a transient bad state or pick up new config:
kubectl -n nac rollout restart deploy/nacd
kubectl -n nac rollout status deploy/nacd

# Roll back a bad release (panic loop / regression introduced by the last deploy):
kubectl -n nac rollout undo deploy/nacd
kubectl -n nac rollout status deploy/nacd
```

For a config/secret fix: update via the secrets manager / `config.yaml`, apply,
then `rollout restart` (ConfigMap/Secret changes are not hot-reloaded).

## When to scale

The HPA targets 70% CPU, min 3 / max 20 replicas. If CPU-bound under genuine
load it should scale on its own. Check and, if needed, intervene:

```bash
kubectl -n nac get hpa nacd
kubectl -n nac top pods -l app=nacd

# Temporary manual floor while investigating (HPA will reconcile within bounds):
kubectl -n nac scale deploy/nacd --replicas=8
```

PDB `minAvailable=2` keeps capacity during voluntary disruptions. If you are at
`max=20` and still saturated, the bottleneck is likely PostgreSQL — see
[Scalability](../docs/operations/scalability.md) and
[Dependency Outage](dependency-outage.md).

## Verification

```bash
kubectl -n nac get pods -l app=nacd          # all Ready
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/readyz   # 200
curl -s http://<host>/metrics | grep 'nac_http_requests_total' | grep 'status="5xx"'  # flat
# Smoke the evaluate path (service-token bearer, not JWT):
curl -s -X POST http://<host>/api/v1/access/evaluate \
  -H "Authorization: Bearer $NAC_SERVICE_TOKEN" \
  -H 'Content-Type: application/json' -d '{"mac":"00:11:22:33:44:55"}' | jq .action
```

## Escalation

- `/readyz` red fleet-wide or evaluate path erroring at scale: **SEV1**.
- Elevated 5xx/latency with limited blast radius, or running on degraded
  redundancy: **SEV2**.
- Single transient spike that self-recovered: **SEV3**.

Follow [Incident Response](../docs/operations/incident-response.md). Always lead
your report with the `request_id`/`trace_id` from the failing requests.
