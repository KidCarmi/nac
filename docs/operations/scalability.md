# Scalability

This document explains how the NAC platform scales, what limits scaling, and the
operational levers available to grow capacity. The short version: `nacd` is a
stateless binary that scales horizontally without coordination, so the scaling
conversation is almost entirely about the shared stateful dependency —
PostgreSQL — and about two specific caveats: the per-instance rate limiter and
the unbounded growth of the audit table.

## Stateless horizontal scaling

Every `nacd` instance is **stateless**. It holds no session state, no
sticky in-memory data that must survive a restart, and no peer coordination. All
durable state lives in PostgreSQL (the system of record) and an optional Redis
cache. Consequently:

- Any replica can serve any request.
- Replicas can be added or removed at will; there is no rebalancing step.
- A rolling deployment or a crash loses no data, because no data lives in the
  process.

This is what makes horizontal scaling trivial: to serve more traffic, run more
replicas. Throughput scales close to linearly with replica count until a shared
dependency becomes the bottleneck.

## Kubernetes autoscaling: HPA and PDB

The manifests in `deployments/kubernetes` encode the intended scaling posture.

| Object | Setting | Value |
|--------|---------|-------|
| Deployment | replicas | `3` |
| Deployment | rolling update | `maxUnavailable: 0`, `maxSurge: 1` |
| HorizontalPodAutoscaler | target | CPU `70%` |
| HorizontalPodAutoscaler | range | `minReplicas: 3`, `maxReplicas: 20` |
| PodDisruptionBudget | `minAvailable` | `2` |

- The **HPA** scales the Deployment between 3 and 20 replicas to hold average
  CPU near 70%. CPU is a good proxy here because the evaluate hot path is
  CPU-and-cache bound rather than I/O bound in steady state.
- `maxUnavailable: 0` with `maxSurge: 1` means rolling updates add a new pod
  before removing an old one, so capacity never dips during a deploy.
- The **PDB** with `minAvailable: 2` ensures voluntary disruptions (node
  drains, cluster upgrades) never take the service below two replicas.

The pods run with a hardened security context — `readOnlyRootFilesystem`, all
capabilities dropped, `runAsNonRoot` as UID `65532`, and the `RuntimeDefault`
seccomp profile — none of which affects scaling but all of which is preserved
across scale-out.

## The database is the scaling bottleneck

Because the application layer scales freely, **PostgreSQL is the component that
ultimately limits how far the platform scales.** Two distinct pressures apply:

1. **Connection count.** Each replica opens up to `NAC_DB_MAX_CONNS` (default
   `10`) connections, so the cluster-wide total is `NAC_DB_MAX_CONNS ×
   replicas`. At the HPA ceiling of 20 replicas with defaults, that is up to 200
   connections. This product must stay within the database's `max_connections`,
   accounting for other clients. When approaching the limit, introduce a
   server-side connection pooler (for example PgBouncer in transaction mode)
   rather than raising per-replica pools.

2. **Read query volume.** The endpoint lookup and the enabled-policy query are
   the per-evaluation database work. The policy query is normally absorbed by
   the Redis cache; the endpoint lookup is not.

### Read replicas

The dominant database workload is reads (endpoint lookups, policy reads). This
is naturally served by **read replicas**: route read-mostly query traffic to one
or more PostgreSQL replicas while keeping the primary for writes (policy
mutations, audit appends, endpoint registration). Because the evaluate path's
expensive-to-cache query — the endpoint lookup — is a read, read replicas are
the primary horizontal-scaling lever for the database tier once a single primary
is saturated.

## The Redis cache role

Redis caches the enabled-policy set under `nac:policies:enabled` with a TTL of
`NAC_REDIS_TTL` (default `30s`), and is invalidated on every policy mutation.
Its role in scaling:

- It **removes the policy query from the hot path** on cache hits, which is the
  single biggest reduction in per-evaluation database load.
- It is **optional and fails open to the database** if unavailable. A Redis
  outage therefore degrades performance (more DB reads, slightly higher
  latency) but does not stop evaluations. The fallback policy query is backed by
  the partial index `policies_enabled_priority_idx`, keeping it cheap.

Because Redis only caches a small, slowly changing dataset, it does not itself
become a scaling bottleneck under normal operation. It should be sized for
availability, not throughput.

## Caveat: the per-instance rate limiter

The HTTP rate limiter is an **in-process, per-instance fixed window**
controlled by `NAC_HTTP_RATE_LIMIT_PER_MIN` (default `600` requests per
minute). It is **not** a global limit.

This has a direct consequence for scaling:

```
effective cluster rate limit = NAC_HTTP_RATE_LIMIT_PER_MIN * replicas
```

Behind `N` replicas the effective ceiling is `N × 600`, and it moves as the HPA
scales pods in and out. Operators must not treat `NAC_HTTP_RATE_LIMIT_PER_MIN`
as a fixed global throttle. It is a per-replica safety valve against a single
abusive client overwhelming a single instance, not a fleet-wide quota.

### Upgrade path: a shared Redis-backed limiter

The documented upgrade path is a **shared rate limiter backed by Redis**. By
maintaining the request counters in Redis rather than in process memory, all
replicas share a single window and the configured limit becomes a true
cluster-wide ceiling that is independent of replica count. Until that upgrade is
in place, plan around the per-instance behaviour described above.

## Audit table growth and partitioning

The `audit_events` table is **append-only and grows unbounded with activity**.
The platform writes one coarse row per access decision plus one row per
administrative mutation. On a busy network this is the fastest-growing table by
a wide margin, and unbounded growth eventually degrades both write performance
and storage economics.

Scaling strategy for audit data:

- **Partition `audit_events` by time** (for example, monthly range partitions on
  the event timestamp). Time partitioning keeps individual partitions small,
  makes retention enforcement a cheap `DROP`/`DETACH` of old partitions rather
  than a large `DELETE`, and improves the locality of recent-event queries.
- **Archive and prune old partitions** according to the retention policy. See
  [backup-strategy.md](backup-strategy.md) for retention, archival, and backup
  of audit data.
- The other tables scale benignly: `endpoints` grows with the device
  population (large but bounded by the number of real devices), while
  `policies`, `operators`, and `schema_migrations` stay small.

## Scaling checklist

Use this checklist when planning or executing a scale-up:

- [ ] Confirm target peak evaluate throughput and divide by measured
      per-replica throughput (see [performance.md](performance.md)) to set a
      replica target.
- [ ] Verify the HPA range (`minReplicas`/`maxReplicas`) covers the target with
      headroom; raise `maxReplicas` if needed.
- [ ] Recompute the connection-pool product `NAC_DB_MAX_CONNS × replicas` and
      confirm it fits within PostgreSQL `max_connections`; add a connection
      pooler before raising per-replica pools.
- [ ] Confirm PostgreSQL has read headroom for the worst case where Redis is
      down and every evaluation hits the database.
- [ ] Add read replicas if the primary's read load is the constraint.
- [ ] Size Redis for availability; confirm the fail-open path has been load
      tested.
- [ ] Account for the per-instance rate limiter: the effective ceiling is
      `N × NAC_HTTP_RATE_LIMIT_PER_MIN`. Plan the Redis-limiter upgrade if a
      true global limit is required.
- [ ] Ensure `audit_events` partitioning and retention are in place before audit
      volume becomes a storage or performance problem.
- [ ] Verify the PDB (`minAvailable: 2`) still leaves the desired availability
      margin at the new scale.

## Related documents

- [performance.md](performance.md) — latency budget, pool sizing, capacity
  planning.
- [backup-strategy.md](backup-strategy.md) — audit retention and archival.
- [disaster-recovery.md](disaster-recovery.md) — failover and rebuild.
- [configuration.md](configuration.md) — environment-variable reference.
