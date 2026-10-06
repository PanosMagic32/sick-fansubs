package handler_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// setupUsersHandler creates a fresh migrated SQLite database and the profile
// handler behind the production middleware chain
// (RequestID → TrustedOrigin → Session) at GET /api/v1/users/me. The chain
// is returned as a plain http.Handler — tests call ServeHTTP on it.
func setupUsersHandler(t *testing.T) (*sql.DB, http.Handler) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users/me", handler.CurrentUserProfile(db, "http://localhost:8080"))

	var h http.Handler = mux
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, h
}

// mustCreateProfileUser seeds the profile fixture user through the shared
// store fixture and returns its ID.
func mustCreateProfileUser(t *testing.T, db *sql.DB, id, username, email string, createdAtMS int64) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:            id,
		Username:      username,
		UsernameCanon: username,
		Email:         email,
		CreatedAtMS:   createdAtMS,
	})
}

// mustCreateProfileSession inserts a session row for the user and returns the
// raw token (for the cookie) — the same token/digest shape the real sign-in
// path produces.
func mustCreateProfileSession(t *testing.T, db *sql.DB, sessionID, userID string) string {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	digest := sha256.Sum256(raw)
	err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID:          sessionID,
		UserID:      userID,
		TokenDigest: digest[:],
		CSRF:        make([]byte, 32),
		AuthVersion: 1,
		CreatedAtMS: 1_000_000_000_000,
		ExpiresAtMS: 9_000_000_000_000,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// getProfile issues GET /api/v1/users/me with the given session cookie.
func getProfile(h http.Handler, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestCurrentUserProfile_Authenticated pins the 200 success contract: the
// full profile projection, canonical API time, nullable avatarUrl, and the
// baseline response headers.
func TestCurrentUserProfile_Authenticated(t *testing.T) {
	t.Parallel()

	db, mux := setupUsersHandler(t)
	mustCreateProfileUser(t, db, "u1", "Katakuri", "katakuri@example.com", 1_700_000_000_000)
	cookie := mustCreateProfileSession(t, db, "s1", "u1")

	rec := getProfile(mux, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}

	var body handler.UserProfile
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ID != "u1" || body.Username != "Katakuri" || body.Email != "katakuri@example.com" || body.Role != "user" {
		t.Errorf("body = %+v, want u1/Katakuri/katakuri@example.com/user", body)
	}
	// Canonical UTC RFC 3339 with exactly milliseconds.
	if body.CreatedAt != "2023-11-14T22:13:20.000Z" {
		t.Errorf("createdAt = %q, want canonical millisecond instant", body.CreatedAt)
	}
	if body.AvatarURL != nil {
		t.Errorf("avatarUrl = %v, want null (no avatar set)", *body.AvatarURL)
	}
}

// TestCurrentUserProfile_AvatarURL pins the avatarUrl wire contract:
// the stored STORAGE-form path
// (media/images/<2hex>/<id>.<ext>) projects to the ABSOLUTE served URL
// (base + /media/images/<id>.<ext>) — the 2-hex subdirectory never reaches
// the wire. The old 3-segment pass-through values also project
// (ServedPath leaves them unchanged), so no stored shape can break the
// response.
func TestCurrentUserProfile_AvatarURL(t *testing.T) {
	t.Parallel()

	db, mux := setupUsersHandler(t)
	mustCreateProfileUser(t, db, "u1", "Katakuri", "katakuri@example.com", 1_700_000_000_000)
	// The 4-segment storage form the pipeline writes (media.RelativePath).
	avatar := "media/images/ab/1234567890abcdef1234567890abcd.jpg"
	if _, err := db.Exec(`UPDATE users SET avatar_url = ? WHERE id = 'u1'`, avatar); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	cookie := mustCreateProfileSession(t, db, "s1", "u1")

	rec := getProfile(mux, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	var body handler.UserProfile
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := "http://localhost:8080/media/images/1234567890abcdef1234567890abcd.jpg"
	if body.AvatarURL == nil || *body.AvatarURL != want {
		t.Errorf("avatarUrl = %v, want %q (absolute served URL, no 2-hex subdirectory)", body.AvatarURL, want)
	}
}

// TestCurrentUserProfile_MasksMalformedAvatar pins the scheme guard on the
// profile projection: a stored avatar that fails the mediaURL check emits
// null, never another scheme (the content-ref avatarURLOrNull rule, shared
// with the profile).
func TestCurrentUserProfile_MasksMalformedAvatar(t *testing.T) {
	t.Parallel()

	db, mux := setupUsersHandler(t)
	mustCreateProfileUser(t, db, "u1", "Katakuri", "katakuri@example.com", 1_700_000_000_000)
	if _, err := db.Exec(`UPDATE users SET avatar_url = 'javascript:alert(1)' WHERE id = 'u1'`); err != nil {
		t.Fatalf("set malformed avatar: %v", err)
	}
	cookie := mustCreateProfileSession(t, db, "s1", "u1")

	rec := getProfile(mux, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	var body handler.UserProfile
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.AvatarURL != nil {
		t.Errorf("avatarUrl = %v, want null (javascript: scheme)", *body.AvatarURL)
	}
}

// TestCurrentUserProfile_Unauthenticated pins the 401 outcome: no session
// cookie means the generic invalid-credentials problem, not the profile.
func TestCurrentUserProfile_Unauthenticated(t *testing.T) {
	t.Parallel()

	_, mux := setupUsersHandler(t)
	rec := getProfile(mux, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	var problem struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Type != "/problems/auth/invalid-credentials" {
		t.Errorf("problem type = %q, want /problems/auth/invalid-credentials", problem.Type)
	}
}

// TestCurrentUserProfile_UserRowMissing pins the deletion-race outcome: the
// session middleware authenticated the request, but the user row vanished
// before the profile read — still the generic 401 (no distinguishable
// outcome, no profile data).
func TestCurrentUserProfile_UserRowMissing(t *testing.T) {
	t.Parallel()

	db, mux := setupUsersHandler(t)
	mustCreateProfileUser(t, db, "u1", "Katakuri", "katakuri@example.com", 1_700_000_000_000)
	cookie := mustCreateProfileSession(t, db, "s1", "u1")
	if _, err := db.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	rec := getProfile(mux, cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
}
