package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store/storetest"
)

// The content-update → push seam: the
// blog update handler fans out content_updated events to followers after
// the update transaction commits — for published→published edits only.

const updateThumb = "media/images/ab/abcdef0123456789abcdef0123456789.jpg"

// setupUpdateSeam seeds a moderator, a follower user, a published post the
// follower follows, and the BlogUpdate handler with the recording
// notifier. serve PUTs the update body with an If-Match revision.
func setupUpdateSeam(t *testing.T, notify push.Notifier) (*sql.DB, func(ifMatch, status, title string) int) {
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

	for _, u := range []storetest.UserSpec{
		{ID: "mod1", Username: "Mod", Role: "moderator"},
		{ID: "u2", Username: "Bob"},
	} {
		storetest.InsertUser(t, db, u)
	}
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms, revision)
		 VALUES ('b1', 'Ένα Post', '', '', '` + updateThumb + `', 'published', 2000, 1000, 1000, 1)`,
		`INSERT INTO content_follows (user_id, content_kind, content_id, created_at_ms)
		 VALUES ('u2', 'blog-posts', 'b1', 1500)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// A real processed file so thumbnail references validate.
	mediaDir := filepath.Join(dir, "media", "images", "ab")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir media fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "abcdef0123456789abcdef0123456789.jpg"), []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}

	mux := http.NewServeMux()
	now := func() time.Time { return time.Unix(7, 0) }
	mux.HandleFunc("PUT /api/v1/blog-posts/{id}",
		handler.BlogUpdate(db, dir, "https://fans.example", notify, now))

	serve := func(ifMatch, status, title string) int {
		body := `{"title":"` + title + `","subtitle":"","description":"d","thumbnailPath":"` + updateThumb +
			`","status":"` + status + `","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=urn:btih:abc123"}]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/blog-posts/b1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", ifMatch)
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1", UserID: "mod1", Username: "Mod", Role: "moderator",
		}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	return db, serve
}

// TestBlogUpdateFansOutContentUpdated pins the published→published fan-out:
// one event for the follower (the editor is never a recipient), the push
// payload carries the new title and NO comment id.
func TestBlogUpdateFansOutContentUpdated(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	db, serve := setupUpdateSeam(t, notify)

	if code := serve(`"1"`, "published", "Ένα Post v2"); code != http.StatusOK {
		t.Fatalf("update: status = %d, want 200", code)
	}

	events := notify.snapshot()
	if len(events) != 1 {
		t.Fatalf("update events: got %d, want 1", len(events))
	}
	ev := events[0]
	if ev.RecipientID != "u2" || ev.Kind != "content_updated" || ev.ActorUsername != "Mod" ||
		ev.ContentKind != "blog-posts" || ev.ContentID != "b1" || ev.ContentTitle != "Ένα Post v2" ||
		ev.CommentID != "" {
		t.Errorf("update event: got %+v", ev)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'content_updated'`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 1 {
		t.Errorf("content_updated rows: got %d, want 1", n)
	}
}

// TestBlogUpdateSilentTransitionsEmitNothing pins the gate: an unpublish
// (published → draft) emits no event and no row. (The draft → published
// direction is the new_content broadcast — pinned by the
// new_content_push_test.go family, not silent anymore.)
func TestBlogUpdateSilentTransitionsEmitNothing(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	db, serve := setupUpdateSeam(t, notify)

	if code := serve(`"1"`, "draft", "Ένα Post off"); code != http.StatusOK {
		t.Fatalf("unpublish: status = %d, want 200", code)
	}
	if events := notify.snapshot(); len(events) != 0 {
		t.Errorf("unpublish events: got %d, want 0", len(events))
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 0 {
		t.Errorf("notification rows after unpublish: got %d, want 0", n)
	}
}
