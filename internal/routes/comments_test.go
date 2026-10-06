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

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// Comments registration tests: the eighteen paths, the
// optional-session reads chain, the CSRF'd writes, and the JSON 404
// fallbacks.

// setupCommentsMux builds a ServeMux with the comments routes and the API
// fallback over a migrated database: one published post with one comment,
// a user ("u1") with a live session.
func setupCommentsMux(t *testing.T) (*http.ServeMux, *sql.DB, string) {
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
		ID: "u1", Username: "alice",
		CreatedAtMS: 1_700_000_000_000,
	})
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(100 + i%50)
	}
	digest := sha256.Sum256(raw)
	if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID: "s1", UserID: "u1", TokenDigest: digest[:], CSRF: make([]byte, 32),
		AuthVersion: 1, CreatedAtMS: 1_700_000_000_000, ExpiresAtMS: 9_000_000_000_000,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'B1', '', '', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		 VALUES ('c1', 'u1', 'b1', NULL, 'first', 0, 1000, 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	mux := http.NewServeMux()
	Comments(mux, db, false, "http://localhost:5173", "http://localhost:5173", nil)
	APIFallback(mux)
	return mux, db, base64.RawURLEncoding.EncodeToString(raw)
}

// commentsAuthedReq builds a request with the session cookie and (for
// unsafe methods) the trusted origin + CSRF token. The fixture session's
// CSRF token is 32 zero bytes.
func commentsAuthedReq(t *testing.T, method, path, cookie, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("X-CSRF-Token", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	}
	return req
}

func TestComments_RoutesRegistered(t *testing.T) {
	t.Parallel()

	mux, db, cookie := setupCommentsMux(t)

	// The public list answers ANONYMOUS readers (the optional session —
	// never a 401) and the count answers without any credentials. The
	// replies page reads on the same optional-session chain.
	for _, path := range []string{
		"/api/v1/blog-posts/b1/comments",
		"/api/v1/blog-posts/b1/comments/count",
		"/api/v1/blog-posts/b1/comments/c1/replies",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s (anonymous): status = %d, want 200 (body %q)", path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Errorf("GET %s: missing X-Request-ID", path)
		}
	}

	// The writes answer through the full chain.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, commentsAuthedReq(t, http.MethodPost, "/api/v1/blog-posts/b1/comments", cookie, `{"body":"hello"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST create: status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Location"), "?focus=") {
		t.Errorf("create Location: %q", rec.Header().Get("Location"))
	}

	// c1 belongs to the session user — self-hearts are forbidden — so heart a
	// comment by ANOTHER user.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "u2", Username: "bob",
		CreatedAtMS: 1_700_000_000_000,
	})
	if _, err := db.Exec(`INSERT INTO blog_post_comments (id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('c2', 'u2', 'b1', NULL, 'second', 0, 2000, 2000)`); err != nil {
		t.Fatalf("seed second comment: %v", err)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, commentsAuthedReq(t, http.MethodPut, "/api/v1/blog-posts/b1/comments/c2/heart", cookie, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT heart: status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}

	// The list resolves the session when present: hearted reflects it.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, commentsAuthedReq(t, http.MethodGet, "/api/v1/blog-posts/b1/comments", cookie, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("authed list status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			ID      string `json:"id"`
			Hearted bool   `json:"hearted"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode authed list: %v", err)
	}
	hearted := false
	for _, item := range list.Items {
		if item.ID == "c2" {
			hearted = item.Hearted
		}
	}
	if !hearted {
		t.Errorf("authed list = %+v, want c2 hearted", list.Items)
	}
}

func TestComments_CSRFRequiredOnWrites(t *testing.T) {
	t.Parallel()

	mux, _, cookie := setupCommentsMux(t)

	cases := []struct {
		name         string
		method, path string
	}{
		{"create", http.MethodPost, "/api/v1/blog-posts/b1/comments"},
		{"reply", http.MethodPost, "/api/v1/blog-posts/b1/comments/c1/replies"},
		{"edit", http.MethodPatch, "/api/v1/blog-posts/b1/comments/c1"},
		{"delete", http.MethodDelete, "/api/v1/blog-posts/b1/comments/c1"},
		{"heart", http.MethodPut, "/api/v1/blog-posts/b1/comments/c1/heart"},
		{"unheart", http.MethodDelete, "/api/v1/blog-posts/b1/comments/c1/heart"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Authenticated + origin but NO CSRF token → the CSRF middleware
			// rejects the write with 403.
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
			req.Header.Set("Origin", "http://localhost:5173")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s without CSRF: status = %d, want 403", tc.method, tc.path, rec.Code)
			}
		})
	}
}

func TestComments_UnauthenticatedWrites(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupCommentsMux(t)

	for _, tc := range []struct {
		name         string
		method, path string
	}{
		{"create", http.MethodPost, "/api/v1/blog-posts/b1/comments"},
		{"reply", http.MethodPost, "/api/v1/blog-posts/b1/comments/c1/replies"},
		{"edit", http.MethodPatch, "/api/v1/blog-posts/b1/comments/c1"},
		{"delete", http.MethodDelete, "/api/v1/blog-posts/b1/comments/c1"},
		{"heart", http.MethodPut, "/api/v1/blog-posts/b1/comments/c1/heart"},
		{"unheart", http.MethodDelete, "/api/v1/blog-posts/b1/comments/c1/heart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// TrustedOrigin rejects a missing origin on unsafe methods with 403
			// BEFORE the session check — supply it so the outcome is the
			// session's 401 (the origin-first slot is pinned in routes/auth_test.go).
			req.Header.Set("Origin", "http://localhost:5173")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
			}
		})
	}
}

func TestComments_UnregisteredPathsJSON404(t *testing.T) {
	t.Parallel()

	mux, _, cookie := setupCommentsMux(t)

	cases := []struct {
		name         string
		method, path string
	}{
		// Methods no endpoint owns on registered paths.
		{"post to the count", http.MethodPost, "/api/v1/blog-posts/b1/comments/count"},
		{"put to the collection", http.MethodPut, "/api/v1/blog-posts/b1/comments"},
		{"put to one comment", http.MethodPut, "/api/v1/blog-posts/b1/comments/c1"},
		{"get one comment", http.MethodGet, "/api/v1/blog-posts/b1/comments/c1"},
		{"post to heart", http.MethodPost, "/api/v1/blog-posts/b1/comments/c1/heart"},
		// Paths no endpoint owns.
		{"extra segment after a comment", http.MethodGet, "/api/v1/blog-posts/b1/comments/c1/extra"},
		{"extra segment after replies", http.MethodGet, "/api/v1/blog-posts/b1/comments/c1/replies/extra"},
		{"unknown comment", http.MethodGet, "/api/v1/blog-posts/b1/comments/nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, commentsAuthedReq(t, tc.method, tc.path, cookie, ""))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: status = %d, want 404 (body %q)", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("%s %s: Content-Type = %q, want the JSON problem", tc.method, tc.path, ct)
			}
		})
	}
}

func TestComments_ProjectRoutesRegistered(t *testing.T) {
	t.Parallel()

	mux, db, cookie := setupCommentsMux(t)
	if _, err := db.Exec(`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
		published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'P1', '', 'p1', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, commentsAuthedReq(t, http.MethodGet, "/api/v1/projects/p1/comments", cookie, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("project list: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode project comment list: %v", err)
	}
	if list.Items == nil || len(list.Items) != 0 {
		t.Errorf("project comment list = %s, want an empty items array", rec.Body.String())
	}
}
