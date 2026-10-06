package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWrite_EmptyRequestIDOmitsMemberAndHeader pins the omission rule: the
// requestId member and the X-Request-ID header appear only when a correlation
// ID was minted (the two fields are mirrors of each other).
func TestWrite_EmptyRequestIDOmitsMemberAndHeader(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)

	Write(rec, req, http.StatusNotFound, "", "/problems/not-found", "Not found")

	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Request-ID"); got != "" {
		t.Errorf("X-Request-ID = %q, want absent for an empty request ID", got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body["requestId"]; ok {
		t.Errorf("body carries requestId for an empty request ID: %v", body)
	}
	if body["status"] != float64(http.StatusNotFound) {
		t.Errorf("status = %v, want %d", body["status"], http.StatusNotFound)
	}
}

// TestWrite_RequestIDMirrored pins the non-empty case: the member and the
// header both carry the server-authoritative ID.
func TestWrite_RequestIDMirrored(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)

	Write(rec, req, http.StatusForbidden, "req-7", "/problems/forbidden", "Forbidden")

	if got := rec.Header().Get("X-Request-ID"); got != "req-7" {
		t.Errorf("X-Request-ID = %q, want req-7", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["requestId"] != "req-7" {
		t.Errorf("requestId = %v, want req-7", body["requestId"])
	}
}

// TestWriteWithExtensions_ReservedMembersCannotBeOverwritten pins the
// reserved-member guard against every member it protects, including the
// RFC 9457 detail/instance pair that is deliberately not part of the
// contract.
func TestWriteWithExtensions_ReservedMembersCannotBeOverwritten(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)

	WriteWithExtensions(rec, req, http.StatusUnprocessableEntity, "req-1",
		"/problems/validation", "Validation failed",
		map[string]any{
			"type":       "spoofed",
			"title":      "spoofed",
			"status":     599,
			"requestId":  "spoofed",
			"detail":     "spoofed",
			"instance":   "/spoofed",
			"violations": []map[string]string{{"field": "q", "code": "required"}},
		})

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["type"] != "/problems/validation" {
		t.Errorf("type = %v, want the caller's problem type", body["type"])
	}
	if body["title"] != "Validation failed" {
		t.Errorf("title = %v, want the caller's title", body["title"])
	}
	if body["status"] != float64(http.StatusUnprocessableEntity) {
		t.Errorf("status = %v, want %d", body["status"], http.StatusUnprocessableEntity)
	}
	if body["requestId"] != "req-1" {
		t.Errorf("requestId = %v, want req-1", body["requestId"])
	}
	for _, key := range []string{"detail", "instance"} {
		if _, ok := body[key]; ok {
			t.Errorf("reserved member %q was injected by extensions: %v", key, body)
		}
	}
	if _, ok := body["violations"]; !ok {
		t.Errorf("violations extension missing from %v", body)
	}
}
