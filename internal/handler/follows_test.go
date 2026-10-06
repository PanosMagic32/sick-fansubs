package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// Follows handler tests: the
// bell-toggle endpoints mirror the favorites contract — session
// + CSRF at the chain level, published-only masking, idempotent toggles.

// setupFollowsHandlers creates a migrated database seeded with one user,
// published + draft content, and a mux with the six follow handlers at
// their production paths.
func setupFollowsHandlers(t *testing.T) (*sql.DB, http.Handler) {
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
		ID:       "u1",
		Username: "Katakuri",
	})
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'B1', '', 'blog one', 'media/images/ab/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('bdraft', 'Draft', '', 'draft', 'media/images/ab/bd.jpg', 'draft', NULL, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p1', 'P1', 'project one', 'p1', 'media/images/ab/p1.jpg', 'published', 2000, 1000, 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed content: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users/me/follows/blog-posts/{id}", handler.FollowsStatus(handler.BlogFollowsKind, db))
	mux.HandleFunc("PUT /api/v1/users/me/follows/blog-posts/{id}", handler.FollowsAdd(handler.BlogFollowsKind, db, nil))
	mux.HandleFunc("DELETE /api/v1/users/me/follows/blog-posts/{id}", handler.FollowsRemove(handler.BlogFollowsKind, db))
	mux.HandleFunc("GET /api/v1/users/me/follows/projects/{id}", handler.FollowsStatus(handler.ProjectFollowsKind, db))
	mux.HandleFunc("PUT /api/v1/users/me/follows/projects/{id}", handler.FollowsAdd(handler.ProjectFollowsKind, db, nil))
	mux.HandleFunc("DELETE /api/v1/users/me/follows/projects/{id}", handler.FollowsRemove(handler.ProjectFollowsKind, db))

	return db, logContext(nil, mux)
}

func followRequest(method, path string, withSession bool) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if withSession {
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1",
			UserID:    "u1",
			Username:  "Katakuri",
			Role:      "user",
		}))
	}
	return req
}

func TestFollows_Unauthenticated(t *testing.T) {
	t.Parallel()

	_, mux := setupFollowsHandlers(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me/follows/blog-posts/b1"},
		{http.MethodPut, "/api/v1/users/me/follows/blog-posts/b1"},
		{http.MethodDelete, "/api/v1/users/me/follows/blog-posts/b1"},
		{http.MethodGet, "/api/v1/users/me/follows/projects/p1"},
	} {
		rec := serve(mux, followRequest(tc.method, tc.path, false))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestFollows_StatusAddRemoveCycle(t *testing.T) {
	t.Parallel()

	db, mux := setupFollowsHandlers(t)

	// Not following yet.
	rec := serve(mux, followRequest(http.MethodGet, "/api/v1/users/me/follows/blog-posts/b1", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: code = %d, want 200", rec.Code)
	}
	var got struct {
		Following bool `json:"following"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if got.Following {
		t.Errorf("following = true, want false before the PUT")
	}

	// Follow.
	rec = serve(mux, followRequest(http.MethodPut, "/api/v1/users/me/follows/blog-posts/b1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add: code = %d, want 204", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_follows`).Scan(&n); err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if n != 1 {
		t.Errorf("follow rows: got %d, want 1", n)
	}

	// Repeat PUT is idempotent.
	rec = serve(mux, followRequest(http.MethodPut, "/api/v1/users/me/follows/blog-posts/b1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeat add: code = %d, want 204", rec.Code)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_follows`).Scan(&n); err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if n != 1 {
		t.Errorf("follow rows after repeat: got %d, want 1 (idempotent)", n)
	}

	// Status flips.
	rec = serve(mux, followRequest(http.MethodGet, "/api/v1/users/me/follows/blog-posts/b1", true))
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !got.Following {
		t.Errorf("following = false, want true after the PUT")
	}

	// Unfollow — 204, repeat idempotent.
	rec = serve(mux, followRequest(http.MethodDelete, "/api/v1/users/me/follows/blog-posts/b1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: code = %d, want 204", rec.Code)
	}
	rec = serve(mux, followRequest(http.MethodDelete, "/api/v1/users/me/follows/blog-posts/b1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeat remove: code = %d, want 204", rec.Code)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_follows`).Scan(&n); err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if n != 0 {
		t.Errorf("follow rows after remove: got %d, want 0", n)
	}
}

func TestFollows_MaskedOutcomes(t *testing.T) {
	t.Parallel()

	_, mux := setupFollowsHandlers(t)

	// PUT on a draft → masked 404 (same for the status read).
	rec := serve(mux, followRequest(http.MethodPut, "/api/v1/users/me/follows/blog-posts/bdraft", true))
	if rec.Code != http.StatusNotFound {
		t.Errorf("follow draft: code = %d, want 404", rec.Code)
	}
	rec = serve(mux, followRequest(http.MethodGet, "/api/v1/users/me/follows/blog-posts/bdraft", true))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status draft: code = %d, want 404", rec.Code)
	}
	// PUT on an unknown id → masked 404.
	rec = serve(mux, followRequest(http.MethodPut, "/api/v1/users/me/follows/projects/missing", true))
	if rec.Code != http.StatusNotFound {
		t.Errorf("follow unknown: code = %d, want 404", rec.Code)
	}
	// Invalid id shape → 400.
	rec = serve(mux, followRequest(http.MethodPut, "/api/v1/users/me/follows/blog-posts/not-an-id!", true))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid id: code = %d, want 400", rec.Code)
	}
	// DELETE stays an idempotent 204 even for unknown content.
	rec = serve(mux, followRequest(http.MethodDelete, "/api/v1/users/me/follows/blog-posts/missing", true))
	if rec.Code != http.StatusNoContent {
		t.Errorf("remove unknown: code = %d, want 204", rec.Code)
	}
}

func TestFollows_InjectedClock(t *testing.T) {
	t.Parallel()

	db, _ := setupFollowsHandlers(t)

	fixed := time.Date(2024, 5, 6, 7, 8, 9, 123_000_000, time.UTC)
	h := handler.FollowsAdd(handler.BlogFollowsKind, db, func() time.Time { return fixed })

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /x/{id}", h)
	rec := serve(mux, followRequest(http.MethodPut, "/x/b1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", rec.Code)
	}
	var createdMS int64
	if err := db.QueryRow(`SELECT created_at_ms FROM content_follows`).Scan(&createdMS); err != nil {
		t.Fatalf("read created_at_ms: %v", err)
	}
	if want := fixed.UnixMilli(); createdMS != want {
		t.Errorf("created_at_ms = %d, want the injected clock's %d", createdMS, want)
	}
}
