# Deployment & Rollout

How to ship a new version of `nacd` to Kubernetes (namespace `nac`,
Deployment `nacd`), verify it, and roll back if needed. `nacd` is stateless;
PostgreSQL is the system of record, so a deploy never moves data — it only
swaps the running binary. Migrations run automatically on boot.

Related: [Release Process](../docs/operations/release-process.md),
[Database Migrations](database-migrations.md),
[Incident Response](../docs/operations/incident-response.md),
[Scalability](../docs/operations/scalability.md).

## When to use

- Promoting a new image to production.
- Applying configuration changes (`nacd-config` ConfigMap / `nacd-secrets`).
- Rolling back a bad release.

## Pre-deploy checks

```bash
# 1. Confirm current state is healthy before you change anything.
kubectl -n nac get deploy nacd
kubectl -n nac get pods -l app=nacd
kubectl -n nac rollout status deploy/nacd        # should already be complete
curl -s http://<host>/version                    # record the version you are replacing

# 2. Confirm the new image exists and is the one you intend to ship.
#    Image is distroless, non-root (uid 65532), readOnlyRootFilesystem.
docker image inspect <registry>/nacd:<new-tag> >/dev/null && echo OK

# 3. Review migrations bundled in the new image (run on boot).
#    Ensure they are BACKWARD-COMPATIBLE with the OLD code, because during a
#    rolling update both versions run simultaneously. See "Migration ordering".
ls internal/storage/postgres/migrations/ | tail
```

Take a database backup or confirm a recent one exists before any deploy that
carries a migration — see [Backup Strategy](../docs/operations/backup-strategy.md)
and the [Database Migrations](database-migrations.md) runbook.

## Build & push the image

```bash
make docker-build                                # builds distroless image
docker tag nacd:latest <registry>/nacd:<new-tag>
docker push <registry>/nacd:<new-tag>
```

## Migration ordering (read before every deploy)

Migrations run on **every boot** and are transactional and idempotent. The
rollout uses `maxUnavailable=0, maxSurge=1`, so a new pod (new code + its
migrations) starts while old pods still serve traffic. Therefore:

- **Backward-compatible migrations first.** A migration must not break the
  currently-running (old) code. Add columns/tables as nullable or with
  defaults; do not drop or rename columns the old code still reads.
- For a destructive change, split across releases: (1) add new schema +
  dual-write/read, ship and verify; (2) a later release removes the old schema
  once no running code depends on it.
- If you need migrations applied *before* any new pod serves traffic, run them
  as a one-shot job first (see [Database Migrations](database-migrations.md))
  and gate the rollout on its success.

## Deploy (rolling update)

Preferred: update the image tag via your manifests / GitOps and apply. Direct
imperative form:

```bash
kubectl -n nac set image deploy/nacd nacd=<registry>/nacd:<new-tag>
kubectl -n nac rollout status deploy/nacd --timeout=300s
```

To apply config/secret changes from the manifests:

```bash
kubectl -n nac apply -f deployments/kubernetes/config.yaml
# ConfigMap/Secret changes are NOT auto-reloaded; trigger a rolling restart:
kubectl -n nac rollout restart deploy/nacd
kubectl -n nac rollout status deploy/nacd
```

The rolling update brings up one surge pod at a time and never drops below the
desired count. The PodDisruptionBudget (`minAvailable=2`) protects against
voluntary disruptions during the rollout. Graceful shutdown drains in-flight
requests within `NAC_HTTP_SHUTDOWN_TIMEOUT` (20s) on SIGTERM.

## Canary / staged notes

There is no built-in traffic-splitting canary. To stage risk:

- Deploy to a staging namespace/cluster first and run the smoke checks below.
- Watch the first new pod become `Ready` and serve real traffic before the
  rollout proceeds to the rest (the `maxSurge=1` step already gives you this
  one-pod observation window — pause and inspect it before continuing).
- If using an HPA-driven fleet (min 3 / max 20), expect replica count to differ
  from 3; the rollout respects current scale.

## Post-deploy verification

```bash
# 1. Version is the new one.
curl -s http://<host>/version

# 2. Readiness (this pings the DB; 503 means DB dependency unhealthy).
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/readyz   # expect 200

# 3. Liveness (no dependency checks).
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/healthz  # expect 200

# 4. Smoke the evaluate path (static service-token bearer, NOT a JWT).
curl -s -X POST http://<host>/api/v1/access/evaluate \
  -H "Authorization: Bearer $NAC_SERVICE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"mac":"00:11:22:33:44:55"}' | jq '.action, .matched_policy_id, .reason'

# 5. Metrics: error rate should not climb after the swap.
curl -s http://<host>/metrics | grep -E 'nac_http_requests_total|nac_access_decisions_total'
```

Watch for new `nac_http_requests_total{status="5xx"}`, latency regressions in
`nac_http_request_duration_seconds`, and any startup config errors in logs
(structured JSON to stderr; every line carries `request_id`/`trace_id`):

```bash
kubectl -n nac logs deploy/nacd --tail=100
```

## Rollback

Fast path — revert to the previous ReplicaSet:

```bash
kubectl -n nac rollout undo deploy/nacd
kubectl -n nac rollout status deploy/nacd
curl -s http://<host>/version        # confirm the prior version is back
```

To roll back to a specific prior revision:

```bash
kubectl -n nac rollout history deploy/nacd
kubectl -n nac rollout undo deploy/nacd --to-revision=<N>
```

Code rollback is always safe. **Schema is not automatically reverted** — a
migration that already ran stays applied. This is why backward-compatible
migrations matter: the old code must still work against the new schema. If a
migration itself is the problem, see [Database Migrations](database-migrations.md).

## Escalation

- Rollout stuck / pods not becoming Ready after rollback: SEV2, page Operations.
- `/readyz` red fleet-wide or evaluate path erroring after deploy: SEV1, follow
  [Incident Response](../docs/operations/incident-response.md) and the
  [Service Degraded](service-degraded.md) runbook.
