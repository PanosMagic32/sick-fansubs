package handler_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// setupSearch creates a migrated SQLite database with fixture content and
// its search index, and returns the pool plus the search handler.
//
// Fixture (merged ordering published_at_ms DESC, id ASC):
//
//	post:b1 (5000) "Καλημέρα κόσμε"   newest
//	project:p1 (4000) "Έργο Ταξίδι"
//	post:b2 (3000) "Κοινή Λέξη"
//	post:b3 (2000) "Κοινή Λέξη"
//
// Drafts and archived rows exist but must never surface.
func setupSearch(t *testing.T) (*sql.DB, http.Handler) {
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

	insertBlog := func(id, title string, published any) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO blog_posts
			(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
			VALUES (?, ?, '', '', 'https://example.com/' || ? || '.jpg', 'published', ?, 1000, 1000)`,
			id, title, id, published); err != nil {
			t.Fatalf("insert blog %q: %v", id, err)
		}
	}
	insertProject := func(id, title string, published any) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO projects
			(id, title, description, slug, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
			VALUES (?, ?, '', 'slug-' || ?, 'https://example.com/' || ? || '.jpg', 'published', ?, 1000, 1000)`,
			id, title, id, id, published); err != nil {
			t.Fatalf("insert project %q: %v", id, err)
		}
	}
	indexRow := func(f func(context.Context, *sql.Tx) error) {
		t.Helper()
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := f(t.Context(), tx); err != nil {
			tx.Rollback()
			t.Fatalf("index: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	insertBlog("b1", "Καλημέρα κόσμε", int64(5000))
	insertProject("p1", "Έργο Ταξίδι", int64(4000))
	insertBlog("b2", "Κοινή Λέξη", int64(3000))
	insertBlog("b3", "Κοινή Λέξη", int64(2000))
	insertBlog("b-draft", "Κρυφό", nil)
	insertProject("p-draft", "Κρυφό Έργο", nil)
	insertBlog("b-archived", "Αρχείο", int64(6000))
	// Archived AFTER insert: published at insert time (indexed below), then
	// hidden by status — the strongest form of the visibility test.
	if _, err := db.Exec(`UPDATE blog_posts SET status = 'archived' WHERE id = 'b-archived'`); err != nil {
		t.Fatalf("archive row: %v", err)
	}

	// Index every published row with normalized text — the import contract.
	// The archived row is deliberately indexed too: the query-time status
	// filter is the authority on visibility, not the index.
	for _, row := range []struct {
		table, id string
	}{
		{"blog_post_search", "b1"}, {"blog_post_search", "b2"}, {"blog_post_search", "b3"},
		{"blog_post_search", "b-archived"}, {"project_search", "p1"},
	} {
		var rowid int64
		if err := db.QueryRow("SELECT rowid FROM "+row.table[:strings.Index(row.table, "_search")]+"s WHERE id = ?", row.id).Scan(&rowid); err != nil {
			t.Fatalf("rowid for %s/%s: %v", row.id, row.table, err)
		}
		row := row
		indexRow(func(ctx context.Context, tx *sql.Tx) error {
			switch row.table {
			case "blog_post_search":
				var title string
				if err := tx.QueryRowContext(ctx, "SELECT title FROM blog_posts WHERE rowid = ?", rowid).Scan(&title); err != nil {
					return err
				}
				return store.IndexBlogPostTx(ctx, tx, rowid, title, "", "")
			default:
				var title string
				if err := tx.QueryRowContext(ctx, "SELECT title FROM projects WHERE rowid = ?", rowid).Scan(&title); err != nil {
					return err
				}
				return store.IndexProjectTx(ctx, tx, rowid, title, "")
			}
		})
	}

	return db, middleware.RequestID()(handler.Search(db, "https://fans.example"))
}

func doSearch(t *testing.T, h http.Handler, target string) (*httptest.ResponseRecorder, map[string]any) {
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

// searchItemKeys extracts type:id pairs from a decoded envelope.
func searchItemKeys(t *testing.T, body map[string]any) []string {
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
		out = append(out, m["type"].(string)+":"+m["id"].(string))
	}
	return out
}

// TestSearch_Success pins the accepted success envelope: merged items
// ordered newest first, the type discriminator, canonical API time, the
// absolute masked thumbnail URL, no top-level total (the count rides
// pageInfo), application/json, X-Request-ID, and Cache-Control: no-store.
func TestSearch_Success(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)
	rec, body := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("κοινή λέξη"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if id := rec.Header().Get("X-Request-ID"); id == "" {
		t.Error("X-Request-ID missing")
	}

	got := searchItemKeys(t, body)
	want := []string{"post:b2", "post:b3"}
	if len(got) != len(want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d = %q, want %q", i, got[i], want[i])
		}
	}

	if _, has := body["total"]; has {
		t.Error("total must not be present")
	}

	item := body["items"].([]any)[0].(map[string]any)
	if item["type"] != "post" || item["id"] != "b2" {
		t.Errorf("first item type/id = %v/%v", item["type"], item["id"])
	}
	if !strings.HasPrefix(item["publishedAt"].(string), "1970-") {
		t.Errorf("publishedAt = %v, want canonical API time", item["publishedAt"])
	}
	// The stored http(s) URL passes through unchanged (mediaURL).
	if item["thumbnailUrl"] != "https://example.com/b2.jpg" {
		t.Errorf("thumbnailUrl = %v, want the stored absolute URL", item["thumbnailUrl"])
	}

	pageInfo := body["pageInfo"].(map[string]any)
	if pageInfo["hasNextPage"] != false || pageInfo["endCursor"] != nil {
		t.Errorf("pageInfo = %v, want hasNextPage=false endCursor=null", pageInfo)
	}
}

// TestSearch_SubtitleOnWire pins the merged item's subtitle contract: the
// blog hit carries the stored subtitle, the project hit an empty string,
// and the key is present on every item.
func TestSearch_SubtitleOnWire(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)
	if _, err := db.Exec(`UPDATE blog_posts SET subtitle = 'Επεισόδιο 1.026' WHERE id = 'b1'`); err != nil {
		t.Fatalf("set subtitle: %v", err)
	}

	_, postBody := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("καλημέρα"))
	posts := postBody["items"].([]any)
	if len(posts) != 1 {
		t.Fatalf("post items = %v, want one hit", posts)
	}
	post := posts[0].(map[string]any)
	if got, has := post["subtitle"]; !has || got != "Επεισόδιο 1.026" {
		t.Errorf("post subtitle = %q (present %v), want the stored subtitle", got, has)
	}

	_, projectBody := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("ταξίδι")+"&type=projects")
	projects := projectBody["items"].([]any)
	if len(projects) != 1 {
		t.Fatalf("project items = %v, want one hit", projects)
	}
	project := projects[0].(map[string]any)
	if got, has := project["subtitle"]; !has || got != "" {
		t.Errorf("project subtitle = %q (present %v), want an empty string", got, has)
	}
}

// TestSearch_GreekMatching pins the accent/case/prefix contract end to end:
// the same word matches accented, unaccented, uppercase,
// and prefixed — and unpublished content never surfaces.
func TestSearch_GreekMatching(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)

	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"καλημέρα", []string{"post:b1"}},
		{"καλημερα", []string{"post:b1"}},
		{"ΚΑΛΗΜΕΡΑ", []string{"post:b1"}},
		{"καλημ", []string{"post:b1"}},
		{"ταξίδι", []string{"project:p1"}},
		{"ταξιδι", []string{"project:p1"}},
		{"κρυφό", []string{}},
		{"κρυφο", []string{}},
		{"αρχείο", []string{}},
		{"αρχειο", []string{}},
	} {
		rec, body := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape(tc.q))
		if rec.Code != http.StatusOK {
			t.Fatalf("q=%q status = %d", tc.q, rec.Code)
		}
		got := searchItemKeys(t, body)
		if len(got) != len(tc.want) {
			t.Errorf("q=%q items = %v, want %v", tc.q, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("q=%q position %d = %q, want %q", tc.q, i, got[i], tc.want[i])
			}
		}
	}
}

// TestSearch_TypeFilter pins type=posts/projects/all.
func TestSearch_TypeFilter(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)

	_, all := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("κοινή λέξη")+"&type=all")
	if got := searchItemKeys(t, all); len(got) != 2 {
		t.Errorf("type=all: %v", got)
	}

	_, posts := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("κοινή λέξη")+"&type=posts")
	if got := searchItemKeys(t, posts); len(got) != 2 || got[0] != "post:b2" {
		t.Errorf("type=posts: %v", got)
	}

	// "ταξίδι" matches only the project — the posts scope must return none.
	_, projects := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("ταξίδι")+"&type=projects")
	if got := searchItemKeys(t, projects); len(got) != 1 || got[0] != "project:p1" {
		t.Errorf("type=projects: %v", got)
	}
}

// TestSearch_EmptyResult pins the empty envelope.
func TestSearch_EmptyResult(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)
	rec, body := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("ανύπαρκτη λέξη"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("items = %v, want []", body["items"])
	}
	pageInfo := body["pageInfo"].(map[string]any)
	if pageInfo["hasNextPage"] != false || pageInfo["endCursor"] != nil {
		t.Errorf("pageInfo = %v", pageInfo)
	}
}

// TestSearch_KeysetPagination walks the "κοινή λέξη" results with limit=1
// through the opaque cursor and pins no duplicates and a final page.
func TestSearch_KeysetPagination(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)

	seen := make(map[string]bool)
	target := "/api/v1/search?q=" + urlQueryEscape("κοινή λέξη") + "&limit=1"
	pages := 0
	for {
		rec, body := doSearch(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		keys := searchItemKeys(t, body)
		if len(keys) != 1 {
			t.Fatalf("page %d: items = %v, want exactly 1", pages, keys)
		}
		if seen[keys[0]] {
			t.Fatalf("duplicate %q across pages", keys[0])
		}
		seen[keys[0]] = true
		pages++

		pageInfo := body["pageInfo"].(map[string]any)
		if pageInfo["hasNextPage"] != true {
			break
		}
		target = "/api/v1/search?q=" + urlQueryEscape("κοινή λέξη") + "&limit=1&after=" + pageInfo["endCursor"].(string)
	}
	if pages != 2 || len(seen) != 2 {
		t.Errorf("pages=%d seen=%v, want 2 pages with both posts", pages, seen)
	}
}

// TestSearch_CursorCrossEndpointRejected pins the cursor namespace: a blog
// cursor must never be accepted by search.
func TestSearch_CursorCrossEndpointRejected(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)

	// A cursor in the blog namespace (v1.) is structurally not a search
	// cursor → 400.
	rec, _ := doSearch(t, h, "/api/v1/search?q=test&after=v1."+"eyJ2IjoxLCJwIjoxMDAwLCJpIjoiYjEifQ==")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("cross-endpoint cursor status = %d, want 400", rec.Code)
	}
}

// TestSearch_TitleSort pins the title sort end to end: the alphabetical
// ordering that disagrees with publish order, the cursor namespace that
// continues it, and the sort binding that rejects a cursor minted under the
// other sort.
func TestSearch_TitleSort(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)

	// Four posts sharing a word that appears nowhere else, with titles whose
	// alphabetical order is deliberately the reverse of their publish order.
	for _, row := range []struct {
		id, title string
		published int64
	}{
		{"b-omega", "Ωμέγα", 9000},
		{"b-alfa", "Άλφα", 8000},
		{"b-vita", "Βήτα", 7000},
		{"b-gamma", "Γάμμα", 6000},
	} {
		insertSearchPost(t, db, row.id, row.title, row.published)
	}

	base := "/api/v1/search?q=" + urlQueryEscape("ταξινόμηση")

	// Default and explicit sort=date agree: newest first.
	_, defaultBody := doSearch(t, h, base)
	_, dateBody := doSearch(t, h, base+"&sort=date")
	dateOrder := searchItemKeys(t, dateBody)
	if got := searchItemKeys(t, defaultBody); !slices.Equal(got, dateOrder) {
		t.Errorf("default order %v differs from sort=date %v", got, dateOrder)
	}
	wantDate := []string{"post:b-omega", "post:b-alfa", "post:b-vita", "post:b-gamma"}
	if !slices.Equal(dateOrder, wantDate) {
		t.Fatalf("date order = %v, want %v", dateOrder, wantDate)
	}

	// sort=title: Greek alphabet order, which is the reverse of the dates.
	rec, body := doSearch(t, h, base+"&sort=title")
	if rec.Code != http.StatusOK {
		t.Fatalf("sort=title status = %d (%v)", rec.Code, body)
	}
	wantTitle := []string{"post:b-alfa", "post:b-vita", "post:b-gamma", "post:b-omega"}
	if got := searchItemKeys(t, body); !slices.Equal(got, wantTitle) {
		t.Fatalf("title order = %v, want %v", got, wantTitle)
	}

	// Walk the title sort one item at a time through the opaque cursor: the
	// namespace is st1. (the date sort keeps sv1.), the order is stable, and
	// the walk terminates on a null endCursor with every item seen once.
	seen := make([]string, 0, len(wantTitle))
	target := base + "&sort=title&limit=1"
	for {
		rec, body := doSearch(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("walk status = %d (%v)", rec.Code, body)
		}
		keys := searchItemKeys(t, body)
		if len(keys) != 1 {
			t.Fatalf("page items = %v, want exactly 1", keys)
		}
		seen = append(seen, keys[0])
		pageInfo := body["pageInfo"].(map[string]any)
		if pageInfo["hasNextPage"] != true {
			if pageInfo["endCursor"] != nil {
				t.Errorf("final page endCursor = %v, want null", pageInfo["endCursor"])
			}
			break
		}
		cursor, _ := pageInfo["endCursor"].(string)
		if !strings.HasPrefix(cursor, "st1.") {
			t.Fatalf("title-sort cursor %q must use the st1. namespace", cursor)
		}
		target = base + "&sort=title&limit=1&after=" + cursor
	}
	if !slices.Equal(seen, wantTitle) {
		t.Errorf("walk = %v, want %v", seen, wantTitle)
	}

	// Sort binding: each sort decodes only its own namespace, so a cursor
	// from the other sort is one opaque invalid token (400).
	_, onePage := doSearch(t, h, base+"&sort=title&limit=1")
	titleCursor := onePage["pageInfo"].(map[string]any)["endCursor"].(string)
	rec, _ = doSearch(t, h, base+"&after="+titleCursor)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("st1. cursor on the date sort = %d, want 400", rec.Code)
	}

	_, datePage := doSearch(t, h, base+"&limit=1")
	dateCursor := datePage["pageInfo"].(map[string]any)["endCursor"].(string)
	if !strings.HasPrefix(dateCursor, "sv1.") {
		t.Fatalf("date-sort cursor %q must use the sv1. namespace", dateCursor)
	}
	rec, _ = doSearch(t, h, base+"&sort=title&after="+dateCursor)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("sv1. cursor on the title sort = %d, want 400", rec.Code)
	}
}

// TestSearch_OldestSort pins the oldest-first ordering end to end: publish
// time ascending with the id tie-break, its own cursor namespace (so1. — the
// two date sorts continue the same tuple in opposite directions, so the
// cursor may not cross), and the sort binding that rejects a foreign cursor.
func TestSearch_OldestSort(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)

	// Three posts sharing a word that appears nowhere else, with publish
	// times deliberately the reverse of the default order.
	for _, row := range []struct {
		id, title string
		published int64
	}{
		{"b-new", "Πρώτη Παλαιότητα", 9000},
		{"b-mid", "Δεύτερη Παλαιότητα", 8000},
		{"b-old", "Τρίτη Παλαιότητα", 7000},
	} {
		insertSearchPost(t, db, row.id, row.title, row.published)
	}

	base := "/api/v1/search?q=" + urlQueryEscape("παλαιότητα")
	wantOldest := []string{"post:b-old", "post:b-mid", "post:b-new"}

	rec, body := doSearch(t, h, base+"&sort=oldest")
	if rec.Code != http.StatusOK {
		t.Fatalf("sort=oldest status = %d (%v)", rec.Code, body)
	}
	if got := searchItemKeys(t, body); !slices.Equal(got, wantOldest) {
		t.Fatalf("oldest order = %v, want %v", got, wantOldest)
	}

	// Walk one item at a time through the so1. namespace: every item exactly
	// once, ending on a null endCursor.
	seen := make([]string, 0, len(wantOldest))
	target := base + "&sort=oldest&limit=1"
	for {
		rec, body := doSearch(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("walk status = %d (%v)", rec.Code, body)
		}
		keys := searchItemKeys(t, body)
		if len(keys) != 1 {
			t.Fatalf("page items = %v, want exactly 1", keys)
		}
		seen = append(seen, keys[0])
		pageInfo := body["pageInfo"].(map[string]any)
		if pageInfo["hasNextPage"] != true {
			if pageInfo["endCursor"] != nil {
				t.Errorf("final page endCursor = %v, want null", pageInfo["endCursor"])
			}
			break
		}
		cursor, _ := pageInfo["endCursor"].(string)
		if !strings.HasPrefix(cursor, "so1.") {
			t.Fatalf("oldest-sort cursor %q must use the so1. namespace", cursor)
		}
		target = base + "&sort=oldest&limit=1&after=" + cursor
	}
	if !slices.Equal(seen, wantOldest) {
		t.Errorf("walk = %v, want %v", seen, wantOldest)
	}

	// Sort binding: the two date sorts read opposite directions of the same
	// tuple, so neither may continue the other's page.
	_, onePage := doSearch(t, h, base+"&sort=oldest&limit=1")
	oldestCursor := onePage["pageInfo"].(map[string]any)["endCursor"].(string)
	if rec, _ := doSearch(t, h, base+"&sort=oldest&after="+oldestCursor); rec.Code != http.StatusOK {
		t.Errorf("so1. cursor on its own sort = %d, want 200", rec.Code)
	}
	rec, _ = doSearch(t, h, base+"&after="+oldestCursor)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("so1. cursor on the newest-first sort = %d, want 400", rec.Code)
	}
	_, datePage := doSearch(t, h, base+"&limit=1")
	dateCursor := datePage["pageInfo"].(map[string]any)["endCursor"].(string)
	rec, _ = doSearch(t, h, base+"&sort=oldest&after="+dateCursor)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("sv1. cursor on the oldest-first sort = %d, want 400", rec.Code)
	}
}

// TestSearch_TitleCursorRejections pins the title cursor's own bounds: an
// over-long token and a well-formed payload carrying a key beyond the rune
// bound are both 400 (the codec never trusts its input). Keys the server can
// legitimately mint — including an empty key or one carrying control
// characters — are covered by the round-trip test instead.
func TestSearch_TitleCursorRejections(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)

	// A VALID payload padded past the bound: only the length check can reject
	// it (base64 garbage would fail the JSON step anyway, which would leave
	// the bound itself unpinned).
	overLong := "st1." + base64.RawURLEncoding.EncodeToString([]byte(
		`{"v":1,"k":"","i":"b1"}`+strings.Repeat(" ", 2100)))
	// Valid JSON, but the key exceeds search.SortKeyRuneLimit runes.
	longKey := base64.RawURLEncoding.EncodeToString([]byte(
		`{"v":1,"k":"` + strings.Repeat("α", 201) + `","i":"b1"}`))

	for name, cursor := range map[string]string{
		"over-long token":           overLong,
		"key beyond the rune bound": "st1." + longKey,
	} {
		t.Run(name, func(t *testing.T) {
			rec, body := doSearch(t, h, "/api/v1/search?q=test&sort=title&after="+cursor)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%v)", rec.Code, body)
			}
		})
	}
}

// insertSearchPost adds one published blog post carrying the shared fixture
// term in its description and indexes it — the import contract (content row
// + index entry in one transaction).
func insertSearchPost(t *testing.T, db *sql.DB, id, title string, published int64) {
	t.Helper()
	insertSearchPostBody(t, db, id, title, published, "ταξινόμηση")
}

// insertSearchPostBody is insertSearchPost with an explicit description, for
// fixtures whose search term (and so their match set) must be its own.
func insertSearchPostBody(t *testing.T, db *sql.DB, id, title string, published int64, description string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := tx.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, '', ?, 'https://example.com/t.jpg', 'published', ?, 1000, 1000)`,
		id, title, description, published)
	if err != nil {
		tx.Rollback()
		t.Fatalf("insert %q: %v", id, err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		t.Fatalf("rowid %q: %v", id, err)
	}
	if err := store.IndexBlogPostTx(t.Context(), tx, rowid, title, "", description); err != nil {
		tx.Rollback()
		t.Fatalf("index %q: %v", id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit %q: %v", id, err)
	}
}

// TestSearch_TitleSort_CursorRoundTrip pins the mint/decode agreement: every
// key the server mints must decode again on the next request. A title
// carrying a control character and a title that folds away entirely are the
// two shapes a stricter decoder would reject mid-walk (decode
// must accept exactly what mint can produce).
func TestSearch_TitleSort_CursorRoundTrip(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)
	insertSearchPost(t, db, "b-tab", "Άλφα\tΒήτα", 2000)
	insertSearchPost(t, db, "b-mark", "\u0301", 1000)

	base := "/api/v1/search?q=" + urlQueryEscape("ταξινόμηση") + "&sort=title&limit=1"
	seen := make([]string, 0, 2)
	target := base
	for {
		rec, body := doSearch(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("walk status = %d (%v)", rec.Code, body)
		}
		keys := searchItemKeys(t, body)
		if len(keys) != 1 {
			t.Fatalf("page items = %v, want exactly 1", keys)
		}
		seen = append(seen, keys[0])
		pageInfo := body["pageInfo"].(map[string]any)
		if pageInfo["hasNextPage"] != true {
			break
		}
		target = base + "&after=" + pageInfo["endCursor"].(string)
	}
	// The mark-only title folds to an empty key, so it sorts first.
	if want := []string{"post:b-mark", "post:b-tab"}; !slices.Equal(seen, want) {
		t.Errorf("walk = %v, want %v", seen, want)
	}
}

// TestSearch_DateRange pins the from/to window end to end: inclusive at both
// ends as calendar days, composed with
// the type filter, either sort, and cursor paging, with out-of-window rows
// never leaving the store.
func TestSearch_DateRange(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)

	const day = int64(86_400_000)
	jan1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	insertSearchPostBody(t, db, "b-jan1", "Ιανουάριος", jan1, "χρονολόγιο")
	insertSearchPostBody(t, db, "b-jan3", "Τρίτη Μέρα", jan1+2*day, "χρονολόγιο")
	insertSearchProjectBody(t, db, "p-jan2", "Έργο Μέρας", jan1+day, "χρονολόγιο")

	base := "/api/v1/search?q=" + urlQueryEscape("χρονολόγιο")
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"no window", "", []string{"post:b-jan3", "project:p-jan2", "post:b-jan1"}},
		{"single day", "&from=2025-01-01&to=2025-01-01", []string{"post:b-jan1"}},
		{"empty day", "&from=2025-01-02&to=2025-01-02", []string{"project:p-jan2"}},
		{"inclusive end day", "&from=2025-01-01&to=2025-01-03", []string{"post:b-jan3", "project:p-jan2", "post:b-jan1"}},
		{"from only", "&from=2025-01-02", []string{"post:b-jan3", "project:p-jan2"}},
		{"to only", "&to=2025-01-01", []string{"post:b-jan1"}},
		{"projects scope", "&from=2025-01-01&to=2025-01-03&type=projects", []string{"project:p-jan2"}},
		{"posts scope", "&from=2025-01-01&to=2025-01-03&type=posts", []string{"post:b-jan3", "post:b-jan1"}},
		{"window with the title sort", "&from=2025-01-01&to=2025-01-03&sort=title", []string{"project:p-jan2", "post:b-jan1", "post:b-jan3"}},
		// The epoch day is a REAL bound: from=1970-01-01&to=1970-01-01
		// selects only epoch-day rows — a zero sentinel would disable the
		// window and return every row.
		{"epoch day excludes later rows", "&from=1970-01-01&to=1970-01-01", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := doSearch(t, h, base+tc.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (%v)", rec.Code, body)
			}
			if got := searchItemKeys(t, body); !slices.Equal(got, tc.want) {
				t.Errorf("items = %v, want %v", got, tc.want)
			}
		})
	}

	// The window bounds the cursor walk: one item per page inside the window,
	// then a clean end with no out-of-window row leaking in.
	seen := make([]string, 0, 2)
	target := base + "&from=2025-01-01&to=2025-01-03&limit=1"
	for {
		rec, body := doSearch(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("walk status = %d (%v)", rec.Code, body)
		}
		seen = append(seen, searchItemKeys(t, body)...)
		pageInfo := body["pageInfo"].(map[string]any)
		if pageInfo["hasNextPage"] != true {
			break
		}
		target = base + "&from=2025-01-01&to=2025-01-03&limit=1&after=" + pageInfo["endCursor"].(string)
	}
	if want := []string{"post:b-jan3", "project:p-jan2", "post:b-jan1"}; !slices.Equal(seen, want) {
		t.Errorf("windowed walk = %v, want %v", seen, want)
	}
}

// insertSearchProjectBody is the project sibling of insertSearchPostBody: one
// published project with a chosen description and publish time, plus its index
// row in the same transaction.
func insertSearchProjectBody(t *testing.T, db *sql.DB, id, title string, published int64, description string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := tx.Exec(`INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, 'slug-' || ?, 'https://example.com/t.jpg', 'published', ?, 1000, 1000)`,
		id, title, description, id, published)
	if err != nil {
		tx.Rollback()
		t.Fatalf("insert project %q: %v", id, err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		t.Fatalf("project rowid %q: %v", id, err)
	}
	if err := store.IndexProjectTx(t.Context(), tx, rowid, title, description); err != nil {
		tx.Rollback()
		t.Fatalf("index project %q: %v", id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit project %q: %v", id, err)
	}
}

func TestSearch_RejectsMalformedParameters(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)

	cases := []struct {
		name, target string
	}{
		{"unknown parameter", "/api/v1/search?q=test&extra=1"},
		{"duplicate q", "/api/v1/search?q=test&q=again"},
		{"duplicate type", "/api/v1/search?q=test&type=all&type=posts"},
		{"duplicate sort", "/api/v1/search?q=test&sort=date&sort=title"},
		{"non-integer limit", "/api/v1/search?q=test&limit=abc"},
		{"malformed query string", "/api/v1/search?q=test%zz"},
		{"garbage cursor", "/api/v1/search?q=test&after=not-a-cursor"},
		{"oversized cursor", "/api/v1/search?q=test&after=sv1." + strings.Repeat("A", 300)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := doSearch(t, h, tc.target)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%v)", rec.Code, body)
			}
		})
	}
}

func TestSearch_Validation422s(t *testing.T) {
	t.Parallel()

	_, h := setupSearch(t)

	longQ := strings.Repeat("α", 101)
	cases := []struct {
		name, target string
		field, code  string
	}{
		{"missing q", "/api/v1/search", "q", "required"},
		{"empty q", "/api/v1/search?q=", "q", "required"},
		{"whitespace q", "/api/v1/search?q=%20%20", "q", "required"},
		{"oversized q", "/api/v1/search?q=" + longQ, "q", "maxLength"},
		{"specials-only q", "/api/v1/search?q=%22%28%29%22", "q", "invalidFormat"},
		{"reserved-only q", "/api/v1/search?q=AND+OR+NOT", "q", "invalidFormat"},
		{"unknown type", "/api/v1/search?q=test&type=videos", "type", "invalidValue"},
		{"unknown sort", "/api/v1/search?q=test&sort=relevance", "sort", "invalidValue"},
		{"non-padded from", "/api/v1/search?q=test&from=2025-1-2", "from", "invalidFormat"},
		{"impossible day", "/api/v1/search?q=test&from=2025-02-30", "from", "invalidFormat"},
		{"instant instead of a day", "/api/v1/search?q=test&to=2025-02-01T00:00:00Z", "to", "invalidFormat"},
		{"date before the epoch", "/api/v1/search?q=test&from=1969-12-31", "from", "invalidFormat"},
		{"injection-shaped from", "/api/v1/search?q=test&from=1970-01-01%27%20OR%201%3D1--", "from", "invalidFormat"},
		{"inverted window", "/api/v1/search?q=test&from=2025-06-10&to=2025-06-01", "to", "invalidValue"},
		{"limit zero", "/api/v1/search?q=test&limit=0", "limit", "outOfRange"},
		{"limit over max", "/api/v1/search?q=test&limit=101", "limit", "outOfRange"},
		{"limit overflows int64", "/api/v1/search?q=test&limit=99999999999999999999", "limit", "outOfRange"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := doSearch(t, h, tc.target)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%v)", rec.Code, body)
			}
			violations, ok := body["violations"].([]any)
			if !ok || len(violations) != 1 {
				t.Fatalf("violations = %v, want exactly one", body["violations"])
			}
			v := violations[0].(map[string]any)
			if v["field"] != tc.field || v["code"] != tc.code {
				t.Errorf("violation = %v, want field=%q code=%q", v, tc.field, tc.code)
			}
		})
	}
}

// TestSearch_StoreFailureIs500 pins the failure path: a broken database
// produces a safe 500 problem, not a panic or leaked detail.
func TestSearch_StoreFailureIs500(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)
	db.Close() // the handler's store calls now fail

	rec, body := doSearch(t, h, "/api/v1/search?q=test")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body["type"] != "/problems/internal-error" {
		t.Errorf("problem type = %v", body["type"])
	}
}

// TestSearch_ThumbnailSchemeGuardEmitsNull pins the OpenAPI contract: a
// stored thumbnail that fails the http(s)/media guard is emitted as null,
// never as an empty string (project-list pattern, OpenAPI
// SearchListItem.thumbnailUrl typed [string, "null"]).
func TestSearch_ThumbnailSchemeGuardEmitsNull(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)

	// A published post with a hostile stored thumbnail, indexed normally.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := tx.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b-bad-thumb', 'Κακό Thumbnail', '', '', 'javascript:alert(1)', 'published', 7000, 1000, 1000)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("rowid: %v", err)
	}
	if err := store.IndexBlogPostTx(t.Context(), tx, rowid, "Κακό Thumbnail", "", ""); err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rec, body := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("thumbnail"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v, want exactly one hit", searchItemKeys(t, body))
	}
	item := items[0].(map[string]any)
	if item["thumbnailUrl"] != nil {
		t.Errorf("thumbnailUrl = %v, want null (guarded value must not reach the JSON)", item["thumbnailUrl"])
	}
}

// urlQueryEscape escapes a raw Greek/space query for use in a test URL.
func urlQueryEscape(s string) string {
	// Keep it dependency-free and deterministic: encode space and %.
	return strings.ReplaceAll(
		strings.ReplaceAll(s, "%", "%25"),
		" ", "%20")
}

// TestSearch_IndicatorCounts pins the counts through the merged search
// projection: post hits carry blog comment/favorite counts, project hits the
// project tables — the type discriminator never mixes the two.
func TestSearch_IndicatorCounts(t *testing.T) {
	t.Parallel()

	db, h := setupSearch(t)

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
			VALUES (?, 'u1', 'b1', NULLIF(?, ''), 'body', 0, 1, 1)`, c.id, c.parent); err != nil {
			t.Fatalf("insert comment %q: %v", c.id, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO project_comments
		(id, user_id, project_id, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES ('pc1', 'u1', 'p1', NULL, 'body', 0, 1, 1)`); err != nil {
		t.Fatalf("insert project comment: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms)
		VALUES ('u1', 'b1', 1)`); err != nil {
		t.Fatalf("insert favorite: %v", err)
	}

	// "Καλημέρα" matches only b1 (post); "ταξίδι" only p1 (project) —
	// unaccented per the accent matrix.
	rec, body := doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("καλημερα"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v, want exactly one post hit", searchItemKeys(t, body))
	}
	post := items[0].(map[string]any)
	if post["commentCount"] != float64(2) || post["favoriteCount"] != float64(1) {
		t.Errorf("post counts = (%v, %v), want (2, 1)", post["commentCount"], post["favoriteCount"])
	}

	rec, body = doSearch(t, h, "/api/v1/search?q="+urlQueryEscape("ταξιδι"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", rec.Code, body)
	}
	items = body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v, want exactly one project hit", searchItemKeys(t, body))
	}
	project := items[0].(map[string]any)
	if project["commentCount"] != float64(1) || project["favoriteCount"] != float64(0) {
		t.Errorf("project counts = (%v, %v), want (1, 0)", project["commentCount"], project["favoriteCount"])
	}
}
