package handler_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// setupStaffUserListHandler creates a fresh migrated SQLite database and the
// staff-user-list handler behind the production chain
// (RequestID → TrustedOrigin → Session) at GET /api/v1/users.
func setupStaffUserListHandler(t *testing.T) (*sql.DB, http.Handler) {
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
	const publicBase = "https://fans.example"
	mux.HandleFunc("GET /api/v1/users", handler.StaffUserList(db, publicBase))

	var h http.Handler = mux
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, h
}

// mustCreateStaffUser inserts a user row with explicit role/status/creation
// time and returns its ID.
func mustCreateStaffUser(t *testing.T, db *sql.DB, id, username, role, status string, createdAtMS int64) string {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          id,
		Username:    username,
		Email:       username + "@example.com",
		Role:        role,
		Status:      status,
		CreatedAtMS: createdAtMS,
	})
	return id
}

// mustCreateStaffSession inserts a session row for an ACTIVE user and returns
// the raw token (for the cookie). The token is derived from the user ID so
// parallel tests in one database never share a token digest; the fixture
// users must be active — CreateSession rejects suspended accounts,
// which is exactly why suspended fixtures get no session here.
func mustCreateStaffSession(t *testing.T, db *sql.DB, userID string) string {
	t.Helper()
	raw := sha256.Sum256([]byte("token-" + userID))
	digest := sha256.Sum256(raw[:])
	err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID:          "sess-" + userID,
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
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

// getStaffUserList issues GET /api/v1/users with the given query string and
// session cookie.
func getStaffUserList(h http.Handler, cookie, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users"+query, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestStaffUserList_Admin pins the 200 success contract for an admin viewer:
// envelope shape, email present, canonical time, baseline headers, and
// the collection ordering.
func TestStaffUserList_Admin(t *testing.T) {
	t.Parallel()

	db, mux := setupStaffUserListHandler(t)
	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3_000)
	cookie := mustCreateStaffSession(t, db, mustCreateStaffUser(t, db, "a1", "AdminOne", "admin", "active", 4_000))

	rec := getStaffUserList(mux, cookie, "")
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

	var body struct {
		Items    []handler.StaffUserItem `json:"items"`
		PageInfo struct {
			HasNextPage bool    `json:"hasNextPage"`
			EndCursor   *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(body.Items))
	}
	// Newest first: the admin (4000) precedes the user (3000).
	if body.Items[0].ID != "a1" || body.Items[1].ID != "u1" {
		t.Errorf("order = [%s %s], want [a1 u1]", body.Items[0].ID, body.Items[1].ID)
	}
	if body.Items[1].Email == nil || *body.Items[1].Email != "Alpha@example.com" {
		t.Errorf("email = %v, want Alpha@example.com (admin viewer)", body.Items[1].Email)
	}
	if body.Items[1].CreatedAt != "1970-01-01T00:00:03.000Z" {
		t.Errorf("createdAt = %q, want canonical millisecond instant", body.Items[1].CreatedAt)
	}
	if body.PageInfo.HasNextPage || body.PageInfo.EndCursor != nil {
		t.Errorf("pageInfo = %+v, want no next page", body.PageInfo)
	}
}

// TestStaffUserList_ModeratorOmitsEmail pins the projection
// split: a moderator's response has NO email key at all — not null, absent.
func TestStaffUserList_ModeratorOmitsEmail(t *testing.T) {
	t.Parallel()

	db, mux := setupStaffUserListHandler(t)
	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3_000)
	cookie := mustCreateStaffSession(t, db, mustCreateStaffUser(t, db, "m1", "ModOne", "moderator", "active", 4_000))

	rec := getStaffUserList(mux, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"email"`) {
		t.Errorf("moderator response contains an email key: %s", rec.Body.String())
	}
}

// TestStaffUserList_AvatarURL pins the avatarUrl projection on the list
// (through the shared UserProfile projection rule): the stored
// storage-form reference reaches the wire as the ABSOLUTE served URL with
// the 2-hex storage subdirectory dropped, an account
// without an avatar carries an explicit null, and the key is present either
// way — the email gate is the only role-dependent omission on this DTO.
func TestStaffUserList_AvatarURL(t *testing.T) {
	t.Parallel()

	db, mux := setupStaffUserListHandler(t)
	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3_000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "active", 2_000)
	if _, err := db.Exec(`UPDATE users
		SET avatar_url = 'media/images/ab/abcdef0123456789abcdef0123456789.jpg'
		WHERE id = 'u1'`); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	cookie := mustCreateStaffSession(t, db, mustCreateStaffUser(t, db, "a1", "AdminOne", "admin", "active", 4_000))

	rec := getStaffUserList(mux, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	rawBody := rec.Body.Bytes()

	var body struct {
		Items []handler.StaffUserItem `json:"items"`
	}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(body.Items))
	}
	// Newest first: a1 (4000), u1 (3000), u2 (2000).
	want := "https://fans.example/media/images/abcdef0123456789abcdef0123456789.jpg"
	if got := body.Items[1].AvatarURL; got == nil || *got != want {
		t.Errorf("u1 avatarUrl = %v, want %q (absolute served URL, 2-hex subdir dropped)", got, want)
	}
	if got := body.Items[2].AvatarURL; got != nil {
		t.Errorf("u2 avatarUrl = %q, want null (no avatar set)", *got)
	}

	// The key is ALWAYS on the wire — null, never omitted.
	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		t.Fatalf("decode raw body: %v", err)
	}
	for _, item := range raw.Items {
		if _, ok := item["avatarUrl"]; !ok {
			t.Errorf("item %v carries no avatarUrl key — the field is always present", item["id"])
		}
	}
}

// TestStaffUserList_Floors pins the authorization boundary: unauthenticated
// → generic 401; an authenticated user → 403.
func TestStaffUserList_Floors(t *testing.T) {
	t.Parallel()

	db, mux := setupStaffUserListHandler(t)
	userCookie := mustCreateStaffSession(t, db, mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3_000))

	rec := getStaffUserList(mux, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: status = %d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
	var problem struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Type != "/problems/auth/invalid-credentials" {
		t.Errorf("unauthenticated problem type = %q, want invalid-credentials", problem.Type)
	}

	rec = getStaffUserList(mux, userCookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("role=user: status = %d, want 403 (body %q)", rec.Code, rec.Body.String())
	}
}

// TestStaffUserList_Filters pins the role/status filters end to end: the
// strict allowlist, valid filtering, and the 422 invalidValue violations.
func TestStaffUserList_Filters(t *testing.T) {
	t.Parallel()

	db, mux := setupStaffUserListHandler(t)
	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 3_000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "suspended", 2_000)
	cookie := mustCreateStaffSession(t, db, mustCreateStaffUser(t, db, "a1", "AdminOne", "admin", "active", 4_000))

	rec := getStaffUserList(mux, cookie, "?status=suspended")
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []handler.StaffUserItem `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "u2" {
		t.Errorf("suspended page = %+v, want just u2", body.Items)
	}

	for _, tc := range []struct {
		name  string
		query string
		field string
	}{
		{"unknown role", "?role=bogus", "role"},
		{"unknown status", "?status=pending", "status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getStaffUserList(mux, cookie, tc.query)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %q)", rec.Code, rec.Body.String())
			}
			var problem struct {
				Type       string `json:"type"`
				Violations []struct {
					Field string `json:"field"`
					Code  string `json:"code"`
				} `json:"violations"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Type != "/problems/validation" {
				t.Errorf("problem type = %q, want /problems/validation", problem.Type)
			}
			if len(problem.Violations) != 1 || problem.Violations[0].Field != tc.field ||
				problem.Violations[0].Code != "invalidValue" {
				t.Errorf("violations = %+v, want [{%s invalidValue}]", problem.Violations, tc.field)
			}
		})
	}
}

// TestStaffUserList_CursorRoundTrip pins the uv1. cursor: the endCursor pages
// within the filtered set and is null on the final page.
func TestStaffUserList_CursorRoundTrip(t *testing.T) {
	t.Parallel()

	db, mux := setupStaffUserListHandler(t)
	mustCreateStaffUser(t, db, "u1", "Alpha", "user", "active", 4_000)
	mustCreateStaffUser(t, db, "u2", "Beta", "user", "active", 3_000)
	mustCreateStaffUser(t, db, "u3", "Gamma", "user", "active", 2_000)
	cookie := mustCreateStaffSession(t, db, mustCreateStaffUser(t, db, "a1", "AdminOne", "admin", "active", 5_000))

	rec := getStaffUserList(mux, cookie, "?limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 1: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var page struct {
		Items    []handler.StaffUserItem `json:"items"`
		PageInfo struct {
			HasNextPage bool    `json:"hasNextPage"`
			EndCursor   *string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode page 1: %v", err)
	}
	if !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == nil {
		t.Fatalf("page 1 pageInfo = %+v, want hasNext + endCursor", page.PageInfo)
	}
	if !strings.HasPrefix(*page.PageInfo.EndCursor, "uv1.") {
		t.Errorf("endCursor = %q, want uv1. namespace", *page.PageInfo.EndCursor)
	}

	rec = getStaffUserList(mux, cookie, "?limit=2&after="+*page.PageInfo.EndCursor)
	if rec.Code != http.StatusOK {
		t.Fatalf("page 2: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	page.Items = nil
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "u2" || page.Items[1].ID != "u3" {
		t.Errorf("page 2 = %+v, want [u2 u3]", page.Items)
	}
	if page.PageInfo.HasNextPage || page.PageInfo.EndCursor != nil {
		t.Errorf("page 2 pageInfo = %+v, want final page", page.PageInfo)
	}
}

// TestStaffUserList_StrictQuery pins the 400 outcomes: unknown parameter,
// duplicate parameter, non-integer limit, and a foreign-namespace cursor.
func TestStaffUserList_StrictQuery(t *testing.T) {
	t.Parallel()

	db, mux := setupStaffUserListHandler(t)
	cookie := mustCreateStaffSession(t, db, mustCreateStaffUser(t, db, "a1", "AdminOne", "admin", "active", 4_000))

	for _, q := range []string{
		"?bogus=1",
		"?role=user&role=admin",
		"?limit=abc",
		"?after=vv1.invalid", // wrong namespace — v1. is the blog cursor
	} {
		rec := getStaffUserList(mux, cookie, q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %q)", q, rec.Code, rec.Body.String())
		}
	}
}
