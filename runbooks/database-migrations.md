# Database Migrations

How `nacd` manages PostgreSQL schema, how to apply migrations, verify them,
and recover from a failed one. Migrations are **embedded** in the binary,
**transactional** (each wrapped in its own transaction), and **idempotent**.

Related: [Database Schema](../docs/database/schema.md),
[Backup Strategy](../docs/operations/backup-strategy.md),
[Disaster Recovery](../docs/operations/disaster-recovery.md),
[Deployment & Rollout](deployment.md).

## How migrations work

- On **every boot**, `nacd` applies all pending migrations before serving
  traffic, then continues to start.
- Files live in `internal/storage/postgres/migrations/` as paired
  `NNNN_*.up.sql` and `NNNN_*.down.sql`. The `.down.sql` files exist for
  **manual** rollback; they are not run automatically.
- A ledger table `schema_migrations(version, applied_at)` records which
  versions have been applied. Already-applied versions are skipped.
- Each migration runs in its own transaction: if migration N fails, N is rolled
  back atomically and boot aborts. Migrations 1..N-1 remain applied.

## Applying migrations

### Option A — on boot (default)

Any new pod applies pending migrations automatically at startup. A normal
[deploy](deployment.md) therefore carries its migrations with it.

### Option B — explicit one-shot (apply then exit)

Use when you want migrations applied **before** rolling new code, or to apply
without starting a server:

```bash
# As a binary invocation (applies all pending migrations and exits):
nacd -migrate

# Via the Makefile (same effect, for local/CI use):
make migrate
```

Run this as a one-shot Kubernetes Job using the **new** image against the same
`NAC_DB_DSN`, and gate the rollout on its success:

```bash
kubectl -n nac run nacd-migrate --rm -i --restart=Never \
  --image=<registry>/nacd:<new-tag> \
  --env="NAC_DB_DSN=$NAC_DB_DSN" \
  --command -- /nacd -migrate
```

## Backup before migrating

Always have a fresh backup or restore point before applying a migration,
especially a destructive one. See [Backup Strategy](../docs/operations/backup-strategy.md).

```bash
# Logical dump (adapt to your managed-Postgres tooling / WAL snapshot policy).
pg_dump "$NAC_DB_DSN" -Fc -f nac-pre-migrate-$(date +%Y%m%dT%H%M%S).dump
```

## Verification

```bash
# Which versions are applied, most recent last.
psql "$NAC_DB_DSN" -c \
  "SELECT version, applied_at FROM schema_migrations ORDER BY version;"

# Confirm the highest applied version matches the latest file shipped.
ls internal/storage/postgres/migrations/*.up.sql | sort | tail -1

# Service confirms DB schema is usable by passing readiness (pings DB).
curl -s -o /dev/null -w '%{http_code}\n' http://<host>/readyz   # expect 200
```

## Handling a failed migration

**Symptom:** new pods crashloop at startup; logs (structured JSON to stderr)
show a migration error before the server binds. `/readyz` is unavailable on the
new pods. With `maxUnavailable=0`, old pods keep serving, so traffic usually
survives — do not panic-delete the old ReplicaSet.

```bash
kubectl -n nac get pods -l app=nacd
kubectl -n nac logs <crashlooping-pod> | tail -50    # find the failing version + SQL error
```

**Why it is recoverable:** the failed migration ran in its own transaction and
was rolled back atomically, so the database is **not** left half-migrated. The
ledger does not record the failed version.

### Preferred remediation: fix forward

1. Halt the rollout so it stops launching doomed pods:
   ```bash
   kubectl -n nac rollout pause deploy/nacd
   ```
2. Correct the migration SQL (or add a new, corrected migration with the next
   version number). Migrations are idempotent, so re-running is safe.
3. Build and ship the corrected image, then resume:
   ```bash
   kubectl -n nac set image deploy/nacd nacd=<registry>/nacd:<fixed-tag>
   kubectl -n nac rollout resume deploy/nacd
   kubectl -n nac rollout status deploy/nacd
   ```
4. Verify via `schema_migrations` and `/readyz` (above).

### Manual rollback via `.down.sql` (use with caution)

Only when a migration applied successfully but you must revert the schema
(e.g. it is incompatible with the running code and you cannot fix forward).
Down migrations can be **destructive** (dropping a column loses its data).

```bash
# 1. Take a backup first (see above).
# 2. Apply the matching down file manually, inside a transaction.
psql "$NAC_DB_DSN" -1 -f internal/storage/postgres/migrations/NNNN_<name>.down.sql

# 3. Remove the version from the ledger so boot does not assume it is applied.
psql "$NAC_DB_DSN" -c "DELETE FROM schema_migrations WHERE version = NNNN;"
```

After a manual down, you **must** also roll back the code to a version that does
not expect the reverted schema, or boot will re-apply the up migration. Treat
manual rollback as a coordinated change, not a quick fix.

## Locking & long-running migrations

- A migration that takes a heavy lock (e.g. `ALTER TABLE` rewrites, adding a
  non-`NOT NULL`-without-default column, certain index builds) can block reads
  or writes and stall pod startup behind a lock wait.
- Prefer non-blocking patterns: add columns nullable / with defaults, build
  indexes `CONCURRENTLY` (note: cannot run inside a transaction — keep such
  steps as a separate, carefully reviewed migration or out-of-band operation),
  and backfill in batches.
- Watch for blocking during a migration:
  ```bash
  psql "$NAC_DB_DSN" -c \
    "SELECT pid, state, wait_event_type, query
       FROM pg_stat_activity WHERE state <> 'idle';"
  psql "$NAC_DB_DSN" -c "SELECT * FROM pg_locks WHERE NOT granted;"
  ```
- For very large tables, prefer Option B (one-shot Job) so the long migration
  runs once, observably, rather than racing across multiple booting pods.

## Escalation

- Migration failing in production and fix-forward is not quick: SEV2, page
  Operations; old pods still serving buys time.
- Schema corruption or data loss suspected: SEV1, follow
  [Disaster Recovery](../docs/operations/disaster-recovery.md) and
  [Incident Response](../docs/operations/incident-response.md).
