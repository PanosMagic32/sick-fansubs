package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/middleware"
)

// TestWriteInternalError_LogsOnceAndMasksTheBody pins the one sanctioned 500
// path: a single record at the boundary carrying the failed operation and its
// cause, and a body that discloses neither.
func TestWriteInternalError_LogsOnceAndMasksTheBody(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts", nil)
	req = req.WithContext(middleware.SetRequestID(req.Context(), "req-1"))
	rec := httptest.NewRecorder()

	writeInternalError(rec, req, logger, "blog list query failed", "error", "database is locked")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1 at the boundary:\n%s", len(lines), buf.String())
	}
	var line map[string]any
	if err := json.Unmarshal(lines[0], &line); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if line["level"] != "ERROR" || line["msg"] != "blog list query failed" || line["error"] != "database is locked" {
		t.Errorf("log line = %v, want ERROR/blog list query failed/error", line)
	}

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body["type"] != "/problems/internal-error" || body["requestId"] != "req-1" {
		t.Errorf("body = %v, want the generic problem carrying the request id", body)
	}
	if raw := rec.Body.String(); bytes.Contains([]byte(raw), []byte("database is locked")) {
		t.Errorf("body leaks the internal cause: %s", raw)
	}
}

// TestResponseRequestIDComesOnlyFromTheMiddleware pins where the response's
// request ID comes from: the middleware. A handler served outside it writes no
// ID rather than minting one of its own.
func TestResponseRequestIDComesOnlyFromTheMiddleware(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts", nil)
	rec := httptest.NewRecorder()

	writeJSON(rec, req, http.StatusOK, map[string]string{"status": "ok"})

	if got := rec.Header().Get("X-Request-ID"); got != "" {
		t.Errorf("X-Request-ID = %q, want no header without the middleware", got)
	}
}
