package routes

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
)

// setupBlogMux builds a ServeMux with the blog routes and the API fallback
// over a migrated database containing one published post.
func setupBlogMux(t *testing.T) (*http.ServeMux, *sql.DB) {
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

	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'T', '', '', 'https://example.com/t.jpg', 'published', 1000, 500, 500)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	mux := http.NewServeMux()
	Blog(mux, db, testDataDir(t), false, "http://localhost:3000", "https://fans.example", nil)
	APIFallback(mux)
	return mux, db
}

// TestBlog_RegisteredCollectionPath proves the exact-path registration:
// GET /api/v1/blog-posts returns the accepted JSON collection through the
// RequestID middleware (authoritative X-Request-ID header present).
func TestBlog_RegisteredCollectionPath(t *testing.T) {
	t.Parallel()

	mux, _ := setupBlogMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}

	var body struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "p1" {
		t.Errorf("items = %+v, want [p1]", body.Items)
	}
}

// TestBlog_RegisteredDetailPath proves the detail registration: a GET to
// /api/v1/blog-posts/{id} resolves the fixture post through the RequestID
// middleware and answers the JSON detail representation — not the API
// catch-all.
func TestBlog_RegisteredDetailPath(t *testing.T) {
	t.Parallel()

	mux, _ := setupBlogMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts/p1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}

	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ID != "p1" {
		t.Errorf("id = %q, want p1", body.ID)
	}
}

// TestBlog_UnregisteredSubpathsReturnAPIProblem proves the fallback holds
// for paths the blog endpoints do not own: the collection root with a
// trailing slash, an unknown post id, a deeper subpath, and non-GET methods
// all stay on the JSON 404 problem — never the SPA HTML navigation response.
func TestBlog_UnregisteredSubpathsReturnAPIProblem(t *testing.T) {
	t.Parallel()

	mux, _ := setupBlogMux(t)

	for _, path := range []string{
		"/api/v1/blog-posts/",
		"/api/v1/blog-posts/unknown",
		"/api/v1/blog-posts/p1/extra",
		"/api/v1/blog-posts/nonexistent",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q, want application/problem+json", path, ct)
		}
	}

	// A POST to the detail path matches no blog pattern — the catch-all
	// owns it (method-aware ServeMux: the GET-only detail pattern is not a
	// match).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blog-posts/p1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST detail: status = %d, want 404", rec.Code)
	}
}

// TestBlog_AdminChain proves the admin registrations carry the full
// authenticated chain: the mutations answer 401 without a session (with a
// trusted origin), 403 with a session but no CSRF token, and the admin
// reads are session-gated 401s for anonymous visitors.
func TestBlog_AdminChain(t *testing.T) {
	t.Parallel()

	mux, db := setupBlogMux(t)

	jsonBody := `{"title":"T","thumbnailPath":"media/images/ab/abcdef0123456789abcdef0123456789.jpg","status":"draft"}`

	t.Run("unauthenticated create", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/blog-posts", strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("session without CSRF is 403", func(t *testing.T) {
		cookie, _ := setupUploadSession(t, db, "admin")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/blog-posts", strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:3000")
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 for a missing CSRF token", rec.Code)
		}
	})

	t.Run("admin read is session-gated", func(t *testing.T) {
		for _, path := range []string{"/api/v1/admin/blog-posts", "/api/v1/admin/blog-posts/p1"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Origin", "http://localhost:3000")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: status = %d, want 401", path, rec.Code)
			}
		}
	})

	t.Run("unknown admin paths are JSON 404 problems", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/nope", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("Content-Type = %q, want problem+json", ct)
		}
	})
}
