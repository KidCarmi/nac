# Secret Rotation

How to rotate `nacd`'s sensitive configuration: the JWT signing key, the static
service token, and database/Redis credentials. Secrets are delivered from a
secrets manager (External Secrets / Vault / sealed-secrets) into the
`nacd-secrets` Secret and consumed as environment variables. Secrets are
**redacted in logs**, so rotation never leaks the old or new value.

General mechanism: update the value in the **secrets manager** (not by editing
the live Secret by hand), let it sync, then trigger a rolling restart so pods
pick it up — ConfigMap/Secret changes are **not** hot-reloaded.

```bash
kubectl -n nac rollout restart deploy/nacd
kubectl -n nac rollout status deploy/nacd
```

Related: [Secrets Management](../docs/operations/secrets-management.md),
[Configuration](../docs/operations/configuration.md),
[Trust Boundaries](../docs/security/trust-boundaries.md),
[Deployment & Rollout](deployment.md).

## Rotate `NAC_JWT_SIGNING_KEY`

Signs operator JWTs for the `/api/v1` operator API. Must be **>= 32 bytes**.

**Impact:** changing the key invalidates **all existing operator JWTs** — every
operator must re-login to get a token signed with the new key. The `/api/v1/access/evaluate`
path is unaffected (it uses the service token, not a JWT). Operator JWT TTL is
short (1h), so even without a forced re-login the blast window self-heals within
an hour, but rotation invalidates them immediately.

```bash
# 1. Generate a new key (>=32 bytes; 48 bytes base64 is comfortable).
openssl rand -base64 48

# 2. Store it in the secrets manager as NAC_JWT_SIGNING_KEY; let it sync.
# 3. Roll the deployment.
kubectl -n nac rollout restart deploy/nacd
kubectl -n nac rollout status deploy/nacd
```

**Coordinate:** announce to operators that they will be logged out. After the
roll, expect a brief rise in `nac_auth_failures_total{reason="invalid_token"}`
as old tokens are rejected; it should subside as operators re-login.

**Verify:**

```bash
# Old tokens now rejected (401):
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/api/v1/audit \
  -H "Authorization: Bearer <OLD_OPERATOR_JWT>"        # expect 401

# Fresh login + call succeeds (login flow per administrator guide):
# ... obtain NEW_JWT via the operator login endpoint ...
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/api/v1/audit \
  -H "Authorization: Bearer <NEW_JWT>"                 # expect 200
```

## Rotate `NAC_SERVICE_TOKEN`

Static bearer token for `POST /api/v1/access/evaluate`, used by enforcement
points (the devices/switches/agents calling the evaluate path). Must be
**>= 24 characters**.

**Impact:** there is a single active token. A naive swap rejects every
enforcement point that still holds the old token until they update — a
fleet-wide auth failure on the evaluate path. **Coordinate** the rollout with
the enforcement-point owners.

**Dual-accept window (recommended):** if your edge / API gateway can accept two
bearer values during a transition, configure it to accept **old OR new** at the
edge, roll enforcement points to the new token, then drop the old. `nacd` itself
holds one `NAC_SERVICE_TOKEN`; the dual-accept lives at the edge to bridge the
cutover without a flag day.

```bash
# 1. Generate a new token (>=24 chars).
openssl rand -hex 24

# 2. (If using dual-accept) configure the edge/gateway to accept old + new.
# 3. Update NAC_SERVICE_TOKEN in the secrets manager; let it sync.
# 4. Roll the deployment.
kubectl -n nac rollout restart deploy/nacd
kubectl -n nac rollout status deploy/nacd
# 5. Roll enforcement points to the new token. Then remove old from the edge.
```

**Verify:**

```bash
# New token works:
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://<host>/api/v1/access/evaluate \
  -H "Authorization: Bearer <NEW_SERVICE_TOKEN>" \
  -H 'Content-Type: application/json' -d '{"mac":"00:11:22:33:44:55"}'   # expect 200

# Old token rejected after cutover:
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://<host>/api/v1/access/evaluate \
  -H "Authorization: Bearer <OLD_SERVICE_TOKEN>" \
  -H 'Content-Type: application/json' -d '{"mac":"00:11:22:33:44:55"}'   # expect 401

# Watch for stragglers still on the old token:
curl -s http://<host>/metrics | grep 'nac_auth_failures_total' | grep 'invalid_service_token'
```

## Rotate database / Redis credentials

`NAC_DB_DSN` (PostgreSQL, **required**) and `NAC_REDIS_ADDR` (Redis, optional)
carry their credentials inline in the connection string.

**Impact:** DB credential rotation is the sensitive one — a wrong/stale DSN
makes `/readyz` go 503 (see [Dependency Outage](dependency-outage.md)). Redis
credential rotation only affects the cache, which fails open, so the blast
radius is latency, not correctness.

**Procedure (zero-downtime DB credential rotation):**

```bash
# 1. In PostgreSQL, create/grant the NEW credential alongside the old one
#    (both valid simultaneously) so there is no gap.
psql "$NAC_DB_DSN" -c "ALTER ROLE nacd WITH PASSWORD '<new-password>';"
#    (or create a new role and grant equivalent privileges)

# 2. Update NAC_DB_DSN (and/or NAC_REDIS_ADDR) in the secrets manager; sync.
# 3. Roll the deployment so pods reconnect with the new DSN.
kubectl -n nac rollout restart deploy/nacd
kubectl -n nac rollout status deploy/nacd

# 4. Once all pods are Ready on the new credential, revoke the old one.
```

**Verify:**

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/readyz   # 200 (DB reachable with new creds)
kubectl -n nac get pods -l app=nacd                             # all Ready
kubectl -n nac logs deploy/nacd --tail=80 | grep -i -E 'postgres|redis|auth'  # no auth errors
```

For Redis, confirm the cache repopulates (`redis-cli -u "$NAC_REDIS_ADDR" PING`
returns `PONG`); if it stays down, the platform still serves correctly from the
DB.

## General notes & escalation

- Never commit secrets to git or paste them into logs/tickets; rely on the
  secrets manager. `nacd` redacts secrets in its own logs.
- Rotate one secret at a time so you can attribute any post-rotation failure.
- Keep the old value available until the new one is confirmed working, then
  revoke — except `NAC_JWT_SIGNING_KEY`, where immediate invalidation is the
  intended effect.
- A botched DB-credential rotation that takes `/readyz` red is a **SEV1/SEV2**
  dependency event — follow [Dependency Outage](dependency-outage.md) and
  [Incident Response](../docs/operations/incident-response.md). The fastest
  recovery is restoring the previous known-good DSN in the secrets manager and
  rolling again.
- If a secret is believed **compromised**, treat rotation as urgent and skip the
  graceful dual-accept window — invalidate immediately and accept the brief
  disruption.
