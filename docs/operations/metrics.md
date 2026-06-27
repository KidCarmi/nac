# Metrics

The NAC daemon exports Prometheus metrics via the `client_golang` library. All
collectors are registered on a **private registry** (`prometheus.NewRegistry`)
rather than the global default registry, which keeps the exported set explicit
and avoids accidental third-party metrics. The registry also includes the
standard **Go runtime collector** and **process collector**, so goroutine
counts, memory, garbage collection, CPU, and file-descriptor metrics are
available alongside the application metrics.

Metrics are served at `GET /metrics` via `promhttp`. The endpoint is
**unauthenticated**, consistent with the other operational endpoints
(`/healthz`, `/readyz`, `/version`, `/openapi.yaml`).

## Exported collectors

### `nac_http_requests_total`

| Property | Value |
|----------|-------|
| Type | Counter |
| Labels | `method`, `route`, `status` |
| Meaning | Total HTTP requests served, partitioned by method, matched route, and response status **class**. |

The `status` label is the **status class** — `2xx`, `3xx`, `4xx`, or `5xx` — not
the exact numeric code. This keeps the label set small while still distinguishing
success from client and server errors.

### `nac_http_request_duration_seconds`

| Property | Value |
|----------|-------|
| Type | Histogram (Prometheus `DefBuckets`) |
| Labels | `method`, `route` |
| Meaning | Distribution of request handling latency in seconds. |

Histogram buckets are the client library's default buckets (`DefBuckets`),
suitable for general web latencies. Use `histogram_quantile` over the
`_bucket` series to compute percentiles.

### `nac_access_decisions_total`

| Property | Value |
|----------|-------|
| Type | Counter |
| Labels | `action` |
| Meaning | Count of policy access decisions, partitioned by outcome. |

The `action` label takes the values `allow`, `deny`, and `quarantine` — the same
three actions configurable as the policy-engine fallback via
`NAC_DEFAULT_ACTION` (see [configuration.md](configuration.md)).

### `nac_auth_failures_total`

| Property | Value |
|----------|-------|
| Type | Counter |
| Labels | `reason` |
| Meaning | Count of authentication failures, partitioned by cause. |

The `reason` label is drawn from a fixed, bounded set:

| Reason | Description |
|--------|-------------|
| `missing_bearer` | No bearer token was presented where one was required. |
| `invalid_token` | The presented JWT failed validation. |
| `invalid_service_token` | The presented service token did not match `NAC_SERVICE_TOKEN`. |
| `unknown_operator` | The token referenced an operator that does not exist. |
| `bad_credentials` | Username/password authentication failed. |
| `disabled_operator` | The operator exists but is disabled. |

## The `route` label and cardinality

The `route` label uses **bounded `ServeMux` patterns** taken from
`r.Pattern`, not raw request paths. Examples of route values:

```
GET /api/v1/endpoints/{id}
POST /api/v1/auth/login
unmatched
```

Path parameters such as an endpoint ID appear as the literal pattern segment
`{id}`, so a million distinct endpoint IDs collapse into a single time series
rather than a million. Requests that do not match any registered route are
labeled `unmatched`. This is a deliberate cardinality-control measure: it bounds
the number of `route` values to the number of registered handlers plus the
`unmatched` catch-all.

Combined with the **status class** label (four values) and the bounded `reason`
and `action` label sets, the total series count stays small and predictable. The
high-cardinality `request_id` is intentionally **never** used as a metric label;
it lives only in logs and trace context (see [logging.md](logging.md)).

## Scrape configuration

The Kubernetes deployment already carries the standard Prometheus pod
annotations, so a Prometheus configured with the common Kubernetes pod
service-discovery and relabeling rules will scrape the daemon automatically.
From [`deployments/kubernetes/deployment.yaml`](../../deployments/kubernetes/deployment.yaml):

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"
  prometheus.io/path: "/metrics"
```

For a static or non-annotation-based scrape, the equivalent `prometheus.yml` job
is:

```yaml
scrape_configs:
  - job_name: nacd
    metrics_path: /metrics
    static_configs:
      - targets: ["nacd:8080"]
```

## Example PromQL

**Error rate** (fraction of requests returning 5xx, over 5 minutes):

```promql
sum(rate(nac_http_requests_total{status="5xx"}[5m]))
  /
sum(rate(nac_http_requests_total[5m]))
```

**p99 request latency** (across all routes):

```promql
histogram_quantile(
  0.99,
  sum by (le) (rate(nac_http_request_duration_seconds_bucket[5m]))
)
```

**p99 latency per route** (to isolate a slow endpoint):

```promql
histogram_quantile(
  0.99,
  sum by (le, route) (rate(nac_http_request_duration_seconds_bucket[5m]))
)
```

**Access deny rate** (denies per second over 5 minutes):

```promql
sum(rate(nac_access_decisions_total{action="deny"}[5m]))
```

**Decision mix** (proportion of each action):

```promql
sum by (action) (rate(nac_access_decisions_total[5m]))
```

**Authentication failure rate by reason:**

```promql
sum by (reason) (rate(nac_auth_failures_total[5m]))
```

**Bad-credentials rate** (useful for spotting credential-stuffing):

```promql
sum(rate(nac_auth_failures_total{reason="bad_credentials"}[5m]))
```

**Request throughput by route:**

```promql
sum by (route) (rate(nac_http_requests_total[5m]))
```

## Related documentation

- [Observability](observability.md) — golden signals, dashboards, and alerting.
- [Logging](logging.md) — why `request_id` is not a metric label.
- [Tracing](tracing.md) — W3C trace context.
- [Configuration](configuration.md) — `NAC_DEFAULT_ACTION` and related settings.
