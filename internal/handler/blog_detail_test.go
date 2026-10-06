package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// setupBlogDetail creates a migrated SQLite database with one published
// post (creator + two downloads), one draft, and one archived post, and
// returns the pool plus the blog-detail handler.
func setupBlogDetail(t *testing.T) (*sql.DB, http.Handler) {
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

	// Creator has an avatar; the updater does not — the projection must
	// emit both shapes (string and null).
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u1",
		Username:    "creator",
		AvatarURL:   "https://example.com/c.png",
		CreatedAtMS: 1,
	})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u2",
		Username:    "editor",
		CreatedAtMS: 1,
	})

	for _, row := range []struct {
		id, status, description string
		published               any
	}{
		{"p1", "published", "Full description", int64(3000)},
		{"draft", "draft", "hidden", int64(9000)},
		{"archived", "archived", "hidden", int64(8000)},
		{"null-published", "published", "hidden", nil},
	} {
		const q = `INSERT INTO blog_posts
			(id, title, subtitle, description, thumbnail_url, status,
			 creator_id, published_at_ms, created_at_ms, updated_at_ms)
			VALUES (?, 'Title ' || ?, '', ?, 'https://example.com/' || ? || '.jpg', ?,
			 CASE WHEN ? = 'p1' THEN 'u1' END, ?, ?, ?)`
		created := int64(1000)
		if row.published != nil {
			created = row.published.(int64) - 500
		}
		if _, err := db.Exec(q, row.id, row.id, row.description, row.id, row.status,
			row.id, row.published, created, created); err != nil {
			t.Fatalf("insert fixture %q: %v", row.id, err)
		}
	}

	// p1 was later edited: a distinct updater and a later updated instant
	// (CHECK: updated_at_ms >= created_at_ms).
	const updateQ = `UPDATE blog_posts SET updater_id = 'u2', updated_at_ms = 3100 WHERE id = 'p1'`
	if _, err := db.Exec(updateQ); err != nil {
		t.Fatalf("set p1 updater: %v", err)
	}

	for _, d := range []struct {
		id, resolution  string
		position        int
		magnet, torrent *string
	}{
		{"d1", "1080p", 0, new("magnet:?xt=urn:btih:aaa"), new("https://example.com/t.torrent")},
		{"d2", "2160p", 1, new("magnet:?xt=urn:btih:bbb"), nil},
		{"d3", "1080p", 2, new("not a valid link at all"), new("javascript:alert(1)")},
	} {
		const q = `INSERT INTO blog_post_downloads
			(id, blog_post_id, resolution, magnet_link, torrent_link, position, created_at_ms)
			VALUES (?, 'p1', ?, ?, ?, ?, 1)`
		if _, err := db.Exec(q, d.id, d.resolution, d.magnet, d.torrent, d.position); err != nil {
			t.Fatalf("insert download %q: %v", d.id, err)
		}
	}

	return db, middleware.RequestID()(handler.BlogDetail(db, "https://fans.example"))
}

//go:fix inline
func strPtr(s string) *string { return new(s) }

// doBlogDetail serves one id through the handler directly, setting the path
// value the way the ServeMux would (same pattern as the media handler tests).
func doBlogDetail(t *testing.T, h http.Handler, id, rawQuery string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts/x"+rawQuery, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec, body
}

// TestBlogDetail_Success pins the accepted detail envelope: every field,
// canonical API time, the creator projection (id + username only), downloads
// ordered by position, and the standard headers.
func TestBlogDetail_Success(t *testing.T) {
	t.Parallel()

	db, h := setupBlogDetail(t)
	// Indicator-count fixtures — kept here, not in the
	// shared setup: the counts belong to THIS test's assertions alone.
	for _, c := range []struct{ id, userID, parent string }{
		{"c1", "u1", ""}, {"r1", "u2", "c1"},
	} {
		if _, err := db.Exec(`INSERT INTO blog_post_comments
			(id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'p1', NULLIF(?, ''), 'body', 0, 1, 1)`, c.id, c.userID, c.parent); err != nil {
			t.Fatalf("insert comment %q: %v", c.id, err)
		}
	}
	for _, u := range []string{"u1", "u2"} {
		if _, err := db.Exec(`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms)
			VALUES (?, 'p1', 1)`, u); err != nil {
			t.Fatalf("insert favorite %q: %v", u, err)
		}
	}

	rec, body := doBlogDetail(t, h, "p1", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing")
	}

	if body["id"] != "p1" || body["title"] != "Title p1" || body["description"] != "Full description" {
		t.Errorf("core fields wrong: %v", body)
	}
	if body["subtitle"] != "" {
		t.Errorf("subtitle = %v, want empty", body["subtitle"])
	}
	if body["thumbnailUrl"] != "https://example.com/p1.jpg" {
		t.Errorf("thumbnailUrl = %v, want legacy pass-through", body["thumbnailUrl"])
	}
	if body["publishedAt"] != "1970-01-01T00:00:03.000Z" {
		t.Errorf("publishedAt = %v, want canonical millisecond time", body["publishedAt"])
	}
	if body["updatedAt"] != "1970-01-01T00:00:03.100Z" {
		t.Errorf("updatedAt = %v, want the later edit instant", body["updatedAt"])
	}

	// Indicator counts: the fixtures seed one top-level
	// comment + one reply and two favorites on p1.
	if body["commentCount"] != float64(2) || body["favoriteCount"] != float64(2) {
		t.Errorf("indicator counts = (%v comments, %v favorites), want (2, 2)",
			body["commentCount"], body["favoriteCount"])
	}

	creator, ok := body["creator"].(map[string]any)
	if !ok {
		t.Fatalf("creator = %v, want object", body["creator"])
	}
	if creator["id"] != "u1" || creator["username"] != "creator" || creator["avatarUrl"] != "https://example.com/c.png" {
		t.Errorf("creator = %v, want {u1 creator} with avatar", creator)
	}
	if _, hasRole := creator["role"]; hasRole {
		t.Error("creator carries role — public content must not expose roles")
	}

	updater, ok := body["updater"].(map[string]any)
	if !ok {
		t.Fatalf("updater = %v, want object", body["updater"])
	}
	if updater["id"] != "u2" || updater["username"] != "editor" || updater["avatarUrl"] != nil {
		t.Errorf("updater = %v, want {u2 editor} without avatar", updater)
	}
	if _, hasRole := updater["role"]; hasRole {
		t.Error("updater carries role — public content must not expose roles")
	}

	downloads, ok := body["downloads"].([]any)
	if !ok {
		t.Fatalf("downloads = %v, want array", body["downloads"])
	}
	if len(downloads) != 3 {
		t.Fatalf("downloads has %d items, want 3 (masked values still occupy their position)", len(downloads))
	}
	first := downloads[0].(map[string]any)
	if first["resolution"] != "1080p" || first["magnetUrl"] != "magnet:?xt=urn:btih:aaa" ||
		first["torrentUrl"] != "https://example.com/t.torrent" {
		t.Errorf("download[0] = %v", first)
	}
	second := downloads[1].(map[string]any)
	if second["resolution"] != "2160p" || second["magnetUrl"] != "magnet:?xt=urn:btih:bbb" ||
		second["torrentUrl"] != nil {
		t.Errorf("download[1] = %v, want 2160p with null torrent", second)
	}
}

// TestBlogDetail_EmptyDownloadsAndNullUserRefs pins the explicit-absence
// shapes: a post with no downloads emits [] (never null), and NULL
// creator_id/updater_id emit creator: null and updater: null.
func TestBlogDetail_EmptyDownloadsAndNullUserRefs(t *testing.T) {
	t.Parallel()

	db, h := setupBlogDetail(t)

	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('plain', 'P', '', '', 'https://example.com/p.jpg', 'published', 5000, 4000, 4000)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert plain fixture: %v", err)
	}

	rec, body := doBlogDetail(t, h, "plain", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	if body["creator"] != nil {
		t.Errorf("creator = %v, want null", body["creator"])
	}
	if body["updater"] != nil {
		t.Errorf("updater = %v, want null", body["updater"])
	}
	if downloads, ok := body["downloads"].([]any); !ok || len(downloads) != 0 {
		t.Errorf("downloads = %v, want []", body["downloads"])
	}
}

// TestBlogDetail_ThumbnailURLConstruction pins the same media URL contract as
// the list: stored relative paths are emitted as
// base-joined served URLs.
func TestBlogDetail_ThumbnailURLConstruction(t *testing.T) {
	t.Parallel()

	db, h := setupBlogDetail(t)

	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('relative', 'R', '', '', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
		 'published', 6000, 5500, 5500)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert relative fixture: %v", err)
	}

	rec, body := doBlogDetail(t, h, "relative", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	want := "https://fans.example/media/images/abcdef0123456789abcdef0123456789.jpg"
	if got := body["thumbnailUrl"]; got != want {
		t.Errorf("thumbnailUrl = %v, want %q", got, want)
	}
}

// TestBlogDetail_NotFound pins the 404 outcome for unknown ids: the stable
// not-found problem with a requestId matching the authoritative header.
func TestBlogDetail_NotFound(t *testing.T) {
	t.Parallel()

	_, h := setupBlogDetail(t)
	rec, body := doBlogDetail(t, h, "unknown", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%v)", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	if body["type"] != "/problems/not-found" {
		t.Errorf("type = %v, want /problems/not-found", body["type"])
	}
	if body["requestId"] == nil || body["requestId"] != rec.Header().Get("X-Request-ID") {
		t.Errorf("problem requestId %v does not match X-Request-ID %q",
			body["requestId"], rec.Header().Get("X-Request-ID"))
	}
}

// TestBlogDetail_MasksNonPublic pins deliberate masking:
// draft, archived, and published-but-unstamped posts answer the same 404 as
// unknown ids — the response must not reveal which ids exist unpublished.
func TestBlogDetail_MasksNonPublic(t *testing.T) {
	t.Parallel()

	_, h := setupBlogDetail(t)
	for _, id := range []string{"draft", "archived", "null-published"} {
		rec, body := doBlogDetail(t, h, id, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (%v)", id, rec.Code, body)
		}
		if body["type"] != "/problems/not-found" {
			t.Errorf("%s: type = %v, want /problems/not-found", id, body["type"])
		}
		if body["requestId"] == nil || body["requestId"] != rec.Header().Get("X-Request-ID") {
			t.Errorf("%s: problem requestId does not match X-Request-ID", id)
		}
	}
}

// TestBlogDetail_RejectsQueryParams pins the strict boundary: the detail GET
// accepts no query parameters — unknown names, known list names, duplicates,
// and a malformed raw query all answer 400.
func TestBlogDetail_RejectsQueryParams(t *testing.T) {
	t.Parallel()

	_, h := setupBlogDetail(t)
	queries := []struct {
		name     string
		rawQuery string
	}{
		{"unknown parameter", "?limit=1"},
		{"list parameter on a detail read", "?after=v1.x"},
		{"unknown name", "?x=1"},
		{"duplicate name", "?x=1&x=2"},
		{"malformed escape", "?%zz"},
	}
	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, body := doBlogDetail(t, h, "p1", tc.rawQuery)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", tc.rawQuery, rec.Code)
				return
			}
			if body["type"] != "/problems/bad-request" {
				t.Errorf("%s: type = %v, want /problems/bad-request", tc.rawQuery, body["type"])
			}
		})
	}
}

// TestBlogDetail_RejectsMalformedIDs pins the ID syntax boundary:
// an id outside the opaque URL-safe
// 1–64 character set is structurally invalid input — a 400, not a 404.
// The invalid-input and masked-absence outcomes stay distinct.
func TestBlogDetail_RejectsMalformedIDs(t *testing.T) {
	t.Parallel()

	_, h := setupBlogDetail(t)
	for _, id := range []string{
		"has space",
		"slash/id",
		"bad%20id",
		"üñíçødé",
		strings.Repeat("a", 65),
		"../etc/passwd",
	} {
		rec, body := doBlogDetail(t, h, id, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id %q: status = %d, want 400", id, rec.Code)
			continue
		}
		if body["type"] != "/problems/bad-request" {
			t.Errorf("id %q: type = %v, want /problems/bad-request", id, body["type"])
		}
	}

	// The boundary values of the accepted set still reach the lookup.
	for _, id := range []string{"a", strings.Repeat("z", 64), "AZaz09._~-"} {
		rec, _ := doBlogDetail(t, h, id, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("id %q: status = %d, want 404 (valid shape, unknown post)", id, rec.Code)
		}
	}
}

// TestBlogDetail_StoreFailureIs500 pins the internal-error outcome: a broken
// store (closed pool) answers the generic 500 problem — never a raw driver
// error in the body.
func TestBlogDetail_StoreFailureIs500(t *testing.T) {
	t.Parallel()

	db, h := setupBlogDetail(t)
	db.Close() // force a store failure on the next query

	rec, body := doBlogDetail(t, h, "p1", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (%v)", rec.Code, body)
	}
	if body["type"] != "/problems/internal-error" {
		t.Errorf("type = %v, want /problems/internal-error", body["type"])
	}
	if body["requestId"] == nil || body["requestId"] != rec.Header().Get("X-Request-ID") {
		t.Errorf("problem requestId does not match X-Request-ID header")
	}
}

// TestBlogDetail_MasksMalformedAvatar pins the avatar guard: a stored
// avatar that fails the scheme check emits null, never another scheme.
func TestBlogDetail_MasksMalformedAvatar(t *testing.T) {
	t.Parallel()

	db, h := setupBlogDetail(t)

	if _, err := db.Exec(`UPDATE users SET avatar_url = 'javascript:alert(1)' WHERE id = 'u2'`); err != nil {
		t.Fatalf("set malformed avatar: %v", err)
	}

	rec, body := doBlogDetail(t, h, "p1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	updater := body["updater"].(map[string]any)
	if updater["avatarUrl"] != nil {
		t.Errorf("updater avatarUrl = %v, want null (javascript: scheme)", updater["avatarUrl"])
	}
}

// TestBlogDetail_MasksMalformedLinks pins the read-boundary link guard: a
// stored value that is not a plausible http(s) URL with a host or a magnet
// URI (scheme magnet + query) is emitted as null — it can never smuggle
// another scheme into the JSON (same rationale as mediaURL for thumbnails).
// The stored rows are untouched; only the wire projection masks them.
func TestBlogDetail_MasksMalformedLinks(t *testing.T) {
	t.Parallel()

	db, h := setupBlogDetail(t)

	// The p1 fixture's third download carries legacy garbage — masked.
	rec, body := doBlogDetail(t, h, "p1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("p1 status = %d, want 200 (%v)", rec.Code, body)
	}
	third := body["downloads"].([]any)[2].(map[string]any)
	if third["magnetUrl"] != nil {
		t.Errorf("magnetUrl = %v, want null (malformed stored value)", third["magnetUrl"])
	}
	if third["torrentUrl"] != nil {
		t.Errorf("torrentUrl = %v, want null (javascript: scheme)", third["torrentUrl"])
	}

	// A second post exercises every guard branch.
	const postQ = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('links', 'L', '', '', 'https://example.com/l.jpg', 'published', 7000, 6500, 6500)`
	if _, err := db.Exec(postQ); err != nil {
		t.Fatalf("insert links fixture: %v", err)
	}
	rows := []struct {
		name            string
		id, resolution  string
		magnet, torrent *string
		wantMagnet      any
		wantTorrent     any
	}{
		{
			name: "garbage magnet and hostless torrent",
			id:   "l1", resolution: "1080p",
			magnet: new("magnet:garbage"), torrent: new("http://"),
		},
		{
			name: "empty magnet and javascript torrent",
			id:   "l2", resolution: "1080p",
			magnet: new("magnet:"), torrent: new("javascript:alert(1)"),
		},
		{
			name: "valid links pass through",
			id:   "l3", resolution: "1080p",
			magnet: new("magnet:?xt=urn:btih:ok"), torrent: new("https://example.com/ok.torrent"),
			wantMagnet: "magnet:?xt=urn:btih:ok", wantTorrent: "https://example.com/ok.torrent",
		},
	}
	for i, row := range rows {
		const dlQ = `INSERT INTO blog_post_downloads
			(id, blog_post_id, resolution, magnet_link, torrent_link, position, created_at_ms)
			VALUES (?, 'links', ?, ?, ?, ?, 1)`
		if _, err := db.Exec(dlQ, row.id, row.resolution, row.magnet, row.torrent, i); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	rec, body = doBlogDetail(t, h, "links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("links status = %d, want 200 (%v)", rec.Code, body)
	}
	dls := body["downloads"].([]any)
	if len(dls) != len(rows) {
		t.Fatalf("downloads = %d items, want %d", len(dls), len(rows))
	}
	for i, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got := dls[i].(map[string]any)
			if got["magnetUrl"] != row.wantMagnet || got["torrentUrl"] != row.wantTorrent {
				t.Errorf("download = %v, want magnet=%v torrent=%v", got, row.wantMagnet, row.wantTorrent)
			}
		})
	}
}
