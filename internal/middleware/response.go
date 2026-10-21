package middleware

import (
	"fmt"
	"net/http"
	"time"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/problem"
)

// writeForbidden writes a 403 Forbidden RFC 9457 Problem Details response.
// The body is intentionally opaque — distinct detail strings would leak
// session/CSRF state to attackers. The real reason is logged server-side.
func writeForbidden(w http.ResponseWriter, r *http.Request, logDetail string) {
	reqID := RequestIDFrom(r.Context())
	logging.From(r.Context()).WarnContext(r.Context(), "forbidden", "detail", logDetail)
	problem.Write(w, r, http.StatusForbidden, reqID,
		"/problems/forbidden", "Forbidden")
}

// writeInternalError logs the cause once and writes the masked 500 problem:
// a non-sentinel error is an infrastructure failure (docs/patterns/go/errors.md),
// and the client never sees the internal detail.
func writeInternalError(w http.ResponseWriter, r *http.Request, msg string, args ...any) {
	logging.From(r.Context()).ErrorContext(r.Context(), msg, args...)
	problem.Write(w, r, http.StatusInternalServerError, RequestIDFrom(r.Context()),
		"/problems/internal-error", "Internal server error")
}

// writeRateLimited writes a 429 Too Many Requests RFC 9457 Problem Details
// response with a Retry-After header.
func writeRateLimited(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	seconds := max(int(retryAfter.Seconds()), 1)
	reqID := RequestIDFrom(r.Context())
	logging.From(r.Context()).WarnContext(r.Context(), "rate limit exceeded", "retryAfter", seconds)

	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	problem.Write(w, r, http.StatusTooManyRequests, reqID,
		"/problems/rate-limited", "Too many requests")
}
