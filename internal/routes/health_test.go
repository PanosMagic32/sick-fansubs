package routes

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/database"
)

// openHealthDB opens a migrated database so the readiness checker's gates run
// against a real schema.
func openHealthDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestHealth_NamespaceFallbacksAreJSON404 pins the probe-typo guard: a bare
// or trailing-slash health path must not fall through to the SPA shell.
func TestHealth_NamespaceFallbacksAreJSON404(t *testing.T) {
	t.Parallel()

	db := openHealthDB(t)
	mux := http.NewServeMux()
	Health(mux, db, testDataDir(t))

	for _, path := range []string{"/health", "/health/", "/health/live/", "/health/ready/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q, want problem+json", path, ct)
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Errorf("%s: X-Request-ID missing", path)
		}
	}
}

// TestHealth_CarriesTheRequestID pins the route-level contract the handler
// tests cannot: Health applies the RequestID middleware itself, so a probe
// response carries a server-generated ID and the readiness checker still runs
// behind it.
func TestHealth_CarriesTheRequestID(t *testing.T) {
	t.Parallel()

	db := openHealthDB(t)
	mux := http.NewServeMux()
	Health(mux, db, testDataDir(t))

	for path, want := range map[string]string{
		"/health/live":  "ok",
		"/health/ready": "ready",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (body %s)", path, rec.Code, rec.Body.String())
		}
		if id := rec.Header().Get("X-Request-ID"); len(id) != 32 {
			t.Errorf("%s: X-Request-ID = %q, want a 32-character server-generated ID", path, id)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("%s: decode body: %v", path, err)
		}
		if body["status"] != want {
			t.Errorf("%s: status field = %q, want %q", path, body["status"], want)
		}
	}
}
