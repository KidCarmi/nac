# Runbooks

Operational procedures for running `nacd` in production. Each runbook is
self-contained and assumes access to logs, metrics, and the deployment platform
(Kubernetes by default).

| Runbook | When to use |
|---------|-------------|
| [Deployment & Rollout](deployment.md) | Deploying a new version, rolling back |
| [Database Migrations](database-migrations.md) | Applying/rolling back schema changes |
| [Service Degraded / Down](service-degraded.md) | `nacd` unhealthy, high error rate, or unreachable |
| [Dependency Outage](dependency-outage.md) | PostgreSQL or Redis unavailable |
| [Secret Rotation](secret-rotation.md) | Rotating JWT key, service token, DB/Redis creds |
| [Access-Decision Incident](access-incident.md) | Devices wrongly allowed/denied/quarantined |

## Conventions

- **Severity:** SEV1 (platform down / mass mis-authorization), SEV2 (degraded),
  SEV3 (minor). See [Incident Response](../docs/operations/incident-response.md).
- **Correlation:** every log line and error carries `request_id`/`trace_id`.
  Start every investigation from these.
- **First checks (always):**
  ```bash
  kubectl -n nac get pods
  kubectl -n nac logs deploy/nacd --tail=100
  curl -s http://<host>/readyz        # readiness (checks DB)
  curl -s http://<host>/metrics | grep nac_   # key metrics
  ```

## Key metrics to watch

| Metric | Meaning |
|--------|---------|
| `nac_http_requests_total{status="5xx"}` | Server-side errors |
| `nac_http_request_duration_seconds` | Latency distribution per route |
| `nac_access_decisions_total{action}` | Allow/deny/quarantine rates |
| `nac_auth_failures_total{reason}` | Auth failures by cause |
