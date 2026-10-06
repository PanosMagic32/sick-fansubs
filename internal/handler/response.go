package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/problem"
)

// setRequestIDHeader stamps the response with the ID the RequestID middleware
// minted for this request. A request that skipped the middleware carries no ID
// and gets no header — production route wiring always applies it.
func setRequestIDHeader(w http.ResponseWriter, r *http.Request) {
	if id := middleware.GetRequestID(r.Context()); id != "" {
		w.Header().Set("X-Request-ID", id)
	}
}

// formatAPITime converts a UTC Unix-millisecond value to the canonical API
// instant: RFC 3339 UTC with exactly millisecond precision
// (e.g. "2026-07-23T12:34:56.789Z").
func formatAPITime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// setAPIHeaders writes the baseline headers every API
// response carries: the authoritative request ID and the default no-store
// cache control. writeJSON and writeNoContent share it so the preamble
// cannot drift between bodyful and bodyless successes.
func setAPIHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	setRequestIDHeader(w, r)
}

// writeJSON writes a JSON success response with the standard API headers.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	setAPIHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logging.From(r.Context()).ErrorContext(r.Context(), "JSON response write failed", "error", err)
	}
}

// writeProblem writes an RFC 9457 Problem Details response.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, problemType, title string) {
	problem.Write(w, r, status, middleware.GetRequestID(r.Context()), problemType, title)
}

// writeInternalError is the only sanctioned 500 response for JSON endpoints:
// the caller names the failed operation, the request-scoped logger records it
// once at this boundary, and the body stays generic.
func writeInternalError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, msg string, args ...any) {
	logger.ErrorContext(r.Context(), msg, args...)
	writeProblem(w, r, http.StatusInternalServerError,
		"/problems/internal-error", "Internal server error")
}

// writeBadRequest writes the 400 problem for a malformed request.
func writeBadRequest(w http.ResponseWriter, r *http.Request, logDetail string) {
	logging.From(r.Context()).WarnContext(r.Context(), "bad request", "detail", logDetail)
	writeProblem(w, r, http.StatusBadRequest,
		"/problems/bad-request", "Bad Request")
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, logDetail string) {
	logging.From(r.Context()).WarnContext(r.Context(), "unauthorized", "detail", logDetail)
	writeProblem(w, r, http.StatusUnauthorized,
		"/problems/auth/invalid-credentials", "Invalid credentials")
}

func writeForbidden(w http.ResponseWriter, r *http.Request, logDetail string) {
	logging.From(r.Context()).WarnContext(r.Context(), "forbidden", "detail", logDetail)
	writeProblem(w, r, http.StatusForbidden,
		"/problems/forbidden", "Forbidden")
}

func writeConflict(w http.ResponseWriter, r *http.Request, logDetail string) {
	logging.From(r.Context()).WarnContext(r.Context(), "conflict", "detail", logDetail)
	writeProblem(w, r, http.StatusConflict,
		"/problems/conflict", "Conflict")
}

// writeTooManyRequests writes a 429 Too Many Requests problem with a
// Retry-After header, and is the one place a handler-level rate-limit
// rejection is logged: the caller passes its bucket fields.
func writeTooManyRequests(w http.ResponseWriter, r *http.Request, retryAfter time.Duration, args ...any) {
	// Clamp to at least one second: a sub-second limiter window would round
	// down to 0, and a 0-second Retry-After invites an immediate retry loop
	// against the same bucket. The OpenAPI RateLimited contract types
	// Retry-After as integer seconds.
	seconds := max(int(retryAfter.Seconds()), 1)
	attrs := slices.Concat(args, []any{"retryAfter", seconds})
	logging.From(r.Context()).WarnContext(r.Context(), "rate limit exceeded", attrs...)
	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	writeProblem(w, r, http.StatusTooManyRequests,
		"/problems/rate-limited", "Too many requests")
}

// writePayloadTooLarge writes a 413 Payload Too Large problem — the endpoint
// body limit contract.
func writePayloadTooLarge(w http.ResponseWriter, r *http.Request) {
	logging.From(r.Context()).WarnContext(r.Context(), "request body too large")
	writeProblem(w, r, http.StatusRequestEntityTooLarge,
		"/problems/payload-too-large", "Payload too large")
}

// writeUnsupportedMediaType writes the 415 problem for a request body sent
// with a media type the endpoint does not accept.
func writeUnsupportedMediaType(w http.ResponseWriter, r *http.Request) {
	logging.From(r.Context()).WarnContext(r.Context(), "unsupported request media type")
	writeProblem(w, r, http.StatusUnsupportedMediaType,
		"/problems/unsupported-media-type", "Unsupported media type")
}

// writeJSONBodyError maps a readJSON failure to its problem response —
// shared by every body-reading endpoint. The classification it consumes is
// owned by [classifyDecodeError].
func writeJSONBodyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errBodyTooLarge):
		writePayloadTooLarge(w, r)
	case errors.Is(err, errUnsupportedMediaType):
		writeUnsupportedMediaType(w, r)
	default:
		writeBadRequest(w, r, err.Error())
	}
}

// NotFound writes the API 404 problem response. It is registered as the
// catch-all under /api/v1/ so unknown API paths return a JSON problem instead
// of falling through to the SPA HTML navigation response.
func NotFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusNotFound, "/problems/not-found", "Not found")
}

// Violation is a field-level validation error for RFC 9457 problem responses.
//
// The wire shape is exactly { field, code } (OpenAPI: components/schemas/Problem
// → violations with additionalProperties: false). Internal validator text is
// never public API — the frontend maps stable codes to the
// Greek catalog. The warn log carries the field:code pairs; the internal
// message text is discarded at this boundary.
type Violation struct {
	Field string `json:"field"` // the offending request field
	Code  string `json:"code"`  // the stable violation code (docs/patterns/go/errors.md rule 14)
}

// writeValidationErrors writes a 422 with per-field violations. Extra
// attributes are logged on the same record as the violations.
func writeValidationErrors(w http.ResponseWriter, r *http.Request, violations []Violation, args ...any) {
	writeViolationProblems(w, r, http.StatusUnprocessableEntity, violations, args...)
}

// writeViolationProblems writes a problem carrying per-field violations at
// an explicit status. The upload endpoint uses the same
// {field, code} shape for its 413 size-bound outcomes, so the status is a
// parameter instead of a second writer.
func writeViolationProblems(w http.ResponseWriter, r *http.Request, status int, violations []Violation, args ...any) {
	reqID := middleware.GetRequestID(r.Context())

	fields := make([]string, 0, len(violations))
	for _, v := range violations {
		fields = append(fields, fmt.Sprintf("%s:%s", v.Field, v.Code))
	}
	attrs := append([]any{"violations", fields}, args...)
	logging.From(r.Context()).WarnContext(r.Context(), "validation failed", attrs...)

	problem.WriteWithExtensions(w, r, status, reqID,
		"/problems/validation", "Validation failed",
		map[string]any{"violations": violations})
}
