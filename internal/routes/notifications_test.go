package routes

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// Notifications registration tests: the two reads and the five writes,
// the read/write chain split, and the JSON 404 fallbacks.

// setupNotificationsMux builds a ServeMux with the notifications routes and
// the API fallback over a migrated database with one moderator ("m1") with
// a live session.
func setupNotificationsMux(t *testing.T) (*http.ServeMux, *sql.DB, string) {
	t.Helper()

	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "m1", Username: "ModOne", Role: "moderator",
		CreatedAtMS: 1_700_000_000_000,
	})
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(100 + i%50)
	}
	digest := sha256.Sum256(raw)
	if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID: "s-mod", UserID: "m1", TokenDigest: digest[:], CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 1_700_000_000_000, ExpiresAtMS: 9_000_000_000_000,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// One visible feed event (a user-target reset — a moderator sees it)
	// so the write endpoints answer 204 instead of the masked 404.
	if _, err := db.Exec(`INSERT INTO audit_events
		(id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms)
		VALUES ('e1', 'password_reset', 'success', 'm1', 't1', 'user', 'r1', 'addr', 1_700_000_000_000)`); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	mux := http.NewServeMux()
	Notifications(mux, db, false, "http://localhost:5173")
	APIFallback(mux)
	return mux, db, base64.RawURLEncoding.EncodeToString(raw)
}

// notifAuthedReq builds a request with the session cookie and (for unsafe
// methods) the trusted origin + CSRF token. The fixture session's CSRF
// token is 32 zero bytes.
func notifAuthedReq(t *testing.T, method, path, cookie string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	}
	return req
}

func TestNotifications_RoutesRegistered(t *testing.T) {
	t.Parallel()

	mux, _, cookie := setupNotificationsMux(t)

	// The two reads answer through the chain (authoritative request ID
	// present) without any CSRF credentials.
	for _, path := range []string{"/api/v1/notifications", "/api/v1/notifications/unread-count"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, notifAuthedReq(t, http.MethodGet, path, cookie))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200 (body %q)", path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Errorf("GET %s: missing X-Request-ID", path)
		}
	}

	// The five writes answer 204 with CSRF + origin present.
	for _, path := range []string{
		"/api/v1/notifications/read-all",
		"/api/v1/notifications/e1/read",
		"/api/v1/notifications/e1",
		"/api/v1/notifications/clear-read",
		"/api/v1/notifications/delete-all",
	} {
		rec := httptest.NewRecorder()
		method := http.MethodPost
		if path == "/api/v1/notifications/e1" {
			method = http.MethodDelete
		}
		mux.ServeHTTP(rec, notifAuthedReq(t, method, path, cookie))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s: status = %d, want 204 (body %q)", method, path, rec.Code, rec.Body.String())
		}
	}
}

func TestNotifications_CSRFRequiredOnWrites(t *testing.T) {
	t.Parallel()

	mux, _, cookie := setupNotificationsMux(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/notifications/read-all"},
		{http.MethodPost, "/api/v1/notifications/e1/read"},
		{http.MethodDelete, "/api/v1/notifications/e1"},
		{http.MethodPost, "/api/v1/notifications/clear-read"},
		{http.MethodPost, "/api/v1/notifications/delete-all"},
	} {
		// Authenticated + origin but NO CSRF token → the CSRF middleware
		// rejects the write with 403.
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s without CSRF: status = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestNotifications_Unauthenticated(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupNotificationsMux(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/notifications"},
		{http.MethodGet, "/api/v1/notifications/unread-count"},
		{http.MethodPost, "/api/v1/notifications/read-all"},
		{http.MethodPost, "/api/v1/notifications/e1/read"},
		{http.MethodDelete, "/api/v1/notifications/e1"},
		{http.MethodPost, "/api/v1/notifications/clear-read"},
		{http.MethodPost, "/api/v1/notifications/delete-all"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.method != http.MethodGet {
			// TrustedOrigin requires a trusted origin on unsafe methods
			// and rejects its absence with 403 BEFORE the session check —
			// the test supplies it so the unauthenticated outcome is the
			// session's 401 (the chain order is pinned separately by the
			// CSRF test).
			req.Header.Set("Origin", "http://localhost:5173")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestNotifications_UnregisteredPathsJSON404(t *testing.T) {
	t.Parallel()

	mux, _, cookie := setupNotificationsMux(t)

	cases := []struct{ method, path string }{
		// Methods no endpoint owns on registered paths.
		{http.MethodPost, "/api/v1/notifications"},
		{http.MethodDelete, "/api/v1/notifications/read-all"},
		{http.MethodGet, "/api/v1/notifications/e1/read"},
		// Paths no endpoint owns.
		{http.MethodGet, "/api/v1/notifications/nope"},
		{http.MethodGet, "/api/v1/notifications/unread-count/extra"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, notifAuthedReq(t, tc.method, tc.path, cookie))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: status = %d, want 404 (body %q)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Fatalf("%s %s: Content-Type = %q, want the JSON problem", tc.method, tc.path, ct)
		}
	}
}
