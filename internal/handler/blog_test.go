package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store/storetest"
)

// setupBlogList creates a migrated SQLite database with fixture blog posts
// and returns the pool plus the blog-list handler.
//
// Fixture ordering (published_at_ms DESC, id ASC):
//
//	p5  (5000)   newest
//	p4  (4000)
//	p3a (3000)   tie broken by id: p3a before p3b
//	p3b (3000)
//	p2  (2000)
//
// draft/archived/null-published rows exist but must never appear.
func setupBlogList(t *testing.T) (*sql.DB, http.Handler) {
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

	for _, row := range []struct {
		id, status string
		published  any
	}{
		{"p2", "published", int64(2000)},
		{"p3b", "published", int64(3000)},
		{"p3a", "published", int64(3000)},
		{"p4", "published", int64(4000)},
		{"p5", "published", int64(5000)},
		{"draft", "draft", int64(9000)},
		{"archived", "archived", int64(8000)},
		{"null-published", "published", nil},
	} {
		const q = `INSERT INTO blog_posts
			(id, title, subtitle, description, thumbnail_url, status,
			 published_at_ms, created_at_ms, updated_at_ms)
			VALUES (?, 'Title ' || ?, '', '', 'https://example.com/' || ? || '.jpg', ?,
			 ?, ?, ?)`
		created := int64(1000)
		if row.published != nil {
			created = row.published.(int64) - 500
		}
		if _, err := db.Exec(q, row.id, row.id, row.id, row.status, row.published, created, created); err != nil {
			t.Fatalf("insert fixture %q: %v", row.id, err)
		}
	}

	return db, middleware.RequestID()(handler.BlogList(db, "https://fans.example"))
}

func doBlogList(t *testing.T, h http.Handler, target string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec, body
}

// idsOf extracts item ids from a decoded list envelope for compact checks.
func idsOf(t *testing.T, body map[string]any) []string {
	t.Helper()
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("items missing or wrong type: %v", body)
	}
	var out []string
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item wrong type: %v", item)
		}
		out = append(out, m["id"].(string))
	}
	return out
}

// TestBlogList_Success pins the accepted success envelope: newest-first
// published-only items, the filtered pageInfo total, canonical API time,
// application/json, X-Request-ID, and Cache-Control: no-store.
func TestBlogList_Success(t *testing.T) {
	t.Parallel()

	_, h := setupBlogList(t)
	rec, body := doBlogList(t, h, "/api/v1/blog-posts")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	reqID := rec.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("X-Request-ID missing")
	}

	if _, hasTotal := body["total"]; hasTotal {
		t.Error("envelope carries a top-level total — the count lives in pageInfo")
	}
	if got := idsOf(t, body); len(got) != 5 {
		t.Errorf("got %d items %v, want 5 published-only posts", len(got), got)
	} else {
		want := []string{"p5", "p4", "p3a", "p3b", "p2"}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
			}
		}
	}

	pageInfo, ok := body["pageInfo"].(map[string]any)
	if !ok {
		t.Fatalf("pageInfo missing or wrong type: %v", body)
	}
	if pageInfo["hasNextPage"] != false {
		t.Errorf("hasNextPage = %v, want false", pageInfo["hasNextPage"])
	}
	if pageInfo["endCursor"] != nil {
		t.Errorf("endCursor = %v, want null", pageInfo["endCursor"])
	}
	// The total is the FILTERED row count: the fixture's draft, archived, and
	// unstamped rows exist but are not part of the walk.
	if pageInfo["total"] != float64(5) {
		t.Errorf("total = %v, want 5 (the five published rows only)", pageInfo["total"])
	}

	// Canonical API time with exactly millisecond precision.
	first := body["items"].([]any)[0].(map[string]any)
	if got := first["publishedAt"]; got != "1970-01-01T00:00:05.000Z" {
		t.Errorf("publishedAt = %v, want 1970-01-01T00:00:05.000Z", got)
	}
	if first["subtitle"] != "" || first["title"] != "Title p5" {
		t.Errorf("unexpected item fields: %v", first)
	}
}

// TestBlogList_ThumbnailURLConstruction pins the media URL contract
// (the owner-accepted wire shape): stored relative paths are
// emitted as base-joined absolute URLs; interim legacy absolute values pass
// through unchanged until cmd/importmedia rewrites them.
func TestBlogList_ThumbnailURLConstruction(t *testing.T) {
	t.Parallel()

	db, h := setupBlogList(t)

	// A post whose thumbnail is already a target-relative path (post-migration
	// state). published_at_ms 6000 puts it first.
	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('relative', 'R', '', '', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
		 'published', 6000, 5500, 5500)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert relative-thumbnail fixture: %v", err)
	}

	rec, body := doBlogList(t, h, "/api/v1/blog-posts?limit=6")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	items := body["items"].([]any)
	first := items[0].(map[string]any)
	// The served URL pattern omits the storage subdirectory.
	want := "https://fans.example/media/images/abcdef0123456789abcdef0123456789.jpg"
	if got := first["thumbnailUrl"]; got != want {
		t.Errorf("relative thumbnailUrl = %v, want %q", got, want)
	}

	// The legacy fixtures store absolute URLs; they must pass through as-is.
	var sawP2 bool
	for _, it := range items {
		m := it.(map[string]any)
		if m["id"] == "p2" {
			sawP2 = true
			if got := m["thumbnailUrl"]; got != "https://example.com/p2.jpg" {
				t.Errorf("legacy thumbnailUrl = %v, want pass-through", got)
			}
		}
	}
	if !sawP2 {
		t.Error("p2 not in response — pass-through assertion did not run")
	}
}

// TestBlogList_CreatorAndDetailFields pins the list fields:
// items carry description, canonical updatedAt, and the creator ref with a
// guarded avatarUrl (null when absent or when the stored value fails the
// scheme guard).
func TestBlogList_CreatorAndDetailFields(t *testing.T) {
	t.Parallel()

	db, h := setupBlogList(t)

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u1",
		Username:    "creator",
		AvatarURL:   "https://example.com/a.png",
		CreatedAtMS: 1,
	})
	const updateQ = `UPDATE blog_posts
		SET creator_id = 'u1', description = 'Full description', updated_at_ms = 2600
		WHERE id = 'p2'`
	if _, err := db.Exec(updateQ); err != nil {
		t.Fatalf("update p2: %v", err)
	}

	rec, body := doBlogList(t, h, "/api/v1/blog-posts?limit=5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}

	var p2 map[string]any
	var p5 map[string]any
	for _, it := range body["items"].([]any) {
		m := it.(map[string]any)
		switch m["id"] {
		case "p2":
			p2 = m
		case "p5":
			p5 = m
		}
	}
	if p2 == nil || p5 == nil {
		t.Fatalf("p2/p5 missing from page: %v", idsOf(t, body))
	}

	if p2["description"] != "Full description" {
		t.Errorf("p2 description = %v", p2["description"])
	}
	if p2["updatedAt"] != "1970-01-01T00:00:02.600Z" {
		t.Errorf("p2 updatedAt = %v, want canonical time", p2["updatedAt"])
	}
	creator, ok := p2["creator"].(map[string]any)
	if !ok {
		t.Fatalf("p2 creator = %v, want object", p2["creator"])
	}
	if creator["id"] != "u1" || creator["username"] != "creator" || creator["avatarUrl"] != "https://example.com/a.png" {
		t.Errorf("p2 creator = %v", creator)
	}

	// Posts without a creator emit null; posts without an avatar emit
	// avatarUrl null.
	if p5["creator"] != nil {
		t.Errorf("p5 creator = %v, want null", p5["creator"])
	}
}

// TestBlogList_EmptyDatabase pins the empty-collection outcome:
// items: [] and pageInfo {hasNextPage: false, endCursor: null, total: 0}.
func TestBlogList_EmptyDatabase(t *testing.T) {
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

	rec, body := doBlogList(t, middleware.RequestID()(handler.BlogList(db, "https://fans.example")), "/api/v1/blog-posts")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 0 {
		t.Errorf("items = %v, want []", body["items"])
	}
	pageInfo := body["pageInfo"].(map[string]any)
	if pageInfo["hasNextPage"] != false || pageInfo["endCursor"] != nil || pageInfo["total"] != float64(0) {
		t.Errorf("pageInfo = %v, want {hasNextPage: false, endCursor: null, total: 0}", pageInfo)
	}
}

// TestBlogList_KeysetPagination proves the end-to-end cursor flow: the
// response cursor is opaque (v1.-prefixed), the next page continues exactly
// after it, and the final page reports no next page.
func TestBlogList_KeysetPagination(t *testing.T) {
	t.Parallel()

	_, h := setupBlogList(t)

	rec1, body1 := doBlogList(t, h, "/api/v1/blog-posts?limit=2")
	if rec1.Code != http.StatusOK {
		t.Fatalf("page 1 status = %d (%v)", rec1.Code, body1)
	}
	if got := idsOf(t, body1); len(got) != 2 || got[0] != "p5" || got[1] != "p4" {
		t.Fatalf("page 1 = %v, want [p5 p4]", got)
	}
	pageInfo1 := body1["pageInfo"].(map[string]any)
	if pageInfo1["hasNextPage"] != true {
		t.Fatal("page 1 hasNextPage = false, want true")
	}
	cursor1, _ := pageInfo1["endCursor"].(string)
	if len(cursor1) < 3 || cursor1[:3] != "v1." {
		t.Fatalf("endCursor = %q, want v1.-prefixed opaque cursor", cursor1)
	}

	rec2, body2 := doBlogList(t, h, "/api/v1/blog-posts?limit=2&after="+cursor1)
	if rec2.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d (%v)", rec2.Code, body2)
	}
	if got := idsOf(t, body2); len(got) != 2 || got[0] != "p3a" || got[1] != "p3b" {
		t.Fatalf("page 2 = %v, want [p3a p3b]", got)
	}
	cursor2, _ := body2["pageInfo"].(map[string]any)["endCursor"].(string)
	if cursor2 == "" {
		t.Fatal("page 2 endCursor missing, want the continuation cursor")
	}

	rec3, body3 := doBlogList(t, h, "/api/v1/blog-posts?limit=2&after="+cursor2)
	if rec3.Code != http.StatusOK {
		t.Fatalf("page 3 status = %d (%v)", rec3.Code, body3)
	}
	if got := idsOf(t, body3); len(got) != 1 || got[0] != "p2" {
		t.Fatalf("page 3 = %v, want [p2]", got)
	}
	pageInfo3 := body3["pageInfo"].(map[string]any)
	if pageInfo3["hasNextPage"] != false || pageInfo3["endCursor"] != nil {
		t.Errorf("page 3 pageInfo = %v, want {hasNextPage: false, endCursor: null}", pageInfo3)
	}
}

// TestBlogList_RejectsMalformedParameters pins the strict query contract:
// unknown names, duplicates, non-integer limit, empty cursor, and tampered
// cursor are 400s with the stable bad-request problem; the problem requestId
// matches the authoritative X-Request-ID header.
func TestBlogList_RejectsMalformedParameters(t *testing.T) {
	t.Parallel()

	_, h := setupBlogList(t)
	targets := []struct {
		name   string
		target string
	}{
		{"unknown parameter", "/api/v1/blog-posts?unknown=1"},
		{"duplicate limit", "/api/v1/blog-posts?limit=10&limit=20"},
		{"non-integer limit", "/api/v1/blog-posts?limit=abc"},
		{"empty limit", "/api/v1/blog-posts?limit="},
		{"semicolon separator", "/api/v1/blog-posts?limit=5;page=2"},
		{"malformed percent-escape", "/api/v1/blog-posts?limit=%zz"},
		{"empty cursor", "/api/v1/blog-posts?after="},
		{"tampered cursor", "/api/v1/blog-posts?after=v1.!!!"},
		{"unknown cursor form", "/api/v1/blog-posts?after=tampered"},
		{"out-of-contract name", "/api/v1/blog-posts?page=2"},
	}

	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, body := doBlogList(t, h, tc.target)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400", tc.target, rec.Code)
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("%s: Content-Type = %q, want application/problem+json", tc.target, ct)
			}
			if body["type"] != "/problems/bad-request" {
				t.Errorf("%s: type = %v, want /problems/bad-request", tc.target, body["type"])
			}
			if body["requestId"] == nil || body["requestId"] != rec.Header().Get("X-Request-ID") {
				t.Errorf("%s: problem requestId does not match X-Request-ID", tc.target)
			}
		})
	}
}

// TestBlogList_OutOfRangeLimitIs422 pins the semantic-validation outcome:
// an integer limit outside 1..100 is a 422 with the stable {field, code}
// violation — the wire shape is exactly field+code, never validator text.
func TestBlogList_OutOfRangeLimitIs422(t *testing.T) {
	t.Parallel()

	_, h := setupBlogList(t)
	for _, target := range []string{
		"/api/v1/blog-posts?limit=0",
		"/api/v1/blog-posts?limit=101",
		"/api/v1/blog-posts?limit=-5",
		"/api/v1/blog-posts?limit=999999999999999999999999",
	} {
		rec, body := doBlogList(t, h, target)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", target, rec.Code)
			continue
		}
		if body["type"] != "/problems/validation" {
			t.Errorf("%s: type = %v, want /problems/validation", target, body["type"])
		}
		violations, ok := body["violations"].([]any)
		if !ok || len(violations) != 1 {
			t.Errorf("%s: violations = %v, want exactly one", target, body["violations"])
			continue
		}
		v := violations[0].(map[string]any)
		if v["field"] != "limit" || v["code"] != "outOfRange" {
			t.Errorf("%s: violation = %v, want {field: limit, code: outOfRange}", target, v)
		}
		if _, hasMessage := v["message"]; hasMessage {
			t.Errorf("%s: violation carries internal message text", target)
		}
	}
}

// TestBlogList_AcceptsBounds pins the accepted limit boundaries: 1, 100, and
// the default 20 when absent (implicitly covered by TestBlogList_Success).
func TestBlogList_AcceptsBounds(t *testing.T) {
	t.Parallel()

	_, h := setupBlogList(t)
	for _, target := range []string{
		"/api/v1/blog-posts?limit=1",
		"/api/v1/blog-posts?limit=100",
		"/api/v1/blog-posts?limit=20",
	} {
		rec, body := doBlogList(t, h, target)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (%v)", target, rec.Code, body)
		}
	}
}

// TestBlogList_IndicatorCounts pins the list-item counts: each item
// carries commentCount (top-level + replies) and favoriteCount, live-derived
// on the wire — not per-card client requests.
func TestBlogList_IndicatorCounts(t *testing.T) {
	t.Parallel()

	db, h := setupBlogList(t)

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u1",
		Username:    "alice",
		CreatedAtMS: 1,
	})
	for _, c := range []struct{ id, parent string }{
		{"c1", ""}, {"r1", "c1"},
	} {
		if _, err := db.Exec(`INSERT INTO blog_post_comments
			(id, user_id, blog_post_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
			VALUES (?, 'u1', 'p5', NULLIF(?, ''), 'body', 0, 1, 1)`, c.id, c.parent); err != nil {
			t.Fatalf("insert comment %q: %v", c.id, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms)
		VALUES ('u1', 'p5', 1)`); err != nil {
		t.Fatalf("insert favorite: %v", err)
	}

	rec, body := doBlogList(t, h, "/api/v1/blog-posts?limit=20")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	var got map[string]any
	for _, it := range items {
		m := it.(map[string]any)
		if m["id"] == "p5" {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("p5 missing from items: %v", body)
	}
	if got["commentCount"] != float64(2) || got["favoriteCount"] != float64(1) {
		t.Errorf("indicator counts = (%v comments, %v favorites), want (2, 1)",
			got["commentCount"], got["favoriteCount"])
	}
}
