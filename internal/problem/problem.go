// Package problem provides a shared RFC 9457 Problem Details response writer.
//
// Both the handler and middleware packages need to write structured error
// responses (403 Forbidden, 429 Too Many Requests, etc.). This package is
// the single canonical source — a deep module with a small surface,
// eliminating duplicated header-setting and JSON-encoding across callers.
//
// The writer emits the members the wire contract declares: type, title,
// status, requestId, and endpoint-specific extensions (the 422 violations
// array). [RFC 9457]'s detail and instance members are not part of the
// contract, and extensions cannot inject them.
//
// [RFC 9457]: https://www.rfc-editor.org/rfc/rfc9457
package problem

import (
	"encoding/json"
	"net/http"

	"sick-fansubs/internal/logging"
)

// Write writes an RFC 9457 Problem Details response with no extension members.
//
// The requestID parameter is the server-authoritative correlation ID.
// Callers obtain it from their context (middleware.GetRequestID); an empty
// value omits the member and its header.
func Write(w http.ResponseWriter, r *http.Request, status int, requestID, problemType, title string) {
	WriteWithExtensions(w, r, status, requestID, problemType, title, nil)
}

// WriteWithExtensions writes an RFC 9457 Problem Details response with
// additional extension members (RFC 9457 §3.2). Extensions are endpoint-
// specific, accepted contract shapes — e.g. the 422 "violations" array.
//
// Values in extensions must already be safe public contract values; this
// function does no sanitization. Never pass internal error strings.
func WriteWithExtensions(w http.ResponseWriter, r *http.Request, status int, requestID, problemType, title string, extensions map[string]any) {
	body := map[string]any{
		"type":   problemType,
		"title":  title,
		"status": status,
	}
	if requestID != "" {
		body["requestId"] = requestID
	}
	// Reserved members are never silently overwritten by extensions — the
	// core shape (type/title/status/requestId) stays authoritative, and the
	// RFC's detail/instance members are not part of this contract.
	for k, v := range extensions {
		switch k {
		case "type", "title", "status", "requestId", "detail", "instance":
			continue
		}
		body[k] = v
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	if requestID != "" {
		w.Header().Set("X-Request-ID", requestID)
	}
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		logging.From(r.Context()).ErrorContext(r.Context(), "problem response write failed", "error", err)
	}
}
