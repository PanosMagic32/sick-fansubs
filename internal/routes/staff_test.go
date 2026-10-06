package routes

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// Staff namespace registration tests: the metrics and
// logs paths on the authenticated chain, their floors, and the JSON 404
// fallbacks for everything else in /api/v1/staff.

// setupStaffMux builds a mux carrying only the staff namespace, optionally
// with the /api/v1 fallback (the production shape). The fallback-free variant
// exists so the namespace fallbacks can be exercised on their own — with
// APIFallback registered they would be shadowed and the test could not fail.
//
// The log directory deliberately does not exist: a missing sink is a valid
// state (a fresh install), and it keeps this test about ROUTING rather than
// about the sink.
func setupStaffMux(t *testing.T, withAPIFallback bool) (*http.ServeMux, string) {
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

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "m1", Username: "mod", Role: "moderator",
		CreatedAtMS: 1_700_000_000_000,
	})
	raw := make([]byte, 32)
	digest := sha256.Sum256(raw)
	if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID: "s1", UserID: "m1", TokenDigest: digest[:], CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 1_700_000_000_000, ExpiresAtMS: 9_000_000_000_000,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	Staff(mux, db, false, "http://localhost:5173", filepath.Join(testDataDir(t), "logs"))
	if withAPIFallback {
		APIFallback(mux)
	}
	return mux, base64.RawURLEncoding.EncodeToString(raw)
}

func TestStaff_RoutesRegistered(t *testing.T) {
	t.Parallel()
	mux, cookie := setupStaffMux(t, true)

	// Anonymous → 401 (the session gate runs; the handler never does).
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/staff/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET: status = %d, want 401", rec.Code)
	}

	// Moderator with a session → 200 on the same chain.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/metrics", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("moderator GET: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	// The logs and audit routes share the chain but not the floor:
	// anonymous → 401, and the SAME moderator session that just read the metrics
	// → 403 on both.
	for _, path := range []string{"/api/v1/staff/logs", "/api/v1/staff/audit-events"} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s: status = %d, want 401", path, rec.Code)
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("moderator GET %s: status = %d, want 403 (super-admin floor)", path, rec.Code)
		}
	}

	// Wrong method and unknown paths answer the JSON 404, not the method
	// mux's text/plain 405 and not the SPA fallback. The namespace
	// fallbacks in staff.go are what make the wrong-METHOD case a 404
	// instead of a 405: the method-less "/api/v1/staff/" pattern matches the
	// metrics path for every other method (the users precedent).
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/staff/metrics"},
		{http.MethodPost, "/api/v1/staff/logs"},
		{http.MethodPost, "/api/v1/staff/audit-events"},
		{http.MethodGet, "/api/v1/staff"},
		{http.MethodGet, "/api/v1/staff/unknown"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", tc.method, tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("%s %s: Content-Type = %q, want a problem", tc.method, tc.path, ct)
		}
	}
}

// TestStaff_NamespaceFallbacksAreLoadBearing pins the two fallback lines in
// staff.go on a mux WITHOUT APIFallback — the only configuration in which
// they are the answer. Drop either line and this test fails with the method
// mux's 405 (or a 404 with the wrong content type).
func TestStaff_NamespaceFallbacksAreLoadBearing(t *testing.T) {
	t.Parallel()
	mux, _ := setupStaffMux(t, false)

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/staff/metrics"},
		{http.MethodDelete, "/api/v1/staff/metrics"},
		{http.MethodPost, "/api/v1/staff/logs"},
		{http.MethodDelete, "/api/v1/staff/logs"},
		{http.MethodPost, "/api/v1/staff/audit-events"},
		{http.MethodDelete, "/api/v1/staff/audit-events"},
		{http.MethodGet, "/api/v1/staff"},
		{http.MethodPost, "/api/v1/staff"},
		{http.MethodGet, "/api/v1/staff/unknown"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want the JSON 404", tc.method, tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("%s %s: Content-Type = %q, want a problem", tc.method, tc.path, ct)
		}
	}
}
