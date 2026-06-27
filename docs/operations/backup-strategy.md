# Backup Strategy

This document defines what must be backed up to recover the NAC platform, how
those backups are taken, and the recovery objectives they support. The guiding
principle is that **PostgreSQL is the only stateful component that requires
backup**. Everything else in the system is either disposable (Redis) or sourced
from elsewhere (secrets), and `nacd` itself is a stateless binary that is
redeployed from an image rather than restored.

## What must be backed up — and what must not

| Component | Backup required? | Rationale |
|-----------|------------------|-----------|
| **PostgreSQL** | **Yes — this is the entire recovery scope** | System of record: operators, endpoints, policies, audit events, migration bookkeeping |
| **Redis** | **No — disposable** | Only an optional cache of the enabled-policy set; rebuilds from the database and fails open if absent |
| **Secrets** (DB DSN, `NAC_SERVICE_TOKEN`, JWT keys) | Not via DB backup | Live in a secrets manager; backed up and rotated there, not in the database backup |
| **`nacd` binary / image** | No | Rebuilt from source via CI; see [release-process.md](release-process.md) |
| **Kubernetes manifests / config** | Via source control | `deployments/kubernetes`, ConfigMap, Secret template live in the repository |

This scoping is deliberate and important: a complete, restorable PostgreSQL
backup plus access to the secrets manager and the container image is sufficient
to reconstitute the entire platform.

### Redis is disposable — back up nothing

Redis caches only the enabled-policy set under the key `nac:policies:enabled`
with a TTL of `NAC_REDIS_TTL` (default `30s`). It holds no authoritative data.
On a cold start, or after a total Redis loss, the platform **fails open to the
database**: evaluations execute the enabled-policy query directly (backed by the
partial index `policies_enabled_priority_idx`) and the cache repopulates
naturally on subsequent requests. There is therefore **nothing in Redis to back
up**, and no Redis restore step in any recovery procedure.

## PostgreSQL backup methods

Two complementary mechanisms cover the spectrum from logical portability to
fine-grained point-in-time recovery.

### 1. Logical backups with `pg_dump`

Periodic logical dumps provide a portable, version-independent snapshot of the
database that is convenient for migration, for spinning up staging copies, and
as a coarse recovery floor.

```bash
# Full logical dump in the custom (compressed, restorable) format
pg_dump --format=custom --no-owner --no-privileges \
        --file=nac-$(date -u +%Y%m%dT%H%M%SZ).dump \
        "$NAC_DB_DSN"
```

Logical dumps are simple and robust but recover only to the moment the dump was
taken. They are not sufficient on their own for a tight recovery-point
objective.

### 2. Continuous WAL archiving and point-in-time recovery (PITR)

For a tight RPO, run continuous **write-ahead log (WAL) archiving** alongside
periodic base backups. This enables **point-in-time recovery**: restore the most
recent base backup, then replay archived WAL forward to any chosen moment (for
example, to just before an erroneous mutation).

The mechanism is:

1. Take periodic base backups of the data directory.
2. Continuously archive completed WAL segments to durable, off-host storage.
3. To recover, restore a base backup and replay WAL up to the desired recovery
   target.

PITR is the primary recovery mechanism for production because it minimises data
loss and allows recovery to a precise point rather than only to the last full
dump.

## Audit data: retention, partitioning, and archival

The `audit_events` table is **append-only and grows unbounded** — one coarse row
per access decision and one per administrative mutation. It is both the most
compliance-sensitive data and the fastest-growing, so it needs a deliberate
lifecycle distinct from the rest of the schema.

- **Partition by time.** Range-partition `audit_events` by event timestamp (for
  example, monthly). This makes both backup and retention enforcement
  operations on whole partitions rather than on a single ever-growing table.
- **Retain online for the compliance window.** Keep recent partitions in the
  live database for the period that operational and compliance queries require.
- **Archive aged partitions.** Before pruning, export aged partitions to durable
  long-term storage (for example, compressed object storage) so the audit record
  is preserved beyond the online retention window.
- **Prune by dropping/detaching partitions.** Once archived and past retention,
  remove old partitions with a partition `DROP`/`DETACH` rather than a bulk
  `DELETE`, which avoids bloat and long-running vacuums.

Because there are **no hard foreign keys** in the schema, audit rows survive the
deletion of the operators or endpoints they reference, so archival and pruning of
audit partitions never break referential integrity elsewhere.

## Recovery objectives (RPO / RTO)

These targets describe the intended recovery posture; tune them to your
environment and validate them with the testing cadence below.

| Scenario | RPO (max data loss) | RTO (max downtime) | Mechanism |
|----------|---------------------|--------------------|-----------|
| Logical-restore fallback | Up to last `pg_dump` interval | Hours | `pg_restore` from latest dump |
| PITR (production target) | Minutes (last archived WAL) | Tens of minutes | Base backup + WAL replay |
| Redis loss | 0 (no durable data) | None (fails open, warms automatically) | No restore required |
| Secrets loss | 0 (held in secrets manager) | Minutes (re-inject) | Re-fetch from secrets manager |

The combination of continuous WAL archiving (small RPO) and a stateless,
quickly-redeployable application tier (small RTO) is what keeps both objectives
low.

## Restore verification cadence

A backup that has never been restored is not a backup. Verify restores on a
fixed cadence:

- **Routinely (e.g. monthly):** restore the latest backup into an isolated
  environment, run the migrations to confirm schema consistency
  (`nacd -migrate` is idempotent), and bring up `nacd` against the restored
  database. Confirm `/readyz` reports the database reachable.
- **Validate PITR specifically:** at least quarterly, perform a base-backup +
  WAL-replay restore to a chosen point in time and confirm the recovered data
  matches expectations.
- **Spot-check audit archives:** confirm that archived audit partitions can be
  read back and that recent and historical events are both retrievable.
- **Record results.** Capture the achieved RPO/RTO from each drill and compare
  against the targets above; treat regressions as defects.

The full restore-and-failover walkthrough lives in
[disaster-recovery.md](disaster-recovery.md), which references the mechanisms in
this document.

## Operational notes

- **Migrations are embedded and idempotent.** A restored database is brought to
  the current schema by running `nacd -migrate`, which applies only pending UP
  migrations, each in its own transaction with a bookkeeping insert. There is no
  separate migration artifact to back up beyond what ships in the image.
- **Secrets are never in the dump.** Backups contain application data only.
  Restoring a database does not restore credentials; those are re-injected from
  the secrets manager (see [secrets-management.md](secrets-management.md)).
- **Test against a representative data volume.** Restore timing for the audit
  table dominates at scale, which is another reason to partition and archive it.

## Related documents

- [disaster-recovery.md](disaster-recovery.md) — DR scenarios and restore
  procedures that use these backups.
- [scalability.md](scalability.md) — audit table growth and partitioning.
- [secrets-management.md](secrets-management.md) — where secrets live.
- [../database/schema.md](../database/schema.md) — table definitions.
