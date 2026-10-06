package logging

import (
	"context"
	"log/slog"
)

// ctxKeyLogger is the context key type for the request-scoped logger.
type ctxKeyLogger struct{}

// With returns a context that carries logger, so request-scoped code obtains it
// through From. Middleware.RequestID scopes the process logger this way; a test
// that asserts log output injects a capture logger the same way.
func With(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKeyLogger{}, logger)
}

// From returns the logger the context carries, or slog.Default() — the
// process-wide handler main installs — when it carries none. A request that
// skipped the RequestID middleware still logs; callers never nil-check.
func From(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(ctxKeyLogger{}).(*slog.Logger)
	if !ok || logger == nil {
		return slog.Default()
	}
	return logger
}
