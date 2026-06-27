# Dependency Outage

`nacd` has two backing dependencies: **PostgreSQL** (the system of record —
losing it is a correctness/availability event) and **Redis** (an *optional*
cache of the enabled-policy set — losing it degrades latency but **never**
correctness, because the cache fails open to the database). This runbook tells
the two apart and recovers each.

Related: [Disaster Recovery](../docs/operations/disaster-recovery.md),
[Backup Strategy](../docs/operations/backup-strategy.md),
[Scalability](../docs/operations/scalability.md),
[Service Degraded](service-degraded.md),
[Incident Response](../docs/operations/incident-response.md).

## Which dependency is it?

| Signal | PostgreSQL outage | Redis outage |
|--------|-------------------|--------------|
| `/healthz` (liveness) | 200 | 200 |
| `/readyz` (pings DB) | **503** | **200** |
| `nac_http_requests_total{status="5xx"}` | rises (evaluate/operator paths) | stays low |
| `nac_http_request_duration_seconds` | high or timing out | rises (cache misses → DB) |
| Logs | DB dial/connection/timeout errors | Redis dial errors, then "falling back to DB" |
| Correctness | reads/writes fail | unchanged (fail-open) |

```bash
curl -s -o /dev/null -w 'healthz=%{http_code} ' http://<host>/healthz
curl -s -o /dev/null -w 'readyz=%{http_code}\n'  http://<host>/readyz
kubectl -n nac logs deploy/nacd --tail=120 | grep -i -E 'postgres|redis|dial|fallback|connection'
curl -s http://<host>/metrics | grep -E 'nac_http_requests_total|nac_http_request_duration_seconds'
```

Rule of thumb: **`/readyz` 503 ⇒ PostgreSQL**. `/readyz` 200 with latency up and
error rate flat ⇒ **Redis**.

---

## PostgreSQL outage

**Severity:** typically SEV1 — `nacd` cannot read policies/endpoints or write
audit records; `/readyz` goes red and pods are pulled from rotation fleet-wide.

### Symptoms

- `/readyz` 503 across replicas; `/healthz` still 200.
- Evaluate and operator API requests return 5xx; logs show DB connection errors
  with `request_id`/`trace_id`.

### Diagnosis

```bash
# Confirm the DB itself, independent of nacd:
psql "$NAC_DB_DSN" -c 'SELECT 1;'                 # hangs/errors => DB unreachable
psql "$NAC_DB_DSN" -c 'SELECT now();'             # liveness + clock

# Distinguish total outage vs saturation/connection exhaustion:
psql "$NAC_DB_DSN" -c \
  "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"
```

Check whether it is: the database down, a network partition, credential failure
(see [Secret Rotation](secret-rotation.md)), or pgx pool exhaustion under load.

### Remediation

1. **If the primary is down and a replica exists — fail over.** Promote the
   replica per your managed-Postgres / HA tooling, then point `nacd` at it:
   ```bash
   # Update NAC_DB_DSN in the secrets manager to the new primary endpoint,
   # apply, and restart so pods reconnect:
   kubectl -n nac apply -f deployments/kubernetes/config.yaml
   kubectl -n nac rollout restart deploy/nacd
   ```
2. **If there is no healthy replica — restore from backup.** Follow
   [Disaster Recovery](../docs/operations/disaster-recovery.md) and
   [Backup Strategy](../docs/operations/backup-strategy.md) to restore to the
   latest recovery point, then update `NAC_DB_DSN` to the restored instance.
3. **If it is connection exhaustion, not an outage**, the DB is up but the pool
   is starved — reduce load (throttle abusive callers), and avoid blindly
   scaling `nacd` replicas higher (more pods = more connections). See
   [Scalability](../docs/operations/scalability.md).

`nacd` is stateless: once the DB is reachable again, pods recover with no data
action on the app side. No restart is strictly required for transient blips —
the pool reconnects and `/readyz` flips back to 200 — but a `rollout restart`
forces fresh connections if pods are stuck.

### Verification

```bash
psql "$NAC_DB_DSN" -c 'SELECT 1;'                                   # OK
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/readyz       # 200
kubectl -n nac get pods -l app=nacd                                 # all Ready
curl -s http://<host>/metrics | grep 'nac_http_requests_total' | grep 'status="5xx"'  # flat
```

---

## Redis outage

**Severity:** typically SEV2 — degraded, not broken. Redis only caches the
enabled-policy set (key `nac:policies:enabled`). On a cache miss or Redis error,
`nacd` **fails open** and reads policies from PostgreSQL. Decisions stay correct.

### Symptoms

- `/readyz` 200, `/healthz` 200 (readiness does not depend on Redis).
- Latency rises (every evaluate now reads policies from the DB); 5xx and
  decision error rate stay low.
- Logs show Redis dial errors followed by fallback-to-DB messages.

### Diagnosis

```bash
redis-cli -u "$NAC_REDIS_ADDR" PING            # PONG if up; error/timeout if down
kubectl -n nac logs deploy/nacd --tail=120 | grep -i -E 'redis|cache|fallback'
curl -s http://<host>/metrics | grep nac_http_request_duration_seconds   # elevated, esp. evaluate
```

### Confirm there is NO correctness impact

This is the key reassurance: losing Redis cannot change a decision. Verify the
same input still resolves correctly straight from the DB:

```bash
curl -s -X POST http://<host>/api/v1/access/evaluate \
  -H "Authorization: Bearer $NAC_SERVICE_TOKEN" \
  -H 'Content-Type: application/json' -d '{"mac":"00:11:22:33:44:55"}' \
  | jq '{action, matched_policy_id, reason}'
```

The `matched_policy_id` and `reason` should match the policy you expect.
Cross-check against the [Access-Decision Incident](access-incident.md) runbook
only if a decision actually looks wrong (that would be a policy-config issue,
not a Redis one).

### Remediation

1. Restart / restore Redis per its own operations (managed service failover or
   pod restart). The cache **warms on demand** — `nacd` repopulates
   `nac:policies:enabled` on the next read; no manual warm step is required.
2. If Redis will be down a while, you may leave it: the platform runs correctly
   on the DB alone, only slower. Consider temporarily scaling `nacd` and/or
   watching DB load while the cache is cold.
3. No `nacd` restart is needed — it reconnects to Redis automatically once it is
   back and resumes caching.

### Verification

```bash
redis-cli -u "$NAC_REDIS_ADDR" PING                 # PONG
redis-cli -u "$NAC_REDIS_ADDR" EXISTS nac:policies:enabled   # 1 after first read repopulates
curl -s http://<host>/metrics | grep nac_http_request_duration_seconds   # latency back to baseline
```

## Escalation

- PostgreSQL outage / restore in progress: **SEV1**, engage IC per
  [Incident Response](../docs/operations/incident-response.md).
- Redis outage with healthy DB fallback: **SEV2**; escalate only if DB load
  becomes the next bottleneck.
