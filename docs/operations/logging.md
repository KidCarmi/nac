# Logging

The NAC daemon logs through the Go standard library's structured logging package
`log/slog`. Logging is configured once at startup by
`internal/observability/logging.go` via `NewLogger(level, format)`, which builds
either a JSON handler (the default) or a text handler.

## Destination and format

- **Destination:** all logs are written to **stderr**. In containerized
  deployments this is collected by the platform's log pipeline. The application
  does not write log files itself.
- **Format:** controlled by `NAC_LOG_FORMAT` — `json` (default) or `text`.
  JSON is recommended for production because it is machine-parseable and
  preserves the structured attributes used for correlation. Text is convenient
  for local development.
- **Level:** controlled by `NAC_LOG_LEVEL` — one of `debug`, `info` (default),
  `warn`, `error`. Records below the configured level are dropped.

See [configuration.md](configuration.md) for these environment variables.

## Request-scoped fields

A request-scoped logger is attached to the request context by the trace+logger
middleware. Retrieve it with `observability.LoggerFrom(ctx)`, which returns the
context logger when present or the default logger otherwise. Every log record
emitted through the context logger carries:

| Field | Meaning |
|-------|---------|
| `request_id` | Unique identifier for the request — the span id from the W3C trace context. Also returned to the client as the `X-Request-Id` response header. |
| `trace_id` | Trace identifier from the inbound `traceparent` header (or freshly generated). Stable across a distributed request path. |
| `method` | HTTP method of the request. |
| `path` | Request path. |

Because `request_id` equals the response `X-Request-Id` header and `trace_id` is
shared across hops, logs correlate cleanly with metrics (by route) and traces
(by trace id). See [observability.md](observability.md) for the full correlation
model and [tracing.md](tracing.md) for how the IDs are derived.

## Secret redaction

The slog handler installs a `ReplaceAttr` hook that **redacts sensitive
attribute keys** before any record is written. When an attribute's key matches
one of the protected keys (case considerations aside), its value is replaced with
the literal string `[REDACTED]`. The protected keys are:

```
password
password_hash
authorization
token
service_token
jwt
secret
signing_key
set-cookie
cookie
```

This protection applies at every log level and for both JSON and text output. A
secret attached to a log record under any of these keys never reaches stderr.

### Guidance: do not log secrets

Redaction protects the known sensitive keys, but it cannot scrub secrets that are
formatted in ways it cannot see. Follow these rules:

- **Attach sensitive values only under the protected keys**, never under an
  arbitrary key name that is not on the redaction list.
- **Never interpolate a secret into a free-form log message string.** The
  redaction hook operates on keyed attributes, not on message text, so a token
  embedded in a message would be written verbatim.
- **Do not log entire request objects, headers, or environment maps** that may
  contain `Authorization`, `Cookie`, or DSN credentials in unexpected places.
- Prefer logging stable identifiers (the `request_id`) over the underlying
  sensitive material when correlating events.

See [secrets-management.md](secrets-management.md) for the platform's overall
never-log guarantees.

## Example log lines

A typical request log record in JSON format (default):

```json
{
  "time": "2026-06-27T09:14:22.481Z",
  "level": "INFO",
  "msg": "request completed",
  "request_id": "4bf92f3577b34da6",
  "trace_id": "0af7651916cd43dd8448eb211c80319c",
  "method": "GET",
  "path": "/api/v1/endpoints/42",
  "status": 200
}
```

An authentication failure, with the offending credential redacted:

```json
{
  "time": "2026-06-27T09:15:03.207Z",
  "level": "WARN",
  "msg": "authentication failed",
  "request_id": "a1c8f4b2d9e07c33",
  "trace_id": "0af7651916cd43dd8448eb211c80319c",
  "method": "POST",
  "path": "/api/v1/auth/login",
  "reason": "bad_credentials",
  "authorization": "[REDACTED]"
}
```

The same record in text format (`NAC_LOG_FORMAT=text`):

```
time=2026-06-27T09:15:03.207Z level=WARN msg="authentication failed" request_id=a1c8f4b2d9e07c33 trace_id=0af7651916cd43dd8448eb211c80319c method=POST path=/api/v1/auth/login reason=bad_credentials authorization=[REDACTED]
```

## Querying by request_id

Because the span id is reused as both the log `request_id` and the
`X-Request-Id` response header, an operator can pivot from a client-reported
identifier straight to the relevant logs.

When a client reports a failed request, ask for the `X-Request-Id` value and
search your log store for it. For example, against JSON logs with `jq`:

```bash
# Pull every record for a given request id from a captured log stream
jq 'select(.request_id == "a1c8f4b2d9e07c33")' app.log
```

In a log platform that indexes JSON fields, the equivalent query is a field
match such as `request_id:"a1c8f4b2d9e07c33"`. To follow a request across
multiple services that propagated the trace, search instead by the shared
`trace_id`:

```bash
jq 'select(.trace_id == "0af7651916cd43dd8448eb211c80319c")' app.log
```

## Related documentation

- [Observability](observability.md) — how logs, metrics, and traces correlate.
- [Metrics](metrics.md) — Prometheus collectors and PromQL examples.
- [Tracing](tracing.md) — derivation of `request_id` and `trace_id`.
- [Configuration](configuration.md) — `NAC_LOG_LEVEL` and `NAC_LOG_FORMAT`.
- [Secrets management](secrets-management.md) — never-log guarantees.
