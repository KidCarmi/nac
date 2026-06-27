# Disaster Recovery

This document describes how to recover the NAC platform from major failures. It
builds on [backup-strategy.md](backup-strategy.md), which defines what is backed
up and how, and assumes the recovery objectives stated there. The recurring
theme is that recovery is straightforward because the platform separates cleanly
into a **stateless application tier** (rebuilt from an image), a **single
stateful system of record** (PostgreSQL, restored from backup), a **disposable
cache** (Redis, never restored), and **secrets** (re-injected from a secrets
manager).

## Recovery objectives

The disaster-recovery targets mirror the backup strategy:

| Scenario | RPO | RTO | Primary mechanism |
|----------|-----|-----|-------------------|
| Database loss / corruption | Minutes (last archived WAL) | Tens of minutes | PITR restore (base backup + WAL replay) |
| Region loss | Minutes | Tens of minutes to ~1 hour | Promote standby / restore in surviving region |
| Total cluster loss | Minutes (DB), 0 (app) | ~1 hour | Rebuild from scratch + DB restore |
| Redis loss | 0 | None | Fails open; no restore |
| Secrets loss | 0 | Minutes | Re-inject from secrets manager |

## Scenario 1: Database loss or corruption

The database is the only component whose loss causes true data loss, so this is
the central DR scenario.

1. **Stop writes** by scaling `nacd` down or putting it into a maintenance state
   so no further mutations or audit rows are written to a damaged database.
2. **Restore PostgreSQL** using the mechanism appropriate to the incident:
   - For corruption or an erroneous mutation, use **PITR**: restore the latest
     base backup and replay archived WAL to a recovery target just before the
     bad change.
   - As a fallback, restore the most recent `pg_dump` with `pg_restore`.
3. **Apply migrations** against the restored database with `nacd -migrate`. The
   runner is idempotent and applies only pending UP migrations, so this is safe
   to run unconditionally and brings the schema to the version the current image
   expects.
4. **Restart `nacd`** and confirm `/readyz` reports the database reachable (it
   performs a DB ping). `/healthz` reports liveness only and has no dependencies.
5. **Let Redis warm naturally.** No action is required; the cache repopulates on
   the first evaluations (see the Redis cold-start note below).

## Scenario 2: Region loss

If the region hosting the primary is lost:

1. **Recover the database in a surviving region** by promoting a standby/replica
   if one is maintained there, or by restoring from off-region backups and WAL
   archive.
2. **Deploy `nacd`** from the container image into the surviving region's
   cluster using the manifests in `deployments/kubernetes`.
3. **Re-inject secrets** from the secrets manager into the new region's Secret
   objects (DB DSN pointing at the recovered database, `NAC_SERVICE_TOKEN`, JWT
   keys).
4. **Cut traffic over** to the surviving region (DNS / load balancer / global
   ingress) once `/readyz` is green on the new replicas.

Because `nacd` is stateless, the application side of a regional failover is just
a redeploy; the time-consuming step is database recovery, which is why
maintaining an off-region standby or recent WAL archive directly improves RTO.

## Scenario 3: Total cluster loss (rebuild from scratch)

When there is no surviving environment, rebuild the whole platform:

1. **Provision infrastructure** — a Kubernetes cluster (or equivalent),
   PostgreSQL, and Redis.
2. **Restore PostgreSQL** from backup (PITR preferred; logical dump as
   fallback) per [backup-strategy.md](backup-strategy.md). If rebuilding with no
   usable backup at all, an empty database is acceptable — migrations will
   create the schema — but all application data will be lost.
3. **Re-inject secrets** from the secrets manager into the cluster's Secret
   objects. Nothing secret comes from the database backup.
4. **Deploy `nacd`** from the image using `deployments/kubernetes`. Migrations
   run on every boot, and `nacd -migrate` can also be run as an explicit step;
   either way the schema is brought current idempotently, each migration in its
   own transaction with all-or-nothing bookkeeping.
5. **Bootstrap an administrator** if the operators table is empty (fresh
   database with no restored data), so that administrative access exists. Use
   the platform's documented bootstrap procedure to create the first operator;
   thereafter manage operators through the normal admin path.
6. **Verify** end to end: `/readyz` green, an evaluate request returns the
   expected decision, and a policy mutation invalidates the Redis cache.

## Redis cold start

After any recovery, Redis starts empty. This requires **no action**:

- The first access evaluations find no `nac:policies:enabled` entry and **fail
  open to the database**, executing the enabled-policy query directly (backed by
  `policies_enabled_priority_idx`).
- The cache repopulates on those requests and subsequent evaluations are served
  from Redis again, subject to the `NAC_REDIS_TTL` (default `30s`) TTL.

The only operational consequence of a cold cache is briefly higher database read
load and slightly higher evaluate latency, never an outage.

## Failover

- **Database failover.** With a primary/standby topology, promote the standby and
  point `nacd` at the new primary by updating the DB DSN secret and recycling
  the pods (or relying on the `1h` max connection lifetime to retire stale
  connections over time). `/readyz` will turn red while the database is
  unreachable and green again once connectivity is restored, which makes
  readiness gating a reliable signal during failover.
- **Application failover.** Handled by Kubernetes: with the shipped Deployment
  (`maxUnavailable: 0`, `maxSurge: 1`) and PDB (`minAvailable: 2`), replica or
  node loss is absorbed without an outage, and the HPA (min 3, max 20) restores
  capacity automatically.

## DR testing cadence

DR procedures are only credible if exercised:

- **Quarterly:** perform a full restore drill into an isolated environment —
  restore the database (including a PITR target), run `nacd -migrate`, bring up
  `nacd`, and verify an evaluate request. Record achieved RPO/RTO and compare to
  the targets above.
- **Semi-annually:** rehearse a region or total-cluster rebuild, including
  re-injecting secrets and bootstrapping an administrator, to validate the
  end-to-end runbook and the recovery-time estimate.
- **After significant change:** re-run the relevant drill whenever the schema,
  backup tooling, or deployment topology changes materially.

For the step-level operational runbook (commands, escalation, rollback), see the
deployment runbook referenced from [release-process.md](release-process.md) and
[incident-response.md](incident-response.md).

## Related documents

- [backup-strategy.md](backup-strategy.md) — what is backed up and how.
- [incident-response.md](incident-response.md) — declaring and coordinating a
  DR incident.
- [scalability.md](scalability.md) — replica, HPA, and PDB behaviour.
- [secrets-management.md](secrets-management.md) — secret sources and rotation.
