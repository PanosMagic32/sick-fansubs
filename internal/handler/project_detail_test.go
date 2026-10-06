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

// setupProjectDetail creates a migrated SQLite database with one published
// project (creator + updater + three batch downloads), one draft, and one
// archived project, and returns the pool plus the project-detail handler.
func setupProjectDetail(t *testing.T) (*sql.DB, http.Handler) {
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
		const q = `INSERT INTO projects
			(id, title, description, slug, thumbnail_url, status,
			 creator_id, published_at_ms, created_at_ms, updated_at_ms)
			VALUES (?, 'Title ' || ?, ?, 'slug-' || ?, 'https://example.com/' || ? || '.jpg', ?,
			 CASE WHEN ? = 'p1' THEN 'u1' END, ?, ?, ?)`
		created := int64(1000)
		if row.published != nil {
			created = row.published.(int64) - 500
		}
		if _, err := db.Exec(q, row.id, row.id, row.description, row.id, row.id, row.status,
			row.id, row.published, created, created); err != nil {
			t.Fatalf("insert fixture %q: %v", row.id, err)
		}
	}

	// p1 was later edited: a distinct updater and a later updated instant
	// (CHECK: updated_at_ms >= created_at_ms).
	const updateQ = `UPDATE projects SET updater_id = 'u2', updated_at_ms = 3100 WHERE id = 'p1'`
	if _, err := db.Exec(updateQ); err != nil {
		t.Fatalf("set p1 updater: %v", err)
	}

	for _, d := range []struct {
		id, name        string
		position        int
		magnet, torrent *string
	}{
		{"d1", "Batch A", 0, new("magnet:?xt=urn:btih:aaa"), new("https://example.com/t.torrent")},
		{"d2", "Batch B", 1, new("magnet:?xt=urn:btih:bbb"), nil},
		{"d3", "Batch C", 2, new("not a valid link at all"), new("javascript:alert(1)")},
	} {
		const q = `INSERT INTO project_downloads
			(id, project_id, name, magnet_link, torrent_link, position, created_at_ms)
			VALUES (?, 'p1', ?, ?, ?, ?, 1)`
		if _, err := db.Exec(q, d.id, d.name, d.magnet, d.torrent, d.position); err != nil {
			t.Fatalf("insert download %q: %v", d.id, err)
		}
	}

	return db, middleware.RequestID()(handler.ProjectDetail(db, "https://fans.example"))
}

// doProjectDetail serves one id through the handler directly, setting the
// path value the way the ServeMux would (same pattern as the blog detail
// tests).
func doProjectDetail(t *testing.T, h http.Handler, id, rawQuery string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/x"+rawQuery, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec, body
}

// TestProjectDetail_Success pins the accepted detail envelope: every field
// (slug included, subtitle absent), canonical API time, the creator and
// updater projections (id + username + guarded avatar), downloads with
// names ordered by position, and the standard headers.
func TestProjectDetail_Success(t *testing.T) {
	t.Parallel()

	db, h := setupProjectDetail(t)
	// Indicator-count fixtures — kept here, not in the
	// shared setup: the counts belong to THIS test's assertions alone.
	for _, c := range []struct{ id, userID, parent string }{
		{"c1", "u1", ""}, {"r1", "u2", "c1"},
	} {
		if _, err := db.Exec(`INSERT INTO project_comments
			(id, user_id, project_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'p1', NULLIF(?, ''), 'body', 0, 1, 1)`, c.id, c.userID, c.parent); err != nil {
			t.Fatalf("insert comment %q: %v", c.id, err)
		}
	}
	for _, u := range []string{"u1", "u2"} {
		if _, err := db.Exec(`INSERT INTO project_favorites (user_id, project_id, created_at_ms)
			VALUES (?, 'p1', 1)`, u); err != nil {
			t.Fatalf("insert favorite %q: %v", u, err)
		}
	}

	rec, body := doProjectDetail(t, h, "p1", "")

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
	if body["slug"] != "slug-p1" {
		t.Errorf("slug = %v, want slug-p1", body["slug"])
	}
	if _, hasSubtitle := body["subtitle"]; hasSubtitle {
		t.Error("detail carries a subtitle field — the projects table has none")
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

	// Indicator counts: one top-level comment + one reply,
	// two favorites on p1.
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
	if first["name"] != "Batch A" || first["magnetUrl"] != "magnet:?xt=urn:btih:aaa" ||
		first["torrentUrl"] != "https://example.com/t.torrent" {
		t.Errorf("download[0] = %v", first)
	}
	if _, hasResolution := first["resolution"]; hasResolution {
		t.Error("project download carries a resolution field — that is the blog shape")
	}
	second := downloads[1].(map[string]any)
	if second["name"] != "Batch B" || second["magnetUrl"] != "magnet:?xt=urn:btih:bbb" ||
		second["torrentUrl"] != nil {
		t.Errorf("download[1] = %v, want Batch B with null torrent", second)
	}
}

// TestProjectDetail_EmptyDownloadsAndNullUserRefs pins the explicit-absence
// shapes: a project with no downloads emits [] (never null), and NULL
// creator_id/updater_id emit creator: null and updater: null.
func TestProjectDetail_EmptyDownloadsAndNullUserRefs(t *testing.T) {
	t.Parallel()

	db, h := setupProjectDetail(t)

	const q = `INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('plain', 'P', '', 'slug-plain', 'https://example.com/p.jpg', 'published', 5000, 4000, 4000)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert plain fixture: %v", err)
	}

	rec, body := doProjectDetail(t, h, "plain", "")
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

// TestProjectDetail_ThumbnailURLConstruction pins the same media URL
// contract as the list: stored relative paths are
// emitted as base-joined served URLs.
func TestProjectDetail_ThumbnailURLConstruction(t *testing.T) {
	t.Parallel()

	db, h := setupProjectDetail(t)

	const q = `INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('relative', 'R', '', 'slug-relative', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
		 'published', 6000, 5500, 5500)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert relative fixture: %v", err)
	}

	rec, body := doProjectDetail(t, h, "relative", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	want := "https://fans.example/media/images/abcdef0123456789abcdef0123456789.jpg"
	if got := body["thumbnailUrl"]; got != want {
		t.Errorf("thumbnailUrl = %v, want %q", got, want)
	}
}

// TestProjectDetail_NotFound pins the 404 outcome for unknown ids: the
// stable not-found problem with a requestId matching the authoritative
// header.
func TestProjectDetail_NotFound(t *testing.T) {
	t.Parallel()

	_, h := setupProjectDetail(t)
	rec, body := doProjectDetail(t, h, "unknown", "")

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

// TestProjectDetail_MasksNonPublic pins deliberate masking:
// draft, archived, and published-but-unstamped projects answer the same 404
// as unknown ids — the response must not reveal which ids exist unpublished.
func TestProjectDetail_MasksNonPublic(t *testing.T) {
	t.Parallel()

	_, h := setupProjectDetail(t)
	for _, id := range []string{"draft", "archived", "null-published"} {
		rec, body := doProjectDetail(t, h, id, "")
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

// TestProjectDetail_RejectsQueryParams pins the strict boundary: the detail
// GET accepts no query parameters — unknown names, known list names,
// duplicates, and a malformed raw query all answer 400.
func TestProjectDetail_RejectsQueryParams(t *testing.T) {
	t.Parallel()

	_, h := setupProjectDetail(t)
	for _, rawQuery := range []string{"?limit=1", "?after=pv1.x", "?x=1", "?x=1&x=2", "?%zz"} {
		rec, body := doProjectDetail(t, h, "p1", rawQuery)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", rawQuery, rec.Code)
			continue
		}
		if body["type"] != "/problems/bad-request" {
			t.Errorf("%s: type = %v, want /problems/bad-request", rawQuery, body["type"])
		}
	}
}

// TestProjectDetail_RejectsMalformedIDs pins the ID syntax boundary:
// an id outside the opaque URL-safe
// 1–64 character set is structurally invalid input — a 400, not a 404.
// The invalid-input and masked-absence outcomes stay distinct (the
// blog-detail precedent).
func TestProjectDetail_RejectsMalformedIDs(t *testing.T) {
	t.Parallel()

	_, h := setupProjectDetail(t)
	for _, id := range []string{
		"has space",
		"slash/id",
		"bad%20id",
		"üñíçødé",
		strings.Repeat("a", 65),
		"../etc/passwd",
	} {
		rec, body := doProjectDetail(t, h, id, "")
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
		rec, _ := doProjectDetail(t, h, id, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("id %q: status = %d, want 404 (valid shape, unknown project)", id, rec.Code)
		}
	}
}

// TestProjectDetail_StoreFailureIs500 pins the internal-error outcome: a
// broken store (closed pool) answers the generic 500 problem — never a raw
// driver error in the body.
func TestProjectDetail_StoreFailureIs500(t *testing.T) {
	t.Parallel()

	db, h := setupProjectDetail(t)
	db.Close() // force a store failure on the next query

	rec, body := doProjectDetail(t, h, "p1", "")
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

// TestProjectDetail_MasksMalformedThumbnail pins the thumbnail scheme guard:
// a stored thumbnail that fails the URL guard emits JSON
// null — never another scheme. The stored row is untouched.
func TestProjectDetail_MasksMalformedThumbnail(t *testing.T) {
	t.Parallel()

	db, h := setupProjectDetail(t)

	const q = `INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('smuggled', 'S', '', 'slug-smuggled', 'javascript:alert(1)', 'published', 6000, 5500, 5500)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert malformed-thumbnail fixture: %v", err)
	}

	rec, body := doProjectDetail(t, h, "smuggled", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	if body["thumbnailUrl"] != nil {
		t.Errorf("thumbnailUrl = %v, want null (javascript: scheme)", body["thumbnailUrl"])
	}

	// The stored row keeps the malformed value — masking is wire-only.
	var stored string
	if err := db.QueryRow(`SELECT thumbnail_url FROM projects WHERE id = 'smuggled'`).Scan(&stored); err != nil {
		t.Fatalf("query stored thumbnail: %v", err)
	}
	if stored != "javascript:alert(1)" {
		t.Errorf("stored thumbnail changed to %q — masking must not write back", stored)
	}
}

// TestProjectDetail_MasksMalformedAvatar pins the avatar guard: a stored
// avatar that fails the scheme check emits null, never another scheme.
func TestProjectDetail_MasksMalformedAvatar(t *testing.T) {
	t.Parallel()

	db, h := setupProjectDetail(t)

	if _, err := db.Exec(`UPDATE users SET avatar_url = 'javascript:alert(1)' WHERE id = 'u2'`); err != nil {
		t.Fatalf("set malformed avatar: %v", err)
	}

	rec, body := doProjectDetail(t, h, "p1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	updater := body["updater"].(map[string]any)
	if updater["avatarUrl"] != nil {
		t.Errorf("updater avatarUrl = %v, want null (javascript: scheme)", updater["avatarUrl"])
	}
}

// TestProjectDetail_MasksMalformedLinks pins the read-boundary link guard: a
// stored value that is not a plausible http(s) URL with a host or a magnet
// URI (scheme magnet + query) is emitted as null — it can never smuggle
// another scheme into the JSON (same rationale as mediaURL for thumbnails).
// The stored rows are untouched; only the wire projection masks them.
func TestProjectDetail_MasksMalformedLinks(t *testing.T) {
	t.Parallel()

	db, h := setupProjectDetail(t)

	// The p1 fixture's third download carries legacy garbage — masked.
	rec, body := doProjectDetail(t, h, "p1", "")
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

	// A second project exercises every guard branch.
	const projectQ = `INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('links', 'L', '', 'slug-links', 'https://example.com/l.jpg', 'published', 7000, 6500, 6500)`
	if _, err := db.Exec(projectQ); err != nil {
		t.Fatalf("insert links fixture: %v", err)
	}
	rows := []struct {
		caseName        string
		id, name        string
		magnet, torrent *string
		wantMagnet      any
		wantTorrent     any
	}{
		{
			caseName: "garbage magnet and hostless torrent",
			id:       "l1", name: "Garbage",
			magnet: new("magnet:garbage"), torrent: new("http://"),
		},
		{
			caseName: "empty magnet and javascript torrent",
			id:       "l2", name: "Empty magnet",
			magnet: new("magnet:"), torrent: new("javascript:alert(1)"),
		},
		{
			caseName: "valid links pass through",
			id:       "l3", name: "Valid",
			magnet: new("magnet:?xt=urn:btih:ok"), torrent: new("https://example.com/ok.torrent"),
			wantMagnet: "magnet:?xt=urn:btih:ok", wantTorrent: "https://example.com/ok.torrent",
		},
	}
	for i, row := range rows {
		const dlQ = `INSERT INTO project_downloads
			(id, project_id, name, magnet_link, torrent_link, position, created_at_ms)
			VALUES (?, 'links', ?, ?, ?, ?, 1)`
		if _, err := db.Exec(dlQ, row.id, row.name, row.magnet, row.torrent, i); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	rec, body = doProjectDetail(t, h, "links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("links status = %d, want 200 (%v)", rec.Code, body)
	}
	dls := body["downloads"].([]any)
	if len(dls) != len(rows) {
		t.Fatalf("downloads = %d items, want %d", len(dls), len(rows))
	}
	for i, row := range rows {
		t.Run(row.caseName, func(t *testing.T) {
			got := dls[i].(map[string]any)
			if got["magnetUrl"] != row.wantMagnet || got["torrentUrl"] != row.wantTorrent {
				t.Errorf("download = %v, want magnet=%v torrent=%v", got, row.wantMagnet, row.wantTorrent)
			}
		})
	}
}
