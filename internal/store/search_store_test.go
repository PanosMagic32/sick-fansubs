package store

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"sick-fansubs/internal/search"
)

// Fixtures — every helper writes content + index in ONE transaction,
// mirroring the import/CRUD contract.

// insertBlog inserts one blog post and its index entry atomically,
// returning the content rowid. published==nil means "no publish time" —
// such rows are NOT indexed.
func insertBlog(t *testing.T, db *sql.DB, id, title, subtitle, description string, published *int64) int64 {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, 'https://example.com/t.jpg', 'published', ?, 1000, 1000)`,
		id, title, subtitle, description, published)
	if err != nil {
		t.Fatalf("insert blog %q: %v", id, err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("blog rowid: %v", err)
	}
	if published != nil {
		if err := IndexBlogPostTx(context.Background(), tx, rowid, title, subtitle, description); err != nil {
			t.Fatalf("index blog %q: %v", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit blog %q: %v", id, err)
	}
	return rowid
}

// insertProject inserts one project and its index entry atomically.
func insertProject(t *testing.T, db *sql.DB, id, title, description string, published *int64) int64 {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, 'slug-' || ?, 'https://example.com/t.jpg', 'published', ?, 1000, 1000)`,
		id, title, description, id, published)
	if err != nil {
		t.Fatalf("insert project %q: %v", id, err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("project rowid: %v", err)
	}
	if published != nil {
		if err := IndexProjectTx(context.Background(), tx, rowid, title, description); err != nil {
			t.Fatalf("index project %q: %v", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit project %q: %v", id, err)
	}
	return rowid
}

func mustMatch(t *testing.T, q string) string {
	t.Helper()
	match, err := search.BuildQuery(q)
	if err != nil {
		t.Fatalf("BuildQuery(%q): %v", q, err)
	}
	return match
}

func searchIDs(t *testing.T, db *sql.DB, q string, scope SearchScope, limit int, after *PageKey) ([]SearchResult, bool) {
	t.Helper()
	var keyset *SearchKeyset
	if after != nil {
		keyset = &SearchKeyset{PublishedAtMS: after.PublishedAtMS, ID: after.ID}
	}
	return searchSorted(t, db, q, scope, SearchByDate, limit, keyset)
}

// searchSorted runs one page under an explicit sort and keyset (the title
// sort's continuation tuple is a collation key, so it cannot reuse PageKey).
func searchSorted(t *testing.T, db *sql.DB, q string, scope SearchScope, sort SearchSort, limit int, after *SearchKeyset) ([]SearchResult, bool) {
	t.Helper()
	return searchWindow(t, db, SearchQuery{
		Match: mustMatch(t, q),
		Scope: scope,
		Sort:  sort,
		Limit: limit,
		After: after,
	})
}

// searchWindow runs one page from a fully built store query — the shape the
// handler passes (window bounds included).
func searchWindow(t *testing.T, db *sql.DB, q SearchQuery) ([]SearchResult, bool) {
	t.Helper()
	results, hasNext, err := SearchContent(context.Background(), db, q)
	if err != nil {
		t.Fatalf("SearchContent(%q): %v", q.Match, err)
	}
	return results, hasNext
}

func resultKey(t *testing.T, r SearchResult) string {
	t.Helper()
	return r.Type + ":" + r.ID
}

func keysOf(t *testing.T, results []SearchResult) []string {
	t.Helper()
	var out []string
	for _, r := range results {
		out = append(out, resultKey(t, r))
	}
	return out
}

// TestSearchContent_GreekMatching pins the accent/case/prefix matrix:
// accent-insensitive Greek through Go normalization,
// case-insensitivity through the tokenizer, prefix on the last token.
func TestSearchContent_GreekMatching(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(5000)
	insertBlog(t, db, "b1", "Καλημέρα κόσμε", "", "ένα τεστ", &pub)

	for _, tc := range []struct {
		q    string
		want bool
	}{
		{"καλημέρα", true},       // accented exact
		{"καλημερα", true},       // unaccented — the core promise
		{"ΚΑΛΗΜΕΡΑ", true},       // uppercase
		{"καλημ", true},          // prefix
		{"κόσμε", true},          // second word, accented
		{"κοσμε", true},          // second word, unaccented
		{"καλημέρα τεστ", true},  // implicit AND across title/description
		{"τεστ", true},           // description token
		{"γειά", false},          // absent word
		{"καλημέρα γειά", false}, // AND failure
	} {
		results, _ := searchIDs(t, db, tc.q, SearchAll, 20, nil)
		got := len(results) > 0
		if got != tc.want {
			t.Errorf("search %q: got hit=%v, want %v", tc.q, got, tc.want)
		}
	}
}

// TestSearchContent_MergedOrdering pins the accepted ordering:
// published_at_ms DESC, id ASC across both content types.
func TestSearchContent_MergedOrdering(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	p1, p2, p3 := int64(1000), int64(3000), int64(2000)
	insertBlog(t, db, "b-early", "Κοινή Λέξη", "", "", &p1)
	insertProject(t, db, "p-late", "Κοινή Λέξη", "", &p2)
	insertBlog(t, db, "b-mid", "Κοινή Λέξη", "", "", &p3)

	results, _ := searchIDs(t, db, "κοινή λέξη", SearchAll, 20, nil)
	// Newest first: 3000 (project:p-late), 2000 (post:b-mid), 1000 (post:b-early).
	want := []string{"project:p-late", "post:b-mid", "post:b-early"}
	got := keysOf(t, results)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full order %v)", i, got[i], want[i], got)
		}
	}
}

// TestSearchContent_TieBreaksById pins the deterministic id tie-breaker:
// equal publish times order by id ascending.
func TestSearchContent_TieBreaksById(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(3000)
	insertBlog(t, db, "b-a", "Τίτλος", "", "", &pub)
	insertProject(t, db, "p-b", "Τίτλος", "", &pub)
	insertBlog(t, db, "b-c", "Τίτλος", "", "", &pub)

	results, _ := searchIDs(t, db, "τίτλος", SearchAll, 20, nil)
	got := keysOf(t, results)
	// Equal publish times: id ascending over the FULL id string —
	// "b-a" < "b-c" < "p-b".
	want := []string{"post:b-a", "post:b-c", "project:p-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full order %v)", i, got[i], want[i], got)
		}
	}
}

// TestSearchContent_ExcludesUnpublished pins the visibility contract: rows
// without a publish time, drafts, and archived rows never surface — even
// when an index entry exists for them (query-time filter is the authority).
func TestSearchContent_ExcludesUnpublished(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)

	pub := int64(5000)
	insertBlog(t, db, "b-pub", "Ορατό Άρθρο", "", "", &pub)

	// Draft: no publish time, never indexed.
	insertBlog(t, db, "b-draft", "Κρυφό Άρθρο", "", "", nil)

	// Defense in depth: index a row the public filter must still hide.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := tx.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b-archived', 'Αρχείο Άρθρο', '', '', 'https://example.com/t.jpg', 'archived', 9000, 1000, 1000)`)
	if err != nil {
		t.Fatalf("insert archived: %v", err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("rowid: %v", err)
	}
	// Indexed despite archived status — the query-time filter must hide it.
	if err := IndexBlogPostTx(context.Background(), tx, rowid, "Αρχείο Άρθρο", "", ""); err != nil {
		t.Fatalf("index archived: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	results, _ := searchIDs(t, db, "άρθρο", SearchAll, 20, nil)
	got := keysOf(t, results)
	if len(got) != 1 || got[0] != "post:b-pub" {
		t.Errorf("got %v, want only [post:b-pub]", got)
	}
}

// TestSearchContent_TypeFilter pins the type=posts/projects/all scope.
func TestSearchContent_TypeFilter(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(5000)
	insertBlog(t, db, "b1", "Σειρά Κοινή", "", "", &pub)
	insertProject(t, db, "p1", "Σειρά Κοινή", "", &pub)

	all, _ := searchIDs(t, db, "σειρά", SearchAll, 20, nil)
	if len(all) != 2 {
		t.Errorf("all: got %d hits, want 2", len(all))
	}
	posts, _ := searchIDs(t, db, "σειρά", SearchPosts, 20, nil)
	if keys := keysOf(t, posts); len(keys) != 1 || keys[0] != "post:b1" {
		t.Errorf("posts: got %v, want [post:b1]", keys)
	}
	projects, _ := searchIDs(t, db, "σειρά", SearchProjects, 20, nil)
	if keys := keysOf(t, projects); len(keys) != 1 || keys[0] != "project:p1" {
		t.Errorf("projects: got %v, want [project:p1]", keys)
	}
}

// TestSearchContent_SubtitlePerKind pins the merged row shape: the blog
// branch carries the post's subtitle, the project branch an empty string.
func TestSearchContent_SubtitlePerKind(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(5000)
	insertBlog(t, db, "b1", "Ιδιος Τιτλος", "Επεισόδιο 1.026", "Κείμενο", &pub)
	insertProject(t, db, "p1", "Ιδιος Τιτλος", "Κείμενο", &pub)

	results, _ := searchIDs(t, db, "ιδιος", SearchAll, 20, nil)
	if len(results) != 2 {
		t.Fatalf("got %d hits, want 2", len(results))
	}
	for _, r := range results {
		switch r.Type {
		case "post":
			if r.Subtitle != "Επεισόδιο 1.026" {
				t.Errorf("post subtitle = %q, want %q", r.Subtitle, "Επεισόδιο 1.026")
			}
		case "project":
			if r.Subtitle != "" {
				t.Errorf("project subtitle = %q, want empty", r.Subtitle)
			}
		default:
			t.Fatalf("unexpected type %q", r.Type)
		}
	}
}

func TestSearchContent_KeysetPagination(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	// Four posts sharing a term with distinct publish times, 1000 apart.
	for i, ms := range []int64{5000, 4000, 3000, 2000} {
		id := string(rune('a' + i))
		insertBlog(t, db, "b-"+id, "Σελίδα Τίτλος", "", "", &ms)
	}

	seen := make(map[string]bool)
	var after *PageKey
	pages := 0
	for {
		results, hasNext := searchIDs(t, db, "τίτλος", SearchAll, 2, after)
		pages++
		if len(results) == 0 {
			t.Fatal("empty page during pagination walk")
		}
		for _, r := range results {
			key := resultKey(t, r)
			if seen[key] {
				t.Fatalf("duplicate %q across pages", key)
			}
			seen[key] = true
		}
		if !hasNext {
			break
		}
		last := results[len(results)-1]
		after = &PageKey{PublishedAtMS: last.PublishedAtMS, ID: last.ID}
	}
	if pages != 2 {
		t.Errorf("pages = %d, want 2", pages)
	}
	if len(seen) != 4 {
		t.Errorf("seen %d unique results, want 4", len(seen))
	}
}

// TestSearchContent_OldestSort pins the oldest-first ordering and its
// continuation: published_at_ms ascending with the id ascending tie-break,
// walked through (publishedAtMs, id) pages exactly once — the mirror of the
// default walk.
func TestSearchContent_OldestSort(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	early, mid, late := int64(2000), int64(3000), int64(5000)
	// The equal-instant pair is inserted in reverse id order, so a dropped id
	// tie-break would fail the expected sequence.
	insertBlog(t, db, "b-e2", "Παλαιά Λέξη", "", "", &early)
	insertBlog(t, db, "b-e1", "Παλαιά Λέξη", "", "", &early)
	insertBlog(t, db, "b-mid", "Παλαιά Λέξη", "", "", &mid)
	insertBlog(t, db, "b-late", "Παλαιά Λέξη", "", "", &late)

	seen := make([]string, 0, 4)
	var after *SearchKeyset
	pages := 0
	for {
		results, hasNext := searchSorted(t, db, "παλαιά λέξη", SearchAll, SearchByOldest, 1, after)
		pages++
		if len(results) == 0 {
			t.Fatal("empty page during oldest-sort walk")
		}
		for _, r := range results {
			seen = append(seen, resultKey(t, r))
		}
		if !hasNext {
			break
		}
		last := results[len(results)-1]
		after = &SearchKeyset{PublishedAtMS: last.PublishedAtMS, ID: last.ID}
	}
	want := []string{"post:b-e1", "post:b-e2", "post:b-mid", "post:b-late"}
	if !slices.Equal(seen, want) {
		t.Errorf("oldest walk = %v, want %v", seen, want)
	}
	if pages != 4 {
		t.Errorf("pages = %d, want 4", pages)
	}
}

// TestSearchContent_TitleSort_GreekOrder pins the title sort through the
// real SQL path: the sf_greek collation registered with the driver decides
// the order, and equal keys fall back to the id tie-break. The ids
// deliberately DISAGREE with the title order, so
// a fall-through to the date branch would fail this test.
func TestSearchContent_TitleSort_GreekOrder(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(3000)
	// Every row carries the same searchable word, so MATCH includes all of
	// them and only the ordering differs. Published times are equal too, so
	// the date branch is exactly the id order — the opposite of the expected
	// sequence below.
	insertBlog(t, db, "b01", "Ωκεανός", "", "κοινή λέξη", &pub)
	insertBlog(t, db, "b02", "Βασιλιάς", "", "κοινή λέξη", &pub)
	insertBlog(t, db, "b03", "Άμος Ήχος", "", "κοινή λέξη", &pub)
	insertBlog(t, db, "b04", "Άμος Ήχος", "", "κοινή λέξη", &pub)
	insertProject(t, db, "p05", "One Piece", "κοινή λέξη", &pub)
	insertProject(t, db, "p06", "5 Αιώνες", "κοινή λέξη", &pub)

	results, hasNext := searchSorted(t, db, "κοινή λέξη", SearchAll, SearchByTitle, 20, nil)
	if hasNext {
		t.Error("hasNext = true for a single complete page")
	}
	got := keysOf(t, results)
	want := []string{
		"post:b03", "post:b04", // αμος ηχος — equal keys, id ascending
		"post:b02",    // βασιλιας
		"post:b01",    // ωκεανος
		"project:p06", // 5 …
		"project:p05", // One …
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full order %v)", i, got[i], want[i], got)
		}
	}
}

// TestSearchContent_TitleSort_KeysetWalk pins the title sort's continuation
// tuple: walking (collation key, id) in pages returns every match exactly
// once, including the equal-key pair whose boundary is decided by the id
// tie-break alone.
func TestSearchContent_TitleSort_KeysetWalk(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(3000)
	insertBlog(t, db, "b01", "Άμλετ", "", "σελίδα λέξη", &pub)
	insertBlog(t, db, "b02", "Αμλετ", "", "σελίδα λέξη", &pub) // same key as b01
	insertBlog(t, db, "b03", "Βήτα", "", "σελίδα λέξη", &pub)
	insertProject(t, db, "p04", "Γάμμα", "σελίδα λέξη", &pub)
	insertProject(t, db, "p05", "Ωμέγα", "σελίδα λέξη", &pub)

	seen := make(map[string]bool)
	var after *SearchKeyset
	pages := 0
	for {
		results, hasNext := searchSorted(t, db, "σελίδα λέξη", SearchAll, SearchByTitle, 2, after)
		pages++
		if len(results) == 0 {
			t.Fatal("empty page during title-sort walk")
		}
		for _, r := range results {
			key := resultKey(t, r)
			if seen[key] {
				t.Fatalf("duplicate %q across pages", key)
			}
			seen[key] = true
		}
		if !hasNext {
			break
		}
		last := results[len(results)-1]
		after = &SearchKeyset{TitleKey: search.SortKey(last.Title), ID: last.ID}
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
	if len(seen) != 5 {
		t.Errorf("seen %d unique results, want 5", len(seen))
	}
	if !seen["post:b01"] || !seen["post:b02"] {
		t.Errorf("equal-key pair missing from the walk: %v", seen)
	}
}

// TestSearchContent_DateWindow pins the publication-date window: SinceMS is
// inclusive, UntilMS is exclusive (the
// handler turns an inclusive end day into the next day's start), the window
// applies to BOTH content kinds, and it composes with both sorts and with
// the keyset.
func TestSearchContent_DateWindow(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)

	early, mid, in, late, out := int64(1000), int64(2000), int64(2500), int64(3000), int64(3500)
	insertBlog(t, db, "b-early", "Πρώτο", "", "χρονικό παράθυρο", &early)
	insertBlog(t, db, "b-mid", "Δεύτερο", "", "χρονικό παράθυρο", &mid)
	insertBlog(t, db, "b-late", "Τρίτο", "", "χρονικό παράθυρο", &late)
	insertProject(t, db, "p-in", "Γέφυρα", "χρονικό παράθυρο", &in)
	insertProject(t, db, "p-out", "Αποκλείεται", "χρονικό παράθυρο", &out)

	since, until := int64(2000), int64(3000)
	window := func(scope SearchScope, sort SearchSort, limit int) ([]SearchResult, bool) {
		t.Helper()
		return searchWindow(t, db, SearchQuery{
			Match: mustMatch(t, "χρονικό παράθυρο"), Scope: scope, Sort: sort,
			SinceMS: &since, UntilMS: &until, Limit: limit,
		})
	}

	// [2000, 3000) holds b-mid and the project p-in — the lower bound is
	// inclusive, the upper bound excludes b-late (3000) and p-out (3500).
	// Newest first, so p-in (2500) precedes b-mid (2000).
	results, hasNext := window(SearchAll, SearchByDate, 20)
	if got, want := keysOf(t, results), []string{"project:p-in", "post:b-mid"}; !slices.Equal(got, want) {
		t.Errorf("date-sorted window = %v, want %v", got, want)
	}
	if hasNext {
		t.Error("hasNext = true for a single complete page")
	}

	// The title sort orders the same window by the collation key
	// (Γέφυρα «γ» before Δεύτερο «δ»).
	results, _ = window(SearchAll, SearchByTitle, 20)
	if got, want := keysOf(t, results), []string{"project:p-in", "post:b-mid"}; !slices.Equal(got, want) {
		t.Errorf("title-sorted window = %v, want %v", got, want)
	}

	// The window applies to the projects branch too (the conditions live on
	// the union; this pins it against a per-branch regression).
	results, _ = window(SearchProjects, SearchByDate, 20)
	if got, want := keysOf(t, results), []string{"project:p-in"}; !slices.Equal(got, want) {
		t.Errorf("projects-scoped window = %v, want %v", got, want)
	}

	// A zero-valued bound is APPLIED, not skipped as "absent": the epoch
	// day's first millisecond is a real lower bound (a zero sentinel would
	// disable the whole window).
	zero, one := int64(0), int64(1)
	results, _ = searchWindow(t, db, SearchQuery{
		Match: mustMatch(t, "χρονικό παράθυρο"), Scope: SearchAll, Sort: SearchByDate,
		SinceMS: &zero, UntilMS: &one, Limit: 20,
	})
	if got := keysOf(t, results); len(got) != 0 {
		t.Errorf("epoch-millisecond window = %v, want none", got)
	}

	// The window bounds the walk: a page sized to the window's contents
	// reports no continuation, and a half page reports one.
	full, hasNext := window(SearchAll, SearchByDate, 2)
	if len(full) != 2 || hasNext {
		t.Errorf("window-sized page = %v (hasNext %v), want both items and no continuation", keysOf(t, full), hasNext)
	}
	half, hasNext := window(SearchAll, SearchByDate, 1)
	if len(half) != 1 || !hasNext {
		t.Errorf("half page = %v (hasNext %v), want one item and a continuation", keysOf(t, half), hasNext)
	}
}

// TestCountSearchContent_ScopeAndWindow proves the count shares the page
// read's branch set and publication window: scope all counts both kinds,
// scope posts/projects narrows to one branch, and a window bound excludes
// the row outside it. Each case also mirrors its read — a page wide enough
// to hold the filtered set returns exactly the counted rows.
func TestCountSearchContent_ScopeAndWindow(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()
	older, newer := int64(2000), int64(4000)

	insertBlog(t, db, "b1", "One Piece", "", "luffy sails", &older)
	insertProject(t, db, "pr1", "One Piece", "luffy sails", &newer)

	match := mustMatch(t, "luffy")
	cases := []struct {
		name string
		q    SearchQuery
		want int
	}{
		{"all scopes count both kinds", SearchQuery{Match: match, Scope: SearchAll, Limit: 10}, 2},
		{"posts scope counts only the blog branch", SearchQuery{Match: match, Scope: SearchPosts, Limit: 10}, 1},
		{"projects scope counts only the project branch", SearchQuery{Match: match, Scope: SearchProjects, Limit: 10}, 1},
		{"since excludes the older blog post", SearchQuery{Match: match, Scope: SearchAll, SinceMS: &newer, Limit: 10}, 1},
		{"until excludes the newer project", SearchQuery{Match: match, Scope: SearchAll, UntilMS: &newer, Limit: 10}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CountSearchContent(ctx, db, tc.q)
			if err != nil {
				t.Fatalf("CountSearchContent: %v", err)
			}
			if got != tc.want {
				t.Errorf("count = %d, want %d", got, tc.want)
			}
			results, _, err := SearchContent(ctx, db, tc.q)
			if err != nil {
				t.Fatalf("SearchContent: %v", err)
			}
			if len(results) != tc.want {
				t.Errorf("read returned %d rows, count says %d — the mirror drifted", len(results), got)
			}
		})
	}
}

func TestDeleteBlogPostTx_RemovesFromResults(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(5000)
	rowid := insertBlog(t, db, "b1", "Προς Διαγραφή", "", "", &pub)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := DeleteBlogPostTx(context.Background(), tx, rowid); err != nil {
		t.Fatalf("DeleteBlogPostTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	results, _ := searchIDs(t, db, "διαγραφή", SearchAll, 20, nil)
	if len(results) != 0 {
		t.Errorf("search after index delete: got %v, want none", keysOf(t, results))
	}
}

// TestReindexAll pins the rebuild path: after
// clearing the index, ReindexAll refills it from the content tables with
// normalized text, indexes only publishable rows, and reports the counts.
func TestReindexAll(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	pub := int64(5000)
	insertBlog(t, db, "b1", "Καλημέρα κόσμε", "", "πρώτο", &pub)
	insertBlog(t, db, "b-draft", "Κρυφό", "", "", nil)
	insertProject(t, db, "p1", "Έργο Ταξίδι", "περιγραφή", &pub)

	// Corrupt/clear the index.
	if _, err := db.Exec(`DELETE FROM blog_post_search`); err != nil {
		t.Fatalf("clear blog index: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM project_search`); err != nil {
		t.Fatalf("clear project index: %v", err)
	}

	blogCount, projectCount, err := ReindexAll(context.Background(), db)
	if err != nil {
		t.Fatalf("ReindexAll: %v", err)
	}
	if blogCount != 1 || projectCount != 1 {
		t.Errorf("counts = (%d, %d), want (1, 1) — drafts must not be indexed", blogCount, projectCount)
	}

	// Accent-insensitive matching works after the rebuild (normalized text).
	results, _ := searchIDs(t, db, "καλημερα", SearchAll, 20, nil)
	if keys := keysOf(t, results); len(keys) != 1 || keys[0] != "post:b1" {
		t.Errorf("after rebuild: got %v, want [post:b1]", keys)
	}
	projects, _ := searchIDs(t, db, "ταξιδι", SearchProjects, 20, nil)
	if keys := keysOf(t, projects); len(keys) != 1 || keys[0] != "project:p1" {
		t.Errorf("projects after rebuild: got %v, want [project:p1]", keys)
	}
}

// TestSearchContent_Counts pins the indicator counts through the
// merged search projection: each branch computes its own counts against
// the matching comment/favorite tables, and the UNION ALL passthrough
// keeps them attached to the right hit.
func TestSearchContent_Counts(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	pub := int64(5000)
	insertBlog(t, db, "b1", "Κοινή Λέξη", "", "μετρητής", &pub)
	insertProject(t, db, "p1", "Κοινή Λέξη", "μετρητής", &pub)

	mustCreateComment(t, db, BlogContent, "bc1", "u1", "b1", nil, "top", 0, 1000, 1000)
	mustCreateComment(t, db, BlogContent, "bc2", "u1", "b1", nil, "second", 0, 2000, 2000)
	mustCreateComment(t, db, ProjectContent, "pc1", "u1", "p1", nil, "top", 0, 1000, 1000)
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 5000); err != nil {
		t.Fatalf("add blog favorite: %v", err)
	}
	if err := AddFavorite(ctx, db, ProjectContent, "u1", "p1", 5000); err != nil {
		t.Fatalf("add project favorite: %v", err)
	}

	results, _ := searchIDs(t, db, "μετρητής", SearchAll, 20, nil)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	byID := make(map[string]SearchResult, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}
	if b := byID["b1"]; b.CommentCount != 2 || b.FavoriteCount != 1 {
		t.Errorf("post counts = (%d comments, %d favorites), want (2, 1)",
			b.CommentCount, b.FavoriteCount)
	}
	if p := byID["p1"]; p.CommentCount != 1 || p.FavoriteCount != 1 {
		t.Errorf("project counts = (%d comments, %d favorites), want (1, 1)",
			p.CommentCount, p.FavoriteCount)
	}
}
