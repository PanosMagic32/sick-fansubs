package migration

import (
	"database/sql"
	"testing"

	"sick-fansubs/internal/database"
)

// favoritesContentOIDs are the fixture content records the favorite arrays
// reference (legacy-blog.jsonl / legacy-projects.jsonl).
const (
	favBlogOID1    = "65b0a1c2d3e4f5a6b7c8d9e0"
	favBlogOID2    = "65c0a1c2d3e4f5a6b7c8d9e2"
	favProjectOID1 = "65b0d1a2b3c4d5e6f7a8b9e0"
)

// favUserID is the synthetic favoriting user — it doubles as the creator of
// the blog fixture's first record and the updater of the project fixture's
// first record, so those references resolve during the imports.
const favUserID = "65a0b1c2d3e4f5a6b7c8d9e1"

// favAnchorMS is the user's imported updated_at_ms: the favorites anchor.
const favAnchorMS = int64(1706781600000) // 2024-02-01T10:00:00.000Z

// favUser builds the synthetic favoriting user (rule 8 — synthetic data only).
func favUser(t *testing.T) LegacyUser {
	t.Helper()
	rec := validUserWithTimestamps(t, favUserID, `"2024-01-10T10:00:00.000Z"`, `"2024-02-01T10:00:00.000Z"`)
	rec.FavoriteBlogPostIds = []MongoObjectID{{OID: favBlogOID1}, {OID: favBlogOID2}}
	rec.FavoriteProjectIds = []MongoObjectID{{OID: favProjectOID1}}
	return rec
}

// insertBlogPostForFavorite inserts the minimal blog row the favorites
// import resolves against: the id is the DERIVED target id for the
// given legacy ObjectId, exactly as a real content import would produce.
func insertBlogPostForFavorite(t *testing.T, db *sql.DB, oid string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO blog_posts (id, title, thumbnail_url, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', 'https://example.com/t.jpg', 1, 1, 1)`,
		deriveTargetID(oid)); err != nil {
		t.Fatalf("insert blog post: %v", err)
	}
}

// insertProjectForFavorite is the projects twin of insertBlogPostForFavorite.
func insertProjectForFavorite(t *testing.T, db *sql.DB, oid string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO projects (id, title, slug, thumbnail_url, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', 'synthetic-project', 'https://example.com/t.jpg', 1, 1, 1)`,
		deriveTargetID(oid)); err != nil {
		t.Fatalf("insert project: %v", err)
	}
}

// TestImportFavorites_ResolvesImportedContent pins the derived-id contract
// end to end: users + content imported first, then the favorites arrays
// resolve through the preserved user ObjectId and the derived content ids —
// no saved mapping state. Timestamps follow the anchor + step-back policy
// exactly, and the foreign-key check stays clean.
func TestImportFavorites_ResolvesImportedContent(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	if _, err := ImportUsers(ctx, db, []LegacyUser{favUser(t)}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	if _, err := ImportBlogPosts(ctx, db, loadFixture(t)); err != nil {
		t.Fatalf("ImportBlogPosts: %v", err)
	}
	projects, err := ReadLegacyProjectsFile("testdata/legacy-projects.jsonl")
	if err != nil {
		t.Fatalf("read projects fixture: %v", err)
	}
	if _, err := ImportProjects(ctx, db, projects); err != nil {
		t.Fatalf("ImportProjects: %v", err)
	}

	report, err := ImportFavorites(ctx, db, []LegacyUser{favUser(t)})
	if err != nil {
		t.Fatalf("ImportFavorites: %v", err)
	}

	blog, proj := report.Favorites.Blog, report.Favorites.Projects
	if blog.Users != 1 || blog.SourceRows != 2 || blog.Imported != 2 || blog.AlreadyPresent != 0 || blog.UserMissing != 0 || blog.ContentMissing != 0 {
		t.Errorf("blog favorites counts: got %+v", blog)
	}
	if proj.Users != 1 || proj.SourceRows != 1 || proj.Imported != 1 || proj.AlreadyPresent != 0 || proj.UserMissing != 0 || proj.ContentMissing != 0 {
		t.Errorf("project favorites counts: got %+v", proj)
	}

	// The join rows carry the DERIVED content ids, and the timestamps
	// step back 1 ms per position: idx0 = anchor-1, idx1 = anchor.
	rows, err := db.QueryContext(ctx,
		`SELECT blog_post_id, created_at_ms FROM blog_post_favorites ORDER BY created_at_ms`)
	if err != nil {
		t.Fatalf("query blog favorites: %v", err)
	}
	defer rows.Close()
	var gotBlog []struct {
		id string
		ts int64
	}
	for rows.Next() {
		var row struct {
			id string
			ts int64
		}
		if err := rows.Scan(&row.id, &row.ts); err != nil {
			t.Fatalf("scan blog favorite: %v", err)
		}
		gotBlog = append(gotBlog, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate blog favorites: %v", err)
	}
	wantBlog := []struct {
		id string
		ts int64
	}{
		{deriveTargetID(favBlogOID1), favAnchorMS - 1},
		{deriveTargetID(favBlogOID2), favAnchorMS},
	}
	if len(gotBlog) != len(wantBlog) {
		t.Fatalf("blog favorite rows: got %d, want %d", len(gotBlog), len(wantBlog))
	}
	for i := range wantBlog {
		if gotBlog[i] != wantBlog[i] {
			t.Errorf("blog favorite[%d] = %+v, want %+v", i, gotBlog[i], wantBlog[i])
		}
	}

	var projRow struct {
		id string
		ts int64
	}
	if err := db.QueryRowContext(ctx,
		`SELECT project_id, created_at_ms FROM project_favorites`).Scan(&projRow.id, &projRow.ts); err != nil {
		t.Fatalf("query project favorite: %v", err)
	}
	if projRow.id != deriveTargetID(favProjectOID1) || projRow.ts != favAnchorMS {
		t.Errorf("project favorite = %+v, want (%q, %d)", projRow, deriveTargetID(favProjectOID1), favAnchorMS)
	}

	if err := database.VerifyForeignKeys(db); err != nil {
		t.Errorf("foreign key check: %v", err)
	}
}

// TestImportFavorites_UserMissing counts entries whose user was never
// imported (rejected or absent) and writes nothing.
func TestImportFavorites_UserMissing(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	report, err := ImportFavorites(ctx, db, []LegacyUser{favUser(t)})
	if err != nil {
		t.Fatalf("ImportFavorites: %v", err)
	}
	if report.Favorites.Blog.UserMissing != 2 || report.Favorites.Projects.UserMissing != 1 {
		t.Errorf("user-missing counts: blog=%d projects=%d, want 2/1",
			report.Favorites.Blog.UserMissing, report.Favorites.Projects.UserMissing)
	}
	if report.Favorites.Blog.Imported != 0 || report.Favorites.Projects.Imported != 0 {
		t.Errorf("imported counts: blog=%d projects=%d, want 0/0",
			report.Favorites.Blog.Imported, report.Favorites.Projects.Imported)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blog_post_favorites`).Scan(&n); err != nil {
		t.Fatalf("count blog favorites: %v", err)
	}
	if n != 0 {
		t.Errorf("blog favorite rows: got %d, want 0", n)
	}
}

// TestImportFavorites_ContentMissing counts entries whose derived content id
// resolves to nothing (dangling reference or content rejected at import).
func TestImportFavorites_ContentMissing(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	if _, err := ImportUsers(ctx, db, []LegacyUser{favUser(t)}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	report, err := ImportFavorites(ctx, db, []LegacyUser{favUser(t)})
	if err != nil {
		t.Fatalf("ImportFavorites: %v", err)
	}
	if report.Favorites.Blog.ContentMissing != 2 || report.Favorites.Projects.ContentMissing != 1 {
		t.Errorf("content-missing counts: blog=%d projects=%d, want 2/1",
			report.Favorites.Blog.ContentMissing, report.Favorites.Projects.ContentMissing)
	}
	if report.Favorites.Blog.Imported != 0 || report.Favorites.Projects.Imported != 0 {
		t.Errorf("imported counts: blog=%d projects=%d, want 0/0",
			report.Favorites.Blog.Imported, report.Favorites.Projects.Imported)
	}
}

// TestImportFavorites_IdempotentRerun: a second run over the same database
// changes nothing — every row counts as already-present.
func TestImportFavorites_IdempotentRerun(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	if _, err := ImportUsers(ctx, db, []LegacyUser{favUser(t)}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	// Content rows inserted directly: the favorites import only needs the
	// derived ids to exist in the content tables.
	for _, oid := range []string{favBlogOID1, favBlogOID2} {
		insertBlogPostForFavorite(t, db, oid)
	}
	insertProjectForFavorite(t, db, favProjectOID1)

	first, err := ImportFavorites(ctx, db, []LegacyUser{favUser(t)})
	if err != nil {
		t.Fatalf("ImportFavorites (first): %v", err)
	}
	if first.Favorites.Blog.Imported != 2 || first.Favorites.Projects.Imported != 1 {
		t.Fatalf("first-run imported counts: blog=%d projects=%d, want 2/1",
			first.Favorites.Blog.Imported, first.Favorites.Projects.Imported)
	}

	second, err := ImportFavorites(ctx, db, []LegacyUser{favUser(t)})
	if err != nil {
		t.Fatalf("ImportFavorites (second): %v", err)
	}
	if second.Favorites.Blog.Imported != 0 || second.Favorites.Blog.AlreadyPresent != 2 ||
		second.Favorites.Projects.Imported != 0 || second.Favorites.Projects.AlreadyPresent != 1 {
		t.Errorf("second-run counts: blog=%+v projects=%+v, want imported 0/0, alreadyPresent 2/1",
			second.Favorites.Blog, second.Favorites.Projects)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blog_post_favorites`).Scan(&n); err != nil {
		t.Fatalf("count blog favorites: %v", err)
	}
	if n != 2 {
		t.Errorf("blog favorite rows after re-run: got %d, want 2", n)
	}
}

// TestImportFavorites_InArrayDuplicate: the same content twice in one array
// imports once; the second entry counts as already-present.
func TestImportFavorites_InArrayDuplicate(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	rec := validUserWithTimestamps(t, favUserID, `"2024-01-10T10:00:00.000Z"`, `"2024-02-01T10:00:00.000Z"`)
	rec.FavoriteBlogPostIds = []MongoObjectID{{OID: favBlogOID1}, {OID: favBlogOID1}}
	if _, err := ImportUsers(ctx, db, []LegacyUser{rec}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	insertBlogPostForFavorite(t, db, favBlogOID1)

	report, err := ImportFavorites(ctx, db, []LegacyUser{rec})
	if err != nil {
		t.Fatalf("ImportFavorites: %v", err)
	}
	if report.Favorites.Blog.Imported != 1 || report.Favorites.Blog.AlreadyPresent != 1 {
		t.Errorf("duplicate-array counts: %+v, want imported 1, alreadyPresent 1", report.Favorites.Blog)
	}
}

// TestImportFavorites_StepBackFloor: an anchor too small for the step-back
// clamps at 1 ms (the created_at_ms > 0 CHECK) with a warning per clamp.
func TestImportFavorites_StepBackFloor(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	rec := validUserWithTimestamps(t, favUserID, `"1970-01-01T00:00:00.003Z"`, `"1970-01-01T00:00:00.003Z"`)
	// Five distinct entries with a 3 ms anchor: idx0→-1, idx1→0 clamp;
	// idx2→1, idx3→2, idx4→3 pass.
	oids := []string{"aaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccc", "dddddddddddddddddddddddd", "eeeeeeeeeeeeeeeeeeeeeeee"}
	for _, oid := range oids {
		rec.FavoriteBlogPostIds = append(rec.FavoriteBlogPostIds, MongoObjectID{OID: oid})
	}
	if _, err := ImportUsers(ctx, db, []LegacyUser{rec}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	for _, oid := range oids {
		insertBlogPostForFavorite(t, db, oid)
	}

	report, err := ImportFavorites(ctx, db, []LegacyUser{rec})
	if err != nil {
		t.Fatalf("ImportFavorites: %v", err)
	}
	if report.Favorites.Blog.Imported != 5 {
		t.Errorf("imported: got %d, want 5", report.Favorites.Blog.Imported)
	}
	var clamps int
	for _, w := range report.Warnings {
		if w.Field == "favorites.created_at_ms" {
			clamps++
		}
	}
	if clamps != 2 {
		t.Errorf("clamp warnings: got %d, want 2", clamps)
	}

	var minTS, maxTS int64
	if err := db.QueryRowContext(ctx,
		`SELECT MIN(created_at_ms), MAX(created_at_ms) FROM blog_post_favorites`).Scan(&minTS, &maxTS); err != nil {
		t.Fatalf("timestamp range: %v", err)
	}
	if minTS != 1 || maxTS != 3 {
		t.Errorf("timestamp range: got [%d, %d], want [1, 3]", minTS, maxTS)
	}
}

// TestImportFavorites_EmptyArrays: users without favorites leave the join
// tables empty and the report zeroed.
func TestImportFavorites_EmptyArrays(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	rec := validUserWithTimestamps(t, favUserID, `"2024-01-10T10:00:00.000Z"`, `"2024-02-01T10:00:00.000Z"`)
	report, err := ImportFavorites(ctx, db, []LegacyUser{rec})
	if err != nil {
		t.Fatalf("ImportFavorites: %v", err)
	}
	if report.Favorites.Blog.Users != 0 || report.Favorites.Blog.SourceRows != 0 ||
		report.Favorites.Projects.Users != 0 || report.Favorites.Projects.SourceRows != 0 {
		t.Errorf("empty-array counts: blog=%+v projects=%+v", report.Favorites.Blog, report.Favorites.Projects)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blog_post_favorites`).Scan(&n); err != nil {
		t.Fatalf("count blog favorites: %v", err)
	}
	if n != 0 {
		t.Errorf("blog favorite rows: got %d, want 0", n)
	}
}
