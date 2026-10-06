// Package handler_test exercises the handler HTTP boundary end to end:
// request parsing, routing, handler logic, and the response headers and
// body. The test conventions live in docs/patterns/go/testing.md.
package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
)

// TestHealthLive verifies the liveness endpoint returns 200 with the
// correct JSON body and mandatory headers.
func TestHealthLive(t *testing.T) {
	t.Parallel()
	// Build a fresh mux for each test. This is intentional: tests must
	// not share mutable state, and a new mux per test is cheap.
	mux := http.NewServeMux()
	mux.Handle("GET /health/live", middleware.RequestID()(http.HandlerFunc(handler.HealthLive)))

	// httptest.NewRequest creates an inbound HTTP request.
	// nil body since GET requests have no body.
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()

	// ServeHTTP processes the request through the mux synchronously.
	// After this call, rec contains the full response.
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want status 200", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("got %q, want Content-Type application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("got %q, want Cache-Control no-store", cc)
	}

	// X-Request-ID must be present and must be a 32-character hex string.
	// 16 random bytes × 2 hex chars each = 32 characters.
	reqID := rec.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("expected X-Request-ID header to be present")
	}
	if len(reqID) != 32 {
		t.Errorf("got %d chars: %q, want X-Request-ID to be 32 hex chars", len(reqID), reqID)
	}

	// The contract is {"status":"ok"} with no additional fields.
	// We decode into a map to verify the key and value without being
	// coupled to JSON field ordering.
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("got %q, want status ok", body["status"])
	}
}

// TestHealthLiveIgnoresInboundRequestID verifies that the server generates
// its own X-Request-ID and never trusts an inbound header value.
//
// This is a security requirement: a hostile client could send a crafted
// X-Request-ID containing log injection payloads, XSS vectors, or a value
// that collides with another request's ID and breaks log correlation.
func TestHealthLiveIgnoresInboundRequestID(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("GET /health/live", middleware.RequestID()(http.HandlerFunc(handler.HealthLive)))

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	req.Header.Set("X-Request-ID", "malicious")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Request-ID")
	if got == "malicious" {
		t.Error("server must not trust inbound X-Request-ID")
	}
	if got == "" {
		t.Error("expected server-generated X-Request-ID")
	}
}

// TestHealthLiveNoCORS verifies that health endpoints do not emit CORS headers.
// The target architecture is same-origin (no cross-origin requests in production).
// CORS middleware will be added later when it's needed.
func TestHealthLiveNoCORS(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("GET /health/live", middleware.RequestID()(http.HandlerFunc(handler.HealthLive)))

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Access-Control-Allow-Origin is the canonical CORS header.
	// If it's set, CORS is active. We expect it to be absent.
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no CORS headers on health endpoint")
	}
}

// TestHealthLiveMethodNotAllowed verifies that Go's method-aware ServeMux
// (Go 1.22+) automatically returns 405 for wrong methods. We register a
// GET handler and send POST — the mux handles the rejection, not our code.
func TestHealthLiveMethodNotAllowed(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("GET /health/live", middleware.RequestID()(http.HandlerFunc(handler.HealthLive)))

	req := httptest.NewRequest(http.MethodPost, "/health/live", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405 Method Not Allowed", rec.Code)
	}
}

// readyChecker is a test fake that always reports "ready". It implements
// HealthChecker and lives in the test file — the production package has no
// no-op checker because real wiring always injects the database checker.
type readyChecker struct{}

func (readyChecker) Check(ctx context.Context) error { return nil }

// TestHealthReadyOK verifies the readiness endpoint returns 200 when the
// HealthChecker reports no error.
func TestHealthReadyOK(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	// HealthReady returns a handler that captures the checker.
	// This is the closure pattern — the checker is injected at wiring time.
	mux.Handle("GET /health/ready", middleware.RequestID()(handler.HealthReady(readyChecker{})))

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want status 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("got %q, want Content-Type application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("got %q, want Cache-Control no-store", cc)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header")
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("got %q, want status ready", body["status"])
	}
}

// failingChecker is a test fake that always returns an error.
// It implements HealthChecker so we can inject it into the readiness handler
// and verify the 503 / "not_ready" path. No mocking library needed.
type failingChecker struct{}

func (failingChecker) Check(ctx context.Context) error {
	return errors.New("database unavailable")
}

// TestHealthReadyNotReady verifies that the readiness endpoint returns 503
// with {"status":"not_ready"} when the HealthChecker reports an error.
func TestHealthReadyNotReady(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("GET /health/ready", middleware.RequestID()(handler.HealthReady(failingChecker{})))

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want status 503", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("got %q, want Cache-Control no-store", cc)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	if body["status"] != "not_ready" {
		t.Errorf("got %q, want status not_ready", body["status"])
	}
}

// TestNotFound verifies that paths without a registered handler return 404.
// Go's ServeMux returns 404 for exact-match patterns when no pattern matches
// (unlike the pre-1.22 behavior which could redirect /path to /path/).
func TestNotFound(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("GET /health/live", middleware.RequestID()(http.HandlerFunc(handler.HealthLive)))

	req := httptest.NewRequest(http.MethodGet, "/health/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 for unknown path", rec.Code)
	}
}

// TestHealthLiveRequestIDIsUnique verifies that request IDs are unique across
// multiple requests. With 16 random bytes (2^128 possible values), collisions
// are astronomically unlikely. This test catches deterministic ID generation
// (e.g., using a counter or timestamp instead of crypto/rand).
func TestHealthLiveRequestIDIsUnique(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.Handle("GET /health/live", middleware.RequestID()(http.HandlerFunc(handler.HealthLive)))

	ids := make(map[string]bool)
	for range 100 {
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		id := rec.Header().Get("X-Request-ID")
		if ids[id] {
			t.Fatalf("duplicate X-Request-ID generated: %q", id)
		}
		ids[id] = true
	}
}
