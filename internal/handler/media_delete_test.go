package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/middleware"
)

// MediaDelete handler tests. The handler reads the
// session from the request context — tests attach a synthetic SessionUser;
// the chain (origin/CSRF) is covered by the routes suite.

const (
	delID      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // subdir "aa"
	delRelPath = "media/images/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
)

// setupMediaDelete creates a migrated DB (one row referencing delRelPath),
// a data dir holding that file plus an unreferenced one, and a mux with the
// delete handler registered.
func setupMediaDelete(t *testing.T) (*sql.DB, string, http.Handler) {
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
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'T', '', '', ?, 'published', 1000, 500, 500)`, delRelPath); err != nil {
		t.Fatalf("seed blog post: %v", err)
	}

	for _, rel := range []string{
		delRelPath,
		media.RelativePath("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", media.ExtJPG),
	} {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("bytes"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/media/{file}", handler.MediaDelete(dir, db))
	return db, dir, mux
}

func deleteRequest(method, path, role string, withSession bool) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if withSession {
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1",
			UserID:    "u1",
			Username:  "Katakuri",
			Role:      role,
		}))
	}
	return req
}

func TestMediaDelete_Unauthenticated(t *testing.T) {
	t.Parallel()

	_, _, mux := setupMediaDelete(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+delID+".jpg", "", false))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMediaDelete_NonAdminForbidden(t *testing.T) {
	t.Parallel()

	_, _, mux := setupMediaDelete(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+delID+".jpg", identity.RoleUser, true))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestMediaDelete_MalformedSegmentBadRequest(t *testing.T) {
	t.Parallel()

	_, _, mux := setupMediaDelete(t)
	// A literal ".." segment never reaches the handler — the ServeMux
	// cleans and redirects it (documented in the routes tests). The
	// ENCODED form does reach the handler and must be rejected there.
	for _, seg := range []string{"short.jpg", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.jpg", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz.gif", "%2E%2E"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+seg, identity.RoleAdmin, true))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", seg, rec.Code)
		}
	}
}

func TestMediaDelete_MissingFileNotFound(t *testing.T) {
	t.Parallel()

	_, _, mux := setupMediaDelete(t)
	// Well-formed id that has no file on disk.
	missing := "cccccccccccccccccccccccccccccccc.jpg"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+missing, identity.RoleAdmin, true))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMediaDelete_ReferencedFileConflicts(t *testing.T) {
	t.Parallel()

	_, dir, mux := setupMediaDelete(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+delID+".jpg", identity.RoleAdmin, true))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, delRelPath)); err != nil {
		t.Errorf("referenced file was removed: %v", err)
	}
}

func TestMediaDelete_UnreferencedFileRemoved(t *testing.T) {
	t.Parallel()

	_, dir, mux := setupMediaDelete(t)
	const orphan = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	rel := media.RelativePath(orphan, media.ExtJPG)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+orphan+".jpg", identity.RoleAdmin, true))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
		t.Errorf("orphan file still exists: %v", err)
	}
}

func TestMediaDelete_SuperAdminAllowed(t *testing.T) {
	t.Parallel()

	_, _, mux := setupMediaDelete(t)
	const orphan = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+orphan+".jpg", identity.RoleSuperAdmin, true))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}
