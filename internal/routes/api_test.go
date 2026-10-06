package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAPIFallback_UnknownPathReturnsJSONProblem pins the rule
// that unknown /api/v1 paths return a 404 application/problem+json response
// with the /problems/not-found type — never the SPA HTML navigation response.
func TestAPIFallback_UnknownPathReturnsJSONProblem(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	APIFallback(mux)

	for _, path := range []string{"/api/v1", "/api/v1/", "/api/v1/unknown", "/api/v1/auth/nonexistent"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("got %d, want 404", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("got %q, want application/problem+json", ct)
			}
			var body map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["type"] != "/problems/not-found" {
				t.Errorf("got %v, want /problems/not-found", body["type"])
			}
		})
	}
}

// TestAPIFallback_UnknownPathCarriesTheRequestID pins the correlation contract
// the OpenAPI Problem schema requires: an unmatched path answers with the same
// X-Request-ID header and problem requestId a handled route answers with.
func TestAPIFallback_UnknownPathCarriesTheRequestID(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	APIFallback(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if len(id) != 32 {
		t.Fatalf("X-Request-ID = %q, want a 32-character server-generated ID", id)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["requestId"] != id {
		t.Errorf("problem requestId = %v, want the header's %q", body["requestId"], id)
	}
}

// TestAPIFallback_PrecedenceKeepsRegisteredPatterns proves the catch-all never
// shadows a more specific registered pattern (ServeMux longest-prefix match).
func TestAPIFallback_PrecedenceKeepsRegisteredPatterns(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/sign-in", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	APIFallback(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sign-in", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("registered pattern shadowed by fallback: got %d", rec.Code)
	}
}
