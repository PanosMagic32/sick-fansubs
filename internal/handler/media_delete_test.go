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
	delBaseURL = "https://sickfansubs.com"
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
	mux.HandleFunc("DELETE /api/v1/media/{file}", handler.MediaDelete(dir, db, delBaseURL))
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

// TestMediaDelete_LegacyReferenceFormsConflict pins the delete guard against
// every stored form the read path serves: the canonical path, the flat served
// form, a served form with a wrong subdirectory, same-origin absolute URLs
// (including an explicit default port and another scheme), and a relative
// query. Each protects the canonical file — the sweep and the explicit delete
// resolve the same target.
func TestMediaDelete_LegacyReferenceFormsConflict(t *testing.T) {
	t.Parallel()

	forms := []struct {
		name   string
		stored string
	}{
		{"canonical", delRelPath},
		{"flat", "media/images/" + delID + ".jpg"},
		{"wrong-subdirectory", "media/images/cd/" + delID + ".jpg"},
		{"absolute", delBaseURL + "/media/images/" + delID + ".jpg"},
		{"absolute-default-port", "https://sickfansubs.com:443/media/images/" + delID + ".jpg"},
		{"absolute-http", "http://sickfansubs.com/media/images/" + delID + ".jpg"},
		{"absolute-uppercase-scheme", "HTTPS://sickfansubs.com/media/images/" + delID + ".jpg"},
		{"absolute-trailing-dot", "https://sickfansubs.com./media/images/" + delID + ".jpg"},
		{"absolute-www", "https://www.sickfansubs.com/media/images/" + delID + ".jpg"},
		{"relative-query", "media/images/" + delID + ".jpg?v=2"},
	}
	for _, form := range forms {
		stored := form.stored
		t.Run(form.name, func(t *testing.T) {
			t.Parallel()

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
				VALUES ('b1', 'T', '', '', ?, 'published', 1000, 500, 500)`, stored); err != nil {
				t.Fatalf("seed blog post: %v", err)
			}
			full := filepath.Join(dir, delRelPath)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(full, []byte("bytes"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}

			mux := http.NewServeMux()
			mux.HandleFunc("DELETE /api/v1/media/{file}", handler.MediaDelete(dir, db, delBaseURL))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+delID+".jpg", identity.RoleAdmin, true))

			if rec.Code != http.StatusConflict {
				t.Fatalf("stored %q: status = %d, want 409", stored, rec.Code)
			}
			if _, err := os.Stat(full); err != nil {
				t.Errorf("stored %q: referenced file was removed: %v", stored, err)
			}
		})
	}
}

// TestMediaDelete_LegacyJPEGReferenceConflicts pins the serving parser's
// `.jpeg` form: a hand-placed file is served, so a stored jpeg reference must
// protect it from the explicit delete even though the pipeline never writes
// that extension.
func TestMediaDelete_LegacyJPEGReferenceConflicts(t *testing.T) {
	t.Parallel()

	forms := []struct {
		name   string
		stored string
	}{
		{"flat-jpeg", "media/images/" + delID + ".jpeg"},
		{"absolute-jpeg", delBaseURL + "/media/images/" + delID + ".jpeg"},
	}
	for _, form := range forms {
		stored := form.stored
		t.Run(form.name, func(t *testing.T) {
			t.Parallel()

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
				VALUES ('b1', 'T', '', '', ?, 'published', 1000, 500, 500)`, stored); err != nil {
				t.Fatalf("seed blog post: %v", err)
			}
			full := filepath.Join(dir, media.RelativePath(delID, media.ExtJPEG))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(full, []byte("bytes"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}

			mux := http.NewServeMux()
			mux.HandleFunc("DELETE /api/v1/media/{file}", handler.MediaDelete(dir, db, delBaseURL))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, deleteRequest(http.MethodDelete, "/api/v1/media/"+delID+".jpeg", identity.RoleAdmin, true))

			if rec.Code != http.StatusConflict {
				t.Fatalf("stored %q: status = %d, want 409", stored, rec.Code)
			}
			if _, err := os.Stat(full); err != nil {
				t.Errorf("stored %q: referenced file was removed: %v", stored, err)
			}
		})
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
