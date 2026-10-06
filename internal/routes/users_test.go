package routes

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// setupUsersMux builds a ServeMux with the users routes and the API fallback
// over a migrated database containing one active user with a live session.
func setupUsersMux(t *testing.T) (*http.ServeMux, *sql.DB, string, string) {
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
		ID:          "u1",
		Username:    "Katakuri",
		CreatedAtMS: 1_700_000_000_000,
	})

	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	digest := sha256.Sum256(raw)
	err = store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID:          "s1",
		UserID:      "u1",
		TokenDigest: digest[:],
		CSRF:        make([]byte, 32),
		AuthVersion: 1,
		CreatedAtMS: 1_700_000_000_000,
		ExpiresAtMS: 9_000_000_000_000,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	Users(mux, db, false, "http://localhost:5173", "http://localhost:3000", dir, mail.LogLink{}, middleware.NewRateLimiter(1000, time.Hour, time.Minute))
	APIFallback(mux)
	return mux, db, base64.RawURLEncoding.EncodeToString(raw), dir
}

// mustCreateAdminSession adds the admin user "a1" and a live session for it,
// returning the base64 session cookie. The route-registration tests
// share it: the token bytes differ from the fixture user's so the session
// lookup resolves to the admin, not "u1".
func mustCreateAdminSession(t *testing.T, db *sql.DB) string {
	t.Helper()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "a1", Username: "AdminOne", Role: "admin",
		CreatedAtMS: 1_700_000_000_000,
	})
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(200 + i%50)
	}
	digest := sha256.Sum256(raw)
	if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID: "s-admin", UserID: "a1", TokenDigest: digest[:], CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 1_700_000_000_000, ExpiresAtMS: 9_000_000_000_000,
	}); err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// TestUsers_RegisteredProfilePath proves the exact-path registration: a GET
// to /api/v1/users/me with a valid session returns the 200 profile through
// the middleware chain (authoritative X-Request-ID header present).
func TestUsers_RegisteredProfilePath(t *testing.T) {
	t.Parallel()

	mux, _, cookie, _ := setupUsersMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}

	var body struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ID != "u1" || body.Email != "katakuri@example.com" {
		t.Errorf("body = %+v, want u1/katakuri@example.com", body)
	}
}

// TestUsers_ProfileUnauthenticated proves the chain requirement: without a
// session cookie the registered path answers the generic 401 problem (not a
// 404 — the route exists; the session gate rejects).
func TestUsers_ProfileUnauthenticated(t *testing.T) {
	t.Parallel()

	mux, _, _, _ := setupUsersMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}

// TestUsers_UnregisteredSubpaths pins the 404 behavior: paths the users
// routes do not own (the trailing-slash namespace root, deeper subpaths,
// other methods) all fall to the JSON 404 problem — never the SPA HTML
// response.
func TestUsers_UnregisteredSubpaths(t *testing.T) {
	t.Parallel()

	mux, _, cookie, _ := setupUsersMux(t)

	// The fixture session's CSRF token is 32 zero bytes (see
	// setupUsersMux) — unsafe methods must present it to pass the CSRF
	// middleware before the 404 catch-all answers.
	csrfToken := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/users/"},
		{http.MethodGet, "/api/v1/users/me/extra"},
		{http.MethodGet, "/api/v1/users/u1"},
		{http.MethodPost, "/api/v1/users/me"},
		// The bare namespace root answers ONLY the staff-list GET; every
		// other method falls to the exact catch-all.
		{http.MethodPost, "/api/v1/users"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
		// Unsafe methods must carry the trusted Origin AND a valid CSRF
		// token — otherwise TrustedOrigin/CSRF reject them with 403 before
		// routing (deliberate chain order). This test pins the 404 property
		// AFTER the chain, so both credentials are supplied.
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("X-CSRF-Token", csrfToken)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (body %q)", tc.method, tc.path, rec.Code, rec.Body.String())
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s %s: Content-Type = %q, want application/problem+json", tc.method, tc.path, ct)
		}
	}
}

// TestUsers_StaffListFloor proves the bare-path staff list is REGISTERED
// through the chain: a role=user session reaches the handler and is rejected
// at the floor (403), not routed to the 404 catch-all.
func TestUsers_StaffListFloor(t *testing.T) {
	t.Parallel()

	mux, _, cookie, _ := setupUsersMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}
}

// TestUsers_ResetRouteRegistered proves the reset endpoint is REGISTERED
// through the users-subtree chain: an admin session with valid CSRF + origin
// reaches the handler (200), not the 404 catch-all.
func TestUsers_ResetRouteRegistered(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	adminCookie := mustCreateAdminSession(t, db)

	// A moderator target on top of the fixture user.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "m1", Username: "ModOne", Role: "moderator",
		CreatedAtMS: 1_700_000_000_000,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/m1/reset-password", nil)
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: adminCookie})
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Cache-Control"); ct != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store (one-time plaintext)", ct)
	}
}

// TestUsers_RoleDeleteRoutesRegistered proves the role-change and deletion
// routes are REGISTERED through the users-subtree chain (StripPrefix + subtree
// mux): an admin session with valid CSRF + origin reaches both handlers, not
// the 404 catch-all.
func TestUsers_RoleDeleteRoutesRegistered(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	adminCookie := mustCreateAdminSession(t, db)

	// Moderator and user targets on top of the fixture user.
	for _, u := range []storetest.UserSpec{
		{ID: "m1", Username: "ModOne", Role: "moderator",
			CreatedAtMS: 1_700_000_000_000},
		{ID: "u2", Username: "UserTwo",
			CreatedAtMS: 1_700_000_000_000},
	} {
		storetest.InsertUser(t, db, u)
	}

	roleReq := httptest.NewRequest(http.MethodPatch, "/api/v1/users/m1/role", strings.NewReader(`{"role":"user"}`))
	roleReq.AddCookie(&http.Cookie{Name: "sf_session", Value: adminCookie})
	roleReq.Header.Set("Origin", "http://localhost:5173")
	roleReq.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	roleReq.Header.Set("Content-Type", "application/json")
	roleRec := httptest.NewRecorder()
	mux.ServeHTTP(roleRec, roleReq)

	if roleRec.Code != http.StatusOK {
		t.Fatalf("role change status = %d, want 200 (body %q)", roleRec.Code, roleRec.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/users/u2", nil)
	delReq.AddCookie(&http.Cookie{Name: "sf_session", Value: adminCookie})
	delReq.Header.Set("Origin", "http://localhost:5173")
	delReq.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (body %q)", delRec.Code, delRec.Body.String())
	}
}

// TestUsers_SuspendReactivateRoutesRegistered proves the suspend/reactivate
// routes are REGISTERED through the users-subtree chain (StripPrefix + subtree
// mux): an admin session with valid CSRF + origin reaches both handlers, not
// the 404 catch-all.
func TestUsers_SuspendReactivateRoutesRegistered(t *testing.T) {
	t.Parallel()

	mux, db, _, _ := setupUsersMux(t)
	adminCookie := mustCreateAdminSession(t, db)

	// A user target on top of the fixture user.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "u2", Username: "UserTwo",
		CreatedAtMS: 1_700_000_000_000,
	})

	susReq := httptest.NewRequest(http.MethodPost, "/api/v1/users/u2/suspend", nil)
	susReq.AddCookie(&http.Cookie{Name: "sf_session", Value: adminCookie})
	susReq.Header.Set("Origin", "http://localhost:5173")
	susReq.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	susRec := httptest.NewRecorder()
	mux.ServeHTTP(susRec, susReq)

	if susRec.Code != http.StatusOK {
		t.Fatalf("suspend status = %d, want 200 (body %q)", susRec.Code, susRec.Body.String())
	}

	reaReq := httptest.NewRequest(http.MethodPost, "/api/v1/users/u2/reactivate", nil)
	reaReq.AddCookie(&http.Cookie{Name: "sf_session", Value: adminCookie})
	reaReq.Header.Set("Origin", "http://localhost:5173")
	reaReq.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	reaRec := httptest.NewRecorder()
	mux.ServeHTTP(reaRec, reaReq)

	if reaRec.Code != http.StatusOK {
		t.Fatalf("reactivate status = %d, want 200 (body %q)", reaRec.Code, reaRec.Body.String())
	}
}

// TestUsers_ForcePasswordChangeGate proves the forced-change gate sits in
// BOTH authenticated chains: a flagged session is rejected with the
// actionable problem on the users subtree (profile) and on the shared chain
// (staff list), while the auth subtree's bootstrap path stays exempt. The
// avatar PUT carries no CSRF token, so its problem type also proves the gate
// runs before CSRF.
func TestUsers_ForcePasswordChangeGate(t *testing.T) {
	t.Parallel()

	mux, db, cookie, _ := setupUsersMux(t)
	if _, err := db.Exec(`UPDATE users SET must_change_password = 1 WHERE id = 'u1'`); err != nil {
		t.Fatalf("flag fixture user: %v", err)
	}
	// The auth subtree is a separate registration — register it here so the
	// exempt-path assertion has a real endpoint to hit. The mailer + base
	// URL are placeholders (no forgot-password request in this test).
	Auth(mux, db, false, "http://localhost:5173", "http://localhost:5173", mail.LogLink{}, middleware.NewRateLimiter(1000, time.Hour, time.Minute))

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPut, "/api/v1/users/me/avatar"},
		{http.MethodPut, "/api/v1/users/me/email"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
		// The avatar PUT is an unsafe method: the trusted-origin
		// middleware answers before the gate unless the Origin matches.
		if tc.method != http.MethodGet {
			req.Header.Set("Origin", "http://localhost:5173")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status = %d, want 403 (body %q)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		var body struct {
			Type string `json:"type"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("%s %s: decode problem: %v", tc.method, tc.path, err)
		}
		if body.Type != "/problems/auth/password-change-required" {
			t.Errorf("%s %s: problem type = %q, want /problems/auth/password-change-required", tc.method, tc.path, body.Type)
		}
	}

	// The exempt auth paths: the auth subtree has NO gate, so a flagged
	// session can still leave and change its password.
	sessionReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionReq.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	sessionRec := httptest.NewRecorder()
	mux.ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/session must stay exempt: status = %d (body %q)", sessionRec.Code, sessionRec.Body.String())
	}

	// sign-out-all with a live flagged session revokes it (200) — the gate
	// must not intercept the way out.
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	signOutAllReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out-all", nil)
	signOutAllReq.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	signOutAllReq.Header.Set("Origin", "http://localhost:5173")
	signOutAllReq.Header.Set("X-CSRF-Token", csrf)
	signOutAllRec := httptest.NewRecorder()
	mux.ServeHTTP(signOutAllRec, signOutAllReq)
	if signOutAllRec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/auth/sign-out-all must stay exempt: status = %d (body %q)", signOutAllRec.Code, signOutAllRec.Body.String())
	}

	// sign-out afterwards is the idempotent 200 (the session is gone).
	signOutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-out", nil)
	signOutReq.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	signOutReq.Header.Set("Origin", "http://localhost:5173")
	signOutReq.Header.Set("X-CSRF-Token", csrf)
	signOutRec := httptest.NewRecorder()
	mux.ServeHTTP(signOutRec, signOutReq)
	if signOutRec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/auth/sign-out must stay exempt: status = %d (body %q)", signOutRec.Code, signOutRec.Body.String())
	}

	// PUT password after the revocation answers the handler's 401 — NOT the
	// gate's 403 (the gate would answer /problems/auth/password-change-required).
	pwReq := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		strings.NewReader(`{"currentPassword":"x","newPassword":"newpassword456"}`))
	pwReq.Header.Set("Content-Type", "application/json")
	pwReq.Header.Set("Origin", "http://localhost:5173")
	pwReq.Header.Set("X-CSRF-Token", csrf)
	pwRec := httptest.NewRecorder()
	mux.ServeHTTP(pwRec, pwReq)
	if pwRec.Code != http.StatusUnauthorized {
		t.Fatalf("PUT /api/v1/auth/password must stay exempt (handler 401, not the gate's 403): status = %d (body %q)", pwRec.Code, pwRec.Body.String())
	}
}

// TestUsers_SessionRoutesRegistered proves the session-list routes are
// REGISTERED through the users-subtree chain: the GET answers
// 200 with the requesting session marked current, and the DELETE needs the
// chain's CSRF token before it revokes anything.
func TestUsers_SessionRoutesRegistered(t *testing.T) {
	t.Parallel()

	mux, db, cookie, _ := setupUsersMux(t)
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/sessions", nil)
	listReq.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET sessions: status = %d, want 200 (body %q)", listRec.Code, listRec.Body.String())
	}
	var list struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode session list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != "s1" || !list.Items[0].Current {
		t.Fatalf("session list = %+v, want the fixture session marked current", list.Items)
	}

	// Without the chain's CSRF token the DELETE is refused before routing.
	noCSRF := httptest.NewRequest(http.MethodDelete, "/api/v1/users/me/sessions/s1", nil)
	noCSRF.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	noCSRF.Header.Set("Origin", "http://localhost:5173")
	noCSRFRec := httptest.NewRecorder()
	mux.ServeHTTP(noCSRFRec, noCSRF)
	if noCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("DELETE without CSRF: status = %d, want 403", noCSRFRec.Code)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/users/me/sessions/s1", nil)
	delReq.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	delReq.Header.Set("Origin", "http://localhost:5173")
	delReq.Header.Set("X-CSRF-Token", csrf)
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE own session: status = %d, want 204 (body %q)", delRec.Code, delRec.Body.String())
	}

	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = 'u1'`).Scan(&remaining); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 0 {
		t.Errorf("session rows left = %d, want 0 (the revoked row is gone)", remaining)
	}

	// The revoked cookie is dead: the next authenticated request on this path
	// is a 401, not a stale 200.
	afterReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/sessions", nil)
	afterReq.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	afterRec := httptest.NewRecorder()
	mux.ServeHTTP(afterRec, afterReq)
	if afterRec.Code != http.StatusUnauthorized {
		t.Fatalf("GET sessions with the revoked cookie: status = %d, want 401 (body %q)", afterRec.Code, afterRec.Body.String())
	}
}
