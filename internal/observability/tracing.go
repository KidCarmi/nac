package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

// This file implements lightweight W3C Trace Context propagation without a
// heavyweight tracing SDK dependency. It is sufficient to correlate logs and
// to interoperate with an upstream/downstream that speaks `traceparent`. A
// full OpenTelemetry exporter can be layered on later behind the same context
// keys; see docs/operations/tracing.md.

type traceKey struct{}

// TraceContext carries the identifiers needed to correlate a request across
// services and with its log lines.
type TraceContext struct {
	TraceID string // 16-byte hex (32 chars)
	SpanID  string // 8-byte hex (16 chars)
}

// traceparentRE matches W3C traceparent: version-traceid-spanid-flags.
var traceparentRE = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-[0-9a-f]{2}$`)

// FromTraceparent parses an inbound traceparent header, generating a fresh
// trace when the header is absent or malformed. It never errors, so callers
// always obtain a usable TraceContext.
func FromTraceparent(header string) TraceContext {
	if m := traceparentRE.FindStringSubmatch(header); m != nil {
		return TraceContext{TraceID: m[1], SpanID: newID(8)}
	}
	return TraceContext{TraceID: newID(16), SpanID: newID(8)}
}

// Traceparent renders the context as a W3C traceparent header value with the
// sampled flag set.
func (t TraceContext) Traceparent() string {
	return "00-" + t.TraceID + "-" + t.SpanID + "-01"
}

// WithTrace stores the trace context for the request.
func WithTrace(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceKey{}, tc)
}

// TraceFrom returns the request's trace context, if any.
func TraceFrom(ctx context.Context) (TraceContext, bool) {
	tc, ok := ctx.Value(traceKey{}).(TraceContext)
	return tc, ok
}

func newID(n int) string {
	b := make([]byte, n)
	// crypto/rand.Read never returns a short read on supported platforms; on
	// the impossible error path we fall back to a zero ID rather than panic,
	// keeping request handling resilient.
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString(make([]byte, n))
	}
	return hex.EncodeToString(b)
}
