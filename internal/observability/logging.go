// Package observability wires structured logging, Prometheus metrics, and
// trace-context propagation. The three pillars share a single request
// correlation identifier so a log line, a metric exemplar, and a trace can all
// be joined by operators during incident response.
package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// NewLogger builds a slog.Logger from level/format strings. Unknown levels
// default to info. The logger writes to stderr so stdout remains free for
// any future structured data output.
func NewLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: lvl,
		// Redact common secret-bearing keys defensively in case they are ever
		// passed to the logger by mistake.
		ReplaceAttr: redactSecrets,
	}

	var h slog.Handler
	if strings.ToLower(format) == "text" {
		h = slog.NewTextHandler(os.Stderr, opts)
	} else {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(h)
}

var secretKeys = map[string]struct{}{
	"password": {}, "password_hash": {}, "authorization": {},
	"token": {}, "service_token": {}, "jwt": {}, "secret": {},
	"signing_key": {}, "set-cookie": {}, "cookie": {},
}

func redactSecrets(_ []string, a slog.Attr) slog.Attr {
	if _, ok := secretKeys[strings.ToLower(a.Key)]; ok {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}

type loggerKey struct{}

// WithLogger stores a logger (typically request-scoped with correlation
// fields) in the context.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// LoggerFrom returns the context logger, or the default logger if none is set,
// so callers never have to nil-check.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
