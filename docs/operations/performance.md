# Performance

This document describes the performance characteristics of the NAC daemon
(`cmd/nacd`), where request time is actually spent, and how to size and
benchmark a deployment for the access-evaluation hot path. The platform is
designed so that the operation invoked on every network admission — access
evaluation — is fast, cacheable, and free of per-request blocking I/O wherever
possible, while the deliberately expensive operation — password hashing — is
confined to the rare login path.

## Performance goals

The platform optimises for a single dominant workload: high-volume access
evaluation issued by network enforcement points. The design goals are, in
priority order:

1. **Low, predictable latency on the evaluate hot path.** The access decision
   itself is computed by a pure, in-memory engine with no I/O, so its cost is
   bounded by the size of the enabled-policy set rather than by database or
   network round trips.
2. **Horizontal scalability.** `nacd` is stateless, so throughput scales by
   adding replicas. See [scalability.md](scalability.md).
3. **Bounded resource usage per instance.** Request body size, connection pool
   size, and server timeouts are all capped so that a single misbehaving client
   cannot exhaust a replica.

These goals are reflected in the Kubernetes resource profile shipped in
`deployments/kubernetes`: requests of `100m` CPU / `128Mi` memory and limits of
`1` CPU / `256Mi` memory per replica. A replica is expected to serve its share
of evaluate traffic comfortably within that envelope.

## The cached hot path

The hot path is `POST /api/v1/access/evaluate`. It is authenticated with a
static service token (`NAC_SERVICE_TOKEN`) compared in constant time, which is
inexpensive relative to a password hash. A single evaluation performs the
following work:

1. **Look up the endpoint by MAC address** in PostgreSQL.
2. **Fetch the enabled-policy set.** This is served from the Redis cache under
   the key `nac:policies:enabled` with a TTL of `NAC_REDIS_TTL` (default
   `30s`). On a cache hit, no policy query reaches the database.
3. **Evaluate the decision in the pure engine** (`internal/access/engine.go`).
   The engine is deterministic, performs no I/O, applies conditions as a logical
   AND, and selects the first matching policy by ascending priority
   (first-match-wins), defaulting to `DENY` (configurable via
   `NAC_DEFAULT_ACTION`) when nothing matches.

Because the policy set is small and cacheable, the steady-state cost of an
evaluation is dominated by the endpoint lookup plus an in-memory engine pass.
Policy mutations invalidate the cache, so a configuration change is reflected on
the next evaluation without waiting for the TTL to expire.

```
POST /api/v1/access/evaluate
  -> constant-time service-token check
  -> endpoint lookup by MAC (PostgreSQL)
  -> enabled policies (Redis cache, key "nac:policies:enabled", TTL 30s)
        cache miss -> DB query (partial index policies_enabled_priority_idx)
  -> pure engine: first-match-wins by priority, conditions AND, default DENY
  -> decision (+ one audit_events row)
```

### Cache behaviour and failing open

The Redis cache is an optimisation, not a system of record. If Redis is
unavailable, the platform **fails open to the database**: the enabled-policy
query is executed directly against PostgreSQL. That query is backed by the
partial index `policies_enabled_priority_idx ON (priority) WHERE enabled`, so it
remains cheap even without the cache. The practical effect of a Redis outage is
higher database read load and slightly higher evaluate latency, not an outage of
the evaluate path itself.

## Where time goes

Understanding the latency budget makes capacity planning straightforward.

| Operation | Cost profile | When it runs |
|-----------|--------------|--------------|
| Service-token check | Constant-time string compare, microseconds | Every evaluate request |
| Endpoint lookup by MAC | Single indexed PostgreSQL query | Every evaluate request |
| Enabled-policy fetch | Redis read on hit; indexed DB query on miss | Every evaluate request |
| Engine decision | Pure in-memory pass over enabled policies | Every evaluate request |
| Audit write | One append row into `audit_events` | Every evaluate request |
| **Argon2 password hash** | **Deliberately slow (CPU + memory hard)** | **Login only, never on evaluate** |

The single most important performance fact is that **Argon2 is intentionally
expensive and is confined to the login path**. Password hashing is a security
control: it must be slow enough to frustrate offline cracking. It is therefore
never invoked on the evaluate hot path, which authenticates with a cheap
constant-time service-token comparison instead. Operators should not be alarmed
by high CPU during a burst of administrator logins; that cost is by design and
is isolated from network admission throughput.

## Resource limits that bound request cost

Several limits cap the work any single request or connection can impose:

| Control | Variable / setting | Default | Purpose |
|---------|--------------------|---------|---------|
| Request body cap | `NAC_HTTP_MAX_BODY_BYTES` | `1 MiB` | Rejects oversized payloads before they are buffered |
| Read timeout | server setting | `10s` | Bounds slow request bodies |
| Read-header timeout | server setting | `5s` | Bounds slow-loris header attacks |
| Write timeout | server setting | `15s` | Bounds slow response consumers |
| Idle timeout | server setting | `60s` | Reclaims idle keep-alive connections |

These timeouts protect a replica from resource exhaustion and keep tail latency
predictable under adverse client behaviour.

## Connection pool sizing

Each `nacd` instance owns a pgx v5 connection pool with the following defaults:

| Setting | Variable | Default |
|---------|----------|---------|
| Max connections | `NAC_DB_MAX_CONNS` | `10` |
| Min connections | (fixed) | `2` |
| Max connection lifetime | (fixed) | `1h` |

Because the pool is per-instance, the **total** number of connections opened
against PostgreSQL is `NAC_DB_MAX_CONNS × replica count`. This product, not the
per-instance value, is what must stay within the database's `max_connections`
budget (and within any connection pooler in front of it). When scaling out,
treat connection-pool math as a first-class capacity input:

```
total DB connections (worst case) = NAC_DB_MAX_CONNS * replicas
```

Sizing guidance:

- **Start with the defaults.** Ten connections per replica is ample for a
  workload whose hot path is cache-served and whose remaining DB work is short,
  indexed queries.
- **Raise `NAC_DB_MAX_CONNS` only if you observe pool-wait latency** under load,
  and only after confirming PostgreSQL has headroom in `max_connections`.
- **When running many replicas**, prefer fewer connections per replica plus a
  server-side connection pooler over large per-instance pools.
- The `1h` maximum connection lifetime ensures connections are recycled, which
  helps spread load evenly after a database failover.

## Capacity planning for evaluate throughput

The evaluate path is the workload to plan around. A pragmatic procedure:

1. **Establish per-replica throughput** with a load test that exercises
   `POST /api/v1/access/evaluate` against a warm Redis cache, using a realistic
   distribution of MAC addresses and a representative enabled-policy set.
   Measure sustained requests per second at acceptable p99 latency.
2. **Watch the metrics** during the test (see
   [observability.md](observability.md)):
   - `nac_http_requests_total{route,status}` for throughput and error ratio,
   - `nac_http_request_duration_seconds{route}` for the latency distribution,
   - `nac_access_decisions_total{action}` to confirm the allow/deny mix matches
     expectations.
3. **Divide target peak throughput by measured per-replica throughput** to get a
   replica count, then add headroom for the HPA to react to spikes. The shipped
   HPA targets 70% CPU with `minReplicas: 3` and `maxReplicas: 20`.
4. **Re-check the connection-pool product** against your chosen replica count.
5. **Plan for cache-miss surges.** Model the worst case where Redis is down and
   every evaluation hits the database. The partial index keeps these queries
   cheap, but the database must have headroom for the resulting read volume.

### A note on the in-process rate limiter

The HTTP rate limiter is an **in-process, per-instance fixed window**
(`NAC_HTTP_RATE_LIMIT_PER_MIN`, default `600`). Because it is per-replica, the
effective cluster-wide limit is `N × 600` behind `N` replicas. This is relevant
to capacity planning: the rate limit does not provide a single global ceiling,
and scaling out raises the effective limit proportionally. The documented
upgrade path to a shared Redis-backed limiter is described in
[scalability.md](scalability.md).

## Benchmarking approach

- **Engine microbenchmarks.** The pure decision engine has no I/O and can be
  benchmarked in isolation with Go benchmarks (`go test -bench`), which is the
  right tool for measuring the cost of the decision logic as a function of
  enabled-policy count. See [testing-strategy.md](testing-strategy.md).
- **End-to-end load tests.** For realistic numbers, drive the running service
  with an HTTP load generator against the evaluate endpoint and read latency and
  throughput from the Prometheus histograms rather than from the load tool's own
  client-side timings, so that server-observed latency is what informs sizing.
- **Always test with a warm cache and a separate cold-cache run.** The two
  represent steady state and Redis-outage conditions respectively, and both
  must meet your latency targets.

## Related documents

- [scalability.md](scalability.md) — horizontal scaling, the rate-limiter
  caveat, and the database bottleneck.
- [observability.md](observability.md) — the metrics referenced above.
- [configuration.md](configuration.md) — full environment-variable reference.
