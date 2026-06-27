# Tracing

The NAC daemon implements **distributed trace propagation** using the
[W3C Trace Context](https://www.w3.org/TR/trace-context/) standard, without
taking on an OpenTelemetry SDK dependency. The implementation in
`internal/observability/tracing.go` is deliberately lightweight: it parses and
emits the `traceparent` header and threads a `TraceContext` through the request
context, but it does not export spans to a collector. This keeps the binary
dependency-free for tracing while preserving the standard identifiers needed to
correlate requests today and to layer in a full exporter later.

## What is and is not implemented

| Implemented | Not (yet) implemented |
|-------------|------------------------|
| Parsing the inbound `traceparent` header | OpenTelemetry SDK / API dependency |
| Generating fresh trace and span IDs when none are supplied | Span export to a collector (OTLP, Jaeger, etc.) |
| Storing `TraceContext` in the request context | Child-span creation per internal operation |
| Echoing `traceparent` and `X-Request-Id` response headers | Sampling configuration beyond passthrough |
| Reusing the span id as `request_id` in logs and audit | — |

## The `traceparent` header

The W3C `traceparent` header has the form:

```
00-<trace-id>-<span-id>-<flags>
```

| Field | Length | Meaning |
|-------|--------|---------|
| version | 2 hex | Format version; `00` for the current version. |
| trace-id | 32 hex | Identifier shared across all hops of a distributed request. |
| span-id | 16 hex | Identifier for this particular hop/operation. |
| flags | 2 hex | Trace flags (for example the sampled bit). |

On each inbound request, the tracing layer:

1. Reads the `traceparent` header if present.
2. **Parses** it into its component IDs.
3. If the header is **missing or malformed**, generates fresh, valid trace and
   span IDs so every request always has a well-formed context.
4. Stores the resulting `TraceContext` in the request context for downstream use.

## How the IDs flow

```
inbound request
   |
   |  traceparent: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
   v
[trace+logger middleware]
   |  parse (or generate) -> TraceContext{ trace_id, span_id }
   |
   |-- trace_id  --> request-scoped log field "trace_id"
   |-- span id   --> request-scoped log field "request_id"
   |             --> audit record correlation id
   |
   v
response headers:
   traceparent: 00-0af7651916cd43dd8448eb211c80319c-<span-id>-...
   X-Request-Id: <span-id>
```

Key points:

- The **span id is reused as the `request_id`** in logs and audit records. A
  single value therefore appears as the log `request_id`, the audit correlation
  id, and the `X-Request-Id` response header.
- The **`trace_id`** is stable across every service that propagates the
  `traceparent` header, making it the join key for a distributed request.
- The middleware **echoes** both `traceparent` and `X-Request-Id` on the
  response, so a client always learns the identifier for the request it just
  made.

This is the same correlation model described in
[observability.md](observability.md); for the resulting log fields and example
records see [logging.md](logging.md).

## Response headers

| Header | Value | Purpose |
|--------|-------|---------|
| `traceparent` | `00-<trace-id>-<span-id>-<flags>` | Lets the caller (and any tracing-aware intermediary) continue the trace. |
| `X-Request-Id` | the span id | Human-quotable identifier for support and log lookup. |

Because `X-Request-Id` equals the log `request_id`, a user who reports a problem
can simply quote the header value, and an operator can find the matching log
records immediately. See the querying guidance in [logging.md](logging.md).

## Middleware placement

Trace context establishment happens early in the middleware chain — in the
`trace+logger` stage, immediately after the panic recoverer and before security
headers, metrics/access logging, rate limiting, and body limiting. This ordering
guarantees that every subsequent layer, including the metrics observer and the
access log, runs with a populated `TraceContext` and a request-scoped logger
already in the context.

## Privacy of identifiers

Trace and span IDs are randomly generated identifiers and carry **no secret
material**. They are safe to emit in response headers and logs. Secrets are never
placed into the trace context; the redaction guarantees in
[secrets-management.md](secrets-management.md) cover the logging path
independently.

## Future work: a full OpenTelemetry exporter

The design intentionally leaves room to adopt OpenTelemetry without reworking
correlation. The plan:

1. **Keep the context keys.** A future OTel exporter can be layered behind the
   same context keys already used for `TraceContext`, so existing call sites and
   log fields continue to work unchanged.
2. **Adopt the OTel SDK** for span creation and add child spans around internal
   operations (database calls, cache lookups, policy evaluation), reusing the
   trace and span IDs already parsed from `traceparent`.
3. **Add an OTLP exporter** to ship spans to a collector (Jaeger, Tempo, or any
   OTLP-compatible backend).
4. **Introduce sampling configuration** beyond the current passthrough behavior.

Because the platform already speaks W3C Trace Context on the wire, it
interoperates with OpenTelemetry-instrumented neighbors today — propagated trace
IDs line up — and the migration to full span export becomes additive rather than
a breaking change.

## Related documentation

- [Observability](observability.md) — the three pillars and the correlation
  model.
- [Logging](logging.md) — how `trace_id` and `request_id` appear in logs and how
  to query by them.
- [Metrics](metrics.md) — why high-cardinality IDs stay out of metric labels.
- [Configuration](configuration.md) — observability-related settings.
