# Observability

The NAC daemon is instrumented across the three classic pillars of
observability — **logs**, **metrics**, and **traces** — wired together so that a
single request can be followed across all three. This document gives the
high-level overview and explains the correlation model; the per-pillar detail
lives in dedicated documents linked at the end.

## The three pillars

| Pillar | Implementation | Where it goes | Detail |
|--------|----------------|---------------|--------|
| Logs | `log/slog`, JSON by default, written to **stderr**, with secret redaction | Container stdout/stderr collector | [logging.md](logging.md) |
| Metrics | Prometheus `client_golang` on a private registry, exposed at `GET /metrics` | Prometheus scrape | [metrics.md](metrics.md) |
| Traces | Lightweight **W3C Trace Context** propagation, no OpenTelemetry SDK dependency | Header propagation; ID echoed in logs and responses | [tracing.md](tracing.md) |

All three pillars are populated by the same HTTP middleware chain, so they share
a consistent view of each request without any per-handler instrumentation.

## Correlation: how the pillars line up

The platform's tracing layer (`internal/observability/tracing.go`) parses an
inbound `traceparent` header in the W3C format
`00-<trace-id>-<span-id>-<flags>`, generating fresh identifiers when the header
is absent or malformed. It stores a `TraceContext` in the request context and
**uses the span id as the `request_id`** in both logs and audit records.

This produces a tight correlation chain:

```
traceparent header  ->  TraceContext in ctx
                         |-- trace_id  -> log field, future trace export
                         |-- span id   -> request_id (logs + audit)
                                       -> X-Request-Id response header
```

A request-scoped logger is attached to the context carrying these fields:

| Field | Source | Meaning |
|-------|--------|---------|
| `request_id` | span id from trace context | Unique per request; echoed to the client as `X-Request-Id`. |
| `trace_id` | trace id from `traceparent` | Stable across a distributed request path. |
| `method` | HTTP method | e.g. `GET`, `POST`. |
| `path` | request path | The concrete path of the request. |

Because `request_id` is also returned to the client in the `X-Request-Id`
response header, a user or upstream service that reports a problem can quote the
ID, and an operator can pivot directly to the matching log records. The same
`trace_id` ties together every hop that propagated the `traceparent` header.

Metrics do not carry the high-cardinality request id (that would explode the
time series). Instead they share the **route** dimension with the logs — the
bounded `ServeMux` pattern such as `GET /api/v1/endpoints/{id}` — so an anomaly
spotted on a metrics dashboard can be narrowed to a route, and the matching log
records for that route then provide per-request `request_id` values to trace
individual failures.

## Golden signals

The four golden signals map directly onto the exported Prometheus metrics:

| Signal | Backing metric(s) |
|--------|-------------------|
| **Latency** | `nac_http_request_duration_seconds` (histogram, default buckets, labeled by `method` and `route`). |
| **Traffic** | `nac_http_requests_total` (counter, labeled by `method`, `route`, and `status` class). |
| **Errors** | `nac_http_requests_total` filtered to the `5xx` (and as needed `4xx`) status class. |
| **Saturation** | Go and process collectors registered on the private registry (goroutines, memory, file descriptors, CPU), plus inferred saturation from latency and the PostgreSQL connection pool sizing. |

Two NAC-specific signals augment the standard four:

- **Access decisions** — `nac_access_decisions_total{action}` for
  `allow` / `deny` / `quarantine`. The deny and quarantine rates are
  security-relevant and worth their own panels.
- **Authentication failures** — `nac_auth_failures_total{reason}` across the
  reasons `missing_bearer`, `invalid_token`, `invalid_service_token`,
  `unknown_operator`, `bad_credentials`, and `disabled_operator`.

## Dashboards to build

A practical Grafana layout, all sourced from the metrics in
[metrics.md](metrics.md):

1. **Request overview** — request rate by route, error-class breakdown, and a
   latency heatmap or p50/p90/p99 lines from the duration histogram.
2. **Latency detail** — per-route p99 latency, to spot a single slow endpoint
   amid healthy aggregate numbers.
3. **Access decisions** — stacked `allow` / `deny` / `quarantine` rates over
   time, with deny/quarantine called out for security review.
4. **Authentication health** — `nac_auth_failures_total` broken down by
   `reason`, useful for distinguishing a credential-stuffing attempt
   (`bad_credentials`) from a misconfigured client (`invalid_service_token`).
5. **Runtime saturation** — goroutines, heap, and process CPU from the Go and
   process collectors.

## Alerting suggestions

These alerts reference only metrics that the system actually exports. Tune
thresholds to your traffic profile.

| Alert | Condition (PromQL sketch) | Why it matters |
|-------|---------------------------|----------------|
| High 5xx error rate | `sum(rate(nac_http_requests_total{status="5xx"}[5m])) / sum(rate(nac_http_requests_total[5m])) > 0.02` | Server-side failures affecting clients. |
| High request latency | `histogram_quantile(0.99, sum by (le) (rate(nac_http_request_duration_seconds_bucket[5m]))) > 1` | p99 latency regression. |
| Auth failure spike | `sum(rate(nac_auth_failures_total[5m])) > <baseline>` | Possible attack or broken client credentials. |
| Bad-credentials surge | `sum(rate(nac_auth_failures_total{reason="bad_credentials"}[5m])) > <baseline>` | Potential credential-stuffing. |
| Elevated deny rate | `sum(rate(nac_access_decisions_total{action="deny"}[5m]))` rising sharply | Misconfigured policy or an active probing source. |
| Readiness failing | `/readyz` returning 503 (probe-level alert) | Database connectivity lost. |

Note that liveness (`/healthz`) intentionally performs no dependency checks,
while readiness (`/readyz`) pings the database and returns 503 when it is down —
so alert on readiness, not liveness, for downstream outages.

## Related documentation

- [Logging](logging.md)
- [Metrics](metrics.md)
- [Tracing](tracing.md)
- [Configuration](configuration.md) — `NAC_LOG_LEVEL`, `NAC_LOG_FORMAT`, and
  related observability settings.
