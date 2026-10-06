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

// The publish → push seam: the create and
// update handlers fan out new_content broadcasts after the emitting
// transaction commits — for create-as-published and the FIRST transition
// into published only.

// setupNewContentSeam seeds the recipient matrix (adm1 = the admin actor,
// mod2 = moderator default-on, uon = user with an explicit ON row, uoff =
// user default-off), a real media file, and the four content-write
// handlers with the recording notifier.
func setupNewContentSeam(t *testing.T, notify push.Notifier) (*sql.DB, func(method, path, body, ifMatch string) int) {
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
		{ID: "adm1", Username: "Adm", Role: "admin"},
		{ID: "mod2", Username: "Mod2", Role: "moderator"},
		{ID: "uon", Username: "Uon"},
		{ID: "uoff", Username: "Uoff"},
	} {
		storetest.InsertUser(t, db, u)
	}
	if _, err := db.Exec(`INSERT INTO notification_preferences (user_id, kind, push_enabled, updated_at_ms)
		 VALUES ('uon', 'new_content', 1, 1500)`); err != nil {
		t.Fatalf("seed preference: %v", err)
	}

	mediaDir := filepath.Join(dir, "media", "images", "ab")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir media fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "abcdef0123456789abcdef0123456789.jpg"), []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}

	mux := http.NewServeMux()
	now := func() time.Time { return time.Unix(7, 0) }
	mux.HandleFunc("POST /api/v1/blog-posts",
		handler.BlogCreate(db, dir, "https://fans.example", notify, now))
	mux.HandleFunc("PUT /api/v1/blog-posts/{id}",
		handler.BlogUpdate(db, dir, "https://fans.example", notify, now))
	mux.HandleFunc("POST /api/v1/projects",
		handler.ProjectCreate(db, dir, "https://fans.example", notify, now))
	mux.HandleFunc("PUT /api/v1/projects/{id}",
		handler.ProjectUpdate(db, dir, "https://fans.example", notify, now))

	// serve issues one request as the admin actor; ifMatch carries the
	// revision-precondition header value the update handlers require (""
	// sends no header).
	serve := func(method, path, body, ifMatch string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1", UserID: "adm1", Username: "Adm", Role: "admin",
		}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code >= 400 {
			t.Logf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec.Code
	}
	return db, serve
}

func newContentBlogBody(status, title string) string {
	return `{"title":"` + title + `","subtitle":"","description":"d","thumbnailPath":"` + updateThumb +
		`","status":"` + status + `","downloads":[{"resolution":"1080p","magnetUrl":"magnet:?xt=urn:btih:abc123"}]}`
}

func newContentProjectBody(status, title string) string {
	return `{"title":"` + title + `","description":"d","thumbnailPath":"` + updateThumb +
		`","status":"` + status + `","downloads":[{"name":"Batch","magnetUrl":"magnet:?xt=urn:btih:abc123"}]}`
}

// assertNewContentSeamEvents pins one batch of new_content seam events:
// exactly the two opted-in recipients (mod2 default-on + uon explicit-on;
// the actor adm1 and the default-off uoff receive nothing).
func assertNewContentSeamEvents(t *testing.T, events []push.Event, contentKind string) {
	t.Helper()
	if len(events) != 2 {
		t.Fatalf("events: got %d, want 2", len(events))
	}
	recipients := map[string]bool{}
	for _, ev := range events {
		recipients[ev.RecipientID] = true
		if ev.Kind != "new_content" || ev.ActorUsername != "Adm" ||
			ev.ContentKind != contentKind || ev.ContentID == "" || ev.CommentID != "" {
			t.Errorf("event shape: got %+v", ev)
		}
	}
	if !recipients["mod2"] || !recipients["uon"] || recipients["adm1"] || recipients["uoff"] {
		t.Errorf("recipients: got %v, want exactly mod2 + uon", recipients)
	}
}

// TestBlogCreateFansOutNewContent pins the create-as-published broadcast
// and the committed rows.
func TestBlogCreateFansOutNewContent(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	db, serve := setupNewContentSeam(t, notify)

	if code := serve("POST", "/api/v1/blog-posts", newContentBlogBody("published", "Νέο Post"), ""); code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201", code)
	}
	assertNewContentSeamEvents(t, notify.snapshot(), "blog-posts")

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'new_content'`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 2 {
		t.Errorf("new_content rows: got %d, want 2", n)
	}
}

// TestBlogCreateDraftSilent pins the draft-create leg of the seam: the
// create succeeds, zero events.
func TestBlogCreateDraftSilent(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	_, serve := setupNewContentSeam(t, notify)

	if code := serve("POST", "/api/v1/blog-posts", newContentBlogBody("draft", "Draft"), ""); code != http.StatusCreated {
		t.Fatalf("create draft: status = %d, want 201", code)
	}
	if events := notify.snapshot(); len(events) != 0 {
		t.Errorf("draft create events: got %d, want 0", len(events))
	}
}

// TestProjectCreateFansOutNewContent pins the project leg of the same
// seam (its own content_kind).
func TestProjectCreateFansOutNewContent(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	_, serve := setupNewContentSeam(t, notify)

	if code := serve("POST", "/api/v1/projects", newContentProjectBody("published", "Νέο Project"), ""); code != http.StatusCreated {
		t.Fatalf("create project: status = %d, want 201", code)
	}
	assertNewContentSeamEvents(t, notify.snapshot(), "projects")
}

// TestBlogUpdatePublishFansOutNewContent pins the transition leg: a draft →
// published PUT broadcasts once; a subsequent unpublish → re-publish
// emits nothing (the first-publish stamp is the gate).
func TestBlogUpdatePublishFansOutNewContent(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	db, serve := setupNewContentSeam(t, notify)

	if _, err := db.Exec(`
		INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			created_at_ms, updated_at_ms, revision)
		VALUES ('d1', 'Draft', '', 'd', '` + updateThumb + `', 'draft', 1000, 1000, 1)`); err != nil {
		t.Fatalf("seed draft: %v", err)
	}

	// 1. draft → published: broadcast (revision 1 → 2).
	if code := serve("PUT", "/api/v1/blog-posts/d1", newContentBlogBody("published", "Live"), `"1"`); code != http.StatusOK {
		t.Fatalf("publish: status = %d, want 200", code)
	}
	assertNewContentSeamEvents(t, notify.snapshot(), "blog-posts")

	// 2. unpublish with a STALE If-Match: 412, zero events (a rejected
	// write emits nothing).
	notify.clear()
	if code := serve("PUT", "/api/v1/blog-posts/d1", newContentBlogBody("draft", "Draft"), `"1"`); code != http.StatusPreconditionFailed {
		t.Fatalf("unpublish with stale If-Match: status = %d, want 412", code)
	}
	if events := notify.snapshot(); len(events) != 0 {
		t.Errorf("stale unpublish events: got %d, want 0", len(events))
	}

	// 3. unpublish with the current revision: silent (revision 2 → 3).
	if code := serve("PUT", "/api/v1/blog-posts/d1", newContentBlogBody("draft", "Draft"), `"2"`); code != http.StatusOK {
		t.Fatalf("unpublish: status = %d, want 200", code)
	}
	if events := notify.snapshot(); len(events) != 0 {
		t.Errorf("unpublish events: got %d, want 0", len(events))
	}

	// 4. draft → published again: silent — the stamp already exists, the
	// content is not new (revision 3 → 4).
	if code := serve("PUT", "/api/v1/blog-posts/d1", newContentBlogBody("published", "Live 2"), `"3"`); code != http.StatusOK {
		t.Fatalf("re-publish: status = %d, want 200", code)
	}
	if events := notify.snapshot(); len(events) != 0 {
		t.Errorf("re-publish events: got %d, want 0", len(events))
	}

	// Exactly ONE batch of rows landed (step 1 only).
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE kind = 'new_content'`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 2 {
		t.Errorf("new_content rows: got %d, want 2", n)
	}
}

// TestProjectUpdatePublishFansOutNewContent pins the project leg of the
// transition seam.
func TestProjectUpdatePublishFansOutNewContent(t *testing.T) {
	t.Parallel()
	notify := &recordingNotifier{}
	db, serve := setupNewContentSeam(t, notify)

	if _, err := db.Exec(`
		INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			created_at_ms, updated_at_ms, revision)
		VALUES ('d1', 'Draft', 'd', 'd1', '` + updateThumb + `', 'draft', 1000, 1000, 1)`); err != nil {
		t.Fatalf("seed draft project: %v", err)
	}

	if code := serve("PUT", "/api/v1/projects/d1", newContentProjectBody("published", "Live"), `"1"`); code != http.StatusOK {
		t.Fatalf("publish project: status = %d, want 200", code)
	}
	assertNewContentSeamEvents(t, notify.snapshot(), "projects")
}
