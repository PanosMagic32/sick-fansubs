package middleware

import (
	"net/http"

	"sick-fansubs/internal/id"
	"sick-fansubs/internal/logging"
)

// RequestID mints the server-authoritative request ID and attaches it, with a
// logger scoped to it, to the request context; an inbound X-Request-ID header
// is ignored. A nested chain reuses an ID the context already carries, so one
// request keeps one identity. See: docs/patterns/go/logging.md
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reqID := GetRequestID(r.Context()); reqID != "" {
				w.Header().Set("X-Request-ID", reqID)
				next.ServeHTTP(w, r)
				return
			}

			reqID, err := id.New()
			if err != nil {
				reqID = id.Fallback()
			}
			logger := logging.From(r.Context()).With("requestId", reqID)
			if err != nil {
				logger.ErrorContext(r.Context(), "request id generation failed", "error", err)
			}
			w.Header().Set("X-Request-ID", reqID)
			ctx := logging.With(SetRequestID(r.Context(), reqID), logger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
