package routes

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/database"
)

// setupSearchMux builds a ServeMux with the search route and the API
// fallback over a migrated database containing one indexed published post.
func setupSearchMux(t *testing.T) (*http.ServeMux, *sql.DB) {
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

	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'Searchable Title', '', '', 'https://example.com/t.jpg', 'published', 1000, 500, 500)`); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
	var rowid int64
	if err := db.QueryRow(`SELECT rowid FROM blog_posts WHERE id = 'p1'`).Scan(&rowid); err != nil {
		t.Fatalf("rowid: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_post_search(rowid, text) VALUES (?, 'searchable title')`, rowid); err != nil {
		t.Fatalf("index fixture: %v", err)
	}

	mux := http.NewServeMux()
	Search(mux, db, "https://fans.example")
	APIFallback(mux)
	return mux, db
}

// TestSearch_RegisteredPath proves the exact-path registration: GET
// /api/v1/search returns the accepted JSON collection through the
// RequestID middleware (authoritative X-Request-ID header present).
func TestSearch_RegisteredPath(t *testing.T) {
	t.Parallel()

	mux, _ := setupSearchMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=searchable", nil)
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
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "p1" || body.Items[0].Type != "post" {
		t.Errorf("items = %+v, want [post p1]", body.Items)
	}
}

// TestSearch_ValidationBoundary proves the 422 boundary fires through the
// registered chain (a missing q is a client error, not a 500).
func TestSearch_ValidationBoundary(t *testing.T) {
	t.Parallel()

	mux, _ := setupSearchMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}

// TestSearch_UnregisteredPathsReturnAPIProblem proves the fallback holds:
// a POST to the search path matches no method pattern and the trailing
// slash stays on the JSON 404 problem — never the SPA HTML response.
func TestSearch_UnregisteredPathsReturnAPIProblem(t *testing.T) {
	t.Parallel()

	mux, _ := setupSearchMux(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/search"},
		{http.MethodGet, "/api/v1/search/"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", tc.method, tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s %s: Content-Type = %q, want application/problem+json", tc.method, tc.path, ct)
		}
	}
}
