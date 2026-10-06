package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/store/storetest"
)

// Blog content-administration store tests:
// create/index atomicity, the published_at_ms transition, revision
// guards, cascades, and the draft-visibility gate.

func setupBlogAdminStore(t *testing.T) *sql.DB {
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

	// Three users: an admin (weight 2), a super-admin (3), a moderator (1).
	for _, u := range []storetest.UserSpec{
		{ID: "admin1", Username: "Admin", Role: "admin"},
		{ID: "sa1", Username: "SA", Role: "super-admin"},
		{ID: "mod1", Username: "Mod", Role: "moderator"},
		{ID: "user1", Username: "User"},
	} {
		storetest.InsertUser(t, db, u)
	}
	return db
}

func createPost(t *testing.T, db *sql.DB, id, status string, publishedMS int64) *StaffBlogPost {
	t.Helper()
	post, _, err := CreateBlogPost(t.Context(), db, CreateBlogPostParams{
		ID:            id,
		Title:         "Title " + id,
		Subtitle:      "Sub",
		Description:   "Desc " + id,
		ThumbnailURL:  "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:        status,
		CreatorID:     "admin1",
		UpdaterID:     "admin1",
		PublishedAtMS: publishedMS,
		NowMS:         5000,
	})
	if err != nil {
		t.Fatalf("CreateBlogPost(%s): %v", id, err)
	}
	return post
}

func ftsRowCount(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM blog_post_search s JOIN blog_posts p ON p.rowid = s.rowid WHERE p.id = ?`, id,
	).Scan(&n); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	return n
}

func TestCreateBlogPost_PublishedIndexesAtomically(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)

	post := createPost(t, db, "p1", "published", 5000)

	if post.Revision != 1 {
		t.Errorf("revision = %d, want 1", post.Revision)
	}
	if post.PublishedAtMS == nil || *post.PublishedAtMS != 5000 {
		t.Errorf("published_at_ms = %v, want 5000", post.PublishedAtMS)
	}
	if n := ftsRowCount(t, db, "p1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 (published rows index atomically)", n)
	}
}

func TestCreateBlogPost_DraftStaysOutOfTheIndex(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)

	post := createPost(t, db, "d1", "draft", 0)

	if post.PublishedAtMS != nil {
		t.Errorf("published_at_ms = %v, want nil for a draft", *post.PublishedAtMS)
	}
	if n := ftsRowCount(t, db, "d1"); n != 0 {
		t.Errorf("fts rows = %d, want 0 (drafts stay out of the index)", n)
	}
}

func TestUpdateBlogPost_PublishesAndReindexes(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "d1", "draft", 0)

	post, _, err := UpdateBlogPost(ctx, db, "d1", 1, UpdateBlogPostParams{
		Title:        "New Title",
		Subtitle:     "",
		Description:  "New desc",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published",
		UpdaterID:    "admin1",
		NowMS:        9000,
	})
	if err != nil {
		t.Fatalf("UpdateBlogPost: %v", err)
	}
	if post.Status != "published" || post.Revision != 2 {
		t.Errorf("post = status %s revision %d, want published/2", post.Status, post.Revision)
	}
	// The first transition into published stamps the publish time.
	if post.PublishedAtMS == nil || *post.PublishedAtMS != 9000 {
		t.Errorf("published_at_ms = %v, want the transition stamp 9000", post.PublishedAtMS)
	}
	if post.UpdaterID != "admin1" {
		t.Errorf("updater_id = %q, want admin1", post.UpdaterID)
	}
	if n := ftsRowCount(t, db, "d1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 after publishing", n)
	}

	// The re-publish path preserves the original stamp and reindexes the
	// NEW text.
	post, _, err = UpdateBlogPost(ctx, db, "d1", 2, UpdateBlogPostParams{
		Title:        "Updated Again",
		Subtitle:     "",
		Description:  "Second desc",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published",
		UpdaterID:    "sa1",
		NowMS:        12000,
	})
	if err != nil {
		t.Fatalf("UpdateBlogPost (re-publish): %v", err)
	}
	if post.PublishedAtMS == nil || *post.PublishedAtMS != 9000 {
		t.Errorf("published_at_ms = %v, want the ORIGINAL stamp 9000 preserved", post.PublishedAtMS)
	}
	var text string
	if err := db.QueryRow(
		`SELECT s.text FROM blog_post_search s JOIN blog_posts p ON p.rowid = s.rowid WHERE p.id = 'd1'`,
	).Scan(&text); err != nil {
		t.Fatalf("read fts text: %v", err)
	}
	if !strings.Contains(text, "Updated") || !strings.Contains(text, "Again") {
		t.Errorf("fts text = %q, want the UPDATED title indexed", text)
	}
}

func TestUpdateBlogPost_LeavingPublishedRemovesFavoritesKeepsIndex(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "p1", "published", 5000)
	if _, err := db.Exec(
		`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES ('user1', 'p1', 6000)`); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	post, _, err := UpdateBlogPost(ctx, db, "p1", 1, UpdateBlogPostParams{
		Title: "T", Subtitle: "", Description: "",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "archived", UpdaterID: "mod1", NowMS: 7000,
	})
	if err != nil {
		t.Fatalf("UpdateBlogPost: %v", err)
	}
	if post.PublishedAtMS == nil || *post.PublishedAtMS != 5000 {
		t.Errorf("published_at_ms = %v, want the stamp preserved on archive", post.PublishedAtMS)
	}
	var favs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_favorites WHERE blog_post_id = 'p1'`).Scan(&favs); err != nil {
		t.Fatalf("count favorites: %v", err)
	}
	if favs != 0 {
		t.Errorf("favorites = %d, want 0 after leaving published (the favorites cascade)", favs)
	}
	// The archived row KEEPS its stamp, and the index membership is
	// stamp-based — the FTS row survives; the query-time
	// status filter hides it from search.
	if n := ftsRowCount(t, db, "p1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 (stamp preserved → index kept; status filters at query time)", n)
	}
}

func TestUpdateBlogPost_StaleRevisionConflicts(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)

	createPost(t, db, "p1", "published", 5000)

	_, _, err := UpdateBlogPost(t.Context(), db, "p1", 99, UpdateBlogPostParams{
		Title: "T", Subtitle: "", Description: "",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "mod1", NowMS: 7000,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a stale revision", err)
	}

	_, _, err = UpdateBlogPost(t.Context(), db, "missing", 1, UpdateBlogPostParams{
		Title: "T", Subtitle: "", Description: "",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published", UpdaterID: "mod1", NowMS: 7000,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for an unknown id", err)
	}
}

func TestCreateBlogPost_WithDownloads(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	post, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
		ID:            "p1",
		Title:         "T",
		Description:   "D",
		ThumbnailURL:  "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:        "published",
		CreatorID:     "admin1",
		UpdaterID:     "admin1",
		PublishedAtMS: 5000,
		NowMS:         5000,
		Downloads: []Download{
			{Label: "1080p", MagnetLink: dlLink("magnet:?xt=one"), TorrentLink: dlLink("https://fans.example/one.torrent")},
			{Label: "2160p", MagnetLink: dlLink("magnet:?xt=two")},
		},
	})
	if err != nil {
		t.Fatalf("CreateBlogPost: %v", err)
	}

	// The read-back carries the rows in display order.
	if len(post.Downloads) != 2 || post.Downloads[0].Label != "1080p" || post.Downloads[1].Label != "2160p" {
		t.Fatalf("downloads = %+v, want 1080p then 2160p", post.Downloads)
	}
	if post.Downloads[1].TorrentLink != nil {
		t.Errorf("torrent slot = %v, want nil for the second row", post.Downloads[1].TorrentLink)
	}

	// The index row is keyed to the CONTENT row's rowid even when download
	// rows were inserted first; the fixture reaches search through the join.
	if n := ftsRowCount(t, db, "p1"); n != 1 {
		t.Errorf("fts rows = %d, want 1 (the content rowid keys the index, not the downloads)", n)
	}

	// Rows are stamped with the content row's created_at_ms, carry fresh
	// 32-hex ids, and positions equal the array index.
	rows, err := db.QueryContext(ctx,
		`SELECT id, position, created_at_ms, resolution, magnet_link, torrent_link
		 FROM blog_post_downloads WHERE blog_post_id = 'p1' ORDER BY position`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		var id, resolution string
		var position, createdMS int64
		var magnet, torrent sql.NullString
		if err := rows.Scan(&id, &position, &createdMS, &resolution, &magnet, &torrent); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		if len(id) != 32 {
			t.Errorf("row id = %q, want a fresh 32-hex value", id)
		}
		if createdMS != 5000 {
			t.Errorf("row created_at_ms = %d, want the content row's 5000", createdMS)
		}
		if position != int64(n) {
			t.Errorf("position = %d, want the array index %d", position, n)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate download rows: %v", err)
	}
	if n != 2 {
		t.Errorf("row count = %d, want 2", n)
	}
}

func TestUpdateBlogPost_ReplacesDownloads(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "p1", "published", 5000)
	for _, q := range []string{
		`INSERT INTO blog_post_downloads (id, blog_post_id, resolution, magnet_link, position, created_at_ms)
		 VALUES ('old1', 'p1', '1080p', 'magnet:?xt=old', 0, 5000)`,
		`INSERT INTO blog_post_downloads (id, blog_post_id, resolution, magnet_link, position, created_at_ms)
		 VALUES ('old2', 'p1', '2160p', 'magnet:?xt=old4k', 1, 5000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed old rows: %v", err)
		}
	}

	post, _, err := UpdateBlogPost(ctx, db, "p1", 1, UpdateBlogPostParams{
		Title:        "T",
		Description:  "D",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published",
		UpdaterID:    "admin1",
		NowMS:        9000, // the update clock — rows must NOT take this stamp
		Downloads: []Download{
			{Label: "720p", TorrentLink: dlLink("https://fans.example/new.torrent")},
		},
	})
	if err != nil {
		t.Fatalf("UpdateBlogPost: %v", err)
	}

	if len(post.Downloads) != 1 || post.Downloads[0].Label != "720p" {
		t.Fatalf("downloads = %+v, want the single replacement row", post.Downloads)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_downloads WHERE blog_post_id = 'p1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count = %d, want 1 after the replacement", n)
	}
	// The replacement row keeps the CONTENT row's created_at_ms (the import
	// precedent), not the update's now.
	var createdMS int64
	if err := db.QueryRow(`SELECT created_at_ms FROM blog_post_downloads WHERE blog_post_id = 'p1'`).Scan(&createdMS); err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	if createdMS != 5000 {
		t.Errorf("replacement created_at_ms = %d, want the content row's 5000", createdMS)
	}
}

func TestUpdateBlogPost_ConflictKeepsDownloads(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "p1", "published", 5000)
	if _, err := db.Exec(
		`INSERT INTO blog_post_downloads (id, blog_post_id, resolution, magnet_link, position, created_at_ms)
		 VALUES ('old1', 'p1', '1080p', 'magnet:?xt=old', 0, 5000)`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	_, _, err := UpdateBlogPost(ctx, db, "p1", 99, UpdateBlogPostParams{
		Title:        "T",
		Description:  "D",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published",
		UpdaterID:    "admin1",
		NowMS:        9000,
		Downloads:    []Download{{Label: "720p", MagnetLink: dlLink("magnet:?xt=new")}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}

	// The stale write touched NOTHING — the old row is intact.
	var got string
	if err := db.QueryRow(`SELECT resolution FROM blog_post_downloads WHERE blog_post_id = 'p1'`).Scan(&got); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if got != "1080p" {
		t.Errorf("resolution = %q, want the untouched 1080p row", got)
	}
}

func TestDeleteBlogPost_Cascades(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "p1", "published", 5000)
	for _, q := range []string{
		`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES ('user1', 'p1', 6000)`,
		`INSERT INTO blog_post_downloads (id, blog_post_id, resolution, position, created_at_ms)
		 VALUES ('dl1', 'p1', '1080p', 0, 6000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed cascade rows: %v", err)
		}
	}

	if err := DeleteBlogPost(ctx, db, "p1", 1); err != nil {
		t.Fatalf("DeleteBlogPost: %v", err)
	}

	for _, q := range []string{
		`SELECT COUNT(*) FROM blog_posts WHERE id = 'p1'`,
		`SELECT COUNT(*) FROM blog_post_favorites WHERE blog_post_id = 'p1'`,
		`SELECT COUNT(*) FROM blog_post_downloads WHERE blog_post_id = 'p1'`,
	} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("count after delete: %v", err)
		}
		if n != 0 {
			t.Errorf("q %s: count = %d, want 0 (hard-delete cascades)", q, n)
		}
	}
	if n := ftsRowCount(t, db, "p1"); n != 0 {
		t.Errorf("fts rows = %d, want 0 after delete", n)
	}

	if err := DeleteBlogPost(ctx, db, "p1", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestDeleteBlogPost_StaleRevisionConflicts(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)

	createPost(t, db, "p1", "published", 5000)
	if err := DeleteBlogPost(t.Context(), db, "p1", 42); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a stale revision", err)
	}
	// The row survived the conflict.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE id = 'p1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count = %d, want 1 after the conflicted delete", n)
	}
}

// TestStaffBlogPostDraftVisibility pins the draft-visibility gate matrix. The
// viewer weights come from identity.RoleWeight, so a model weight reorder
// that the SQL CASE does not mirror flips the creator-side comparison in
// this matrix (the canary). The CASE's ELSE arm for an unknown role is
// unreachable under the users.role CHECK; it stays defensive.
func TestStaffBlogPostDraftVisibility(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "draft1", "draft", 0)
	createPost(t, db, "pub1", "published", 5000)

	cases := []struct {
		name     string
		viewerID string
		role     string
		postID   string
		wantOK   bool
	}{
		{"creator sees own draft", "admin1", identity.RoleAdmin, "draft1", true},
		{"super-admin sees admin draft", "sa1", identity.RoleSuperAdmin, "draft1", true},
		{"other admin excluded", "admin2", identity.RoleAdmin, "draft1", false},
		{"moderator excluded", "mod1", identity.RoleModerator, "draft1", false},
		{"user excluded", "user1", identity.RoleUser, "draft1", false},
		{"published visible to moderator", "mod1", identity.RoleModerator, "pub1", true},
		{"unknown id masked", "sa1", identity.RoleSuperAdmin, "nope", false},
	}
	// The "other admin" case needs a second admin row.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "admin2", Username: "Admin2", Role: "admin",
	})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			post, err := GetStaffBlogPost(ctx, db, c.postID, c.viewerID, identity.RoleWeight(c.role))
			if c.wantOK {
				if err != nil {
					t.Fatalf("GetStaffBlogPost: %v (want the row)", err)
				}
				if post.ID != c.postID {
					t.Errorf("post id = %q, want %q", post.ID, c.postID)
				}
			} else if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want masked ErrNotFound", err)
			}
		})
	}
}

// TestStaffBlogPost_NullCreatorUpdater pins the migrated-row shape: the
// import never stamps creator/updater (NULL ids) and user deletion SETs
// them NULL, so the staff detail must read both without a Scan error — a
// NULL scan into a plain string fails with "converting NULL to string is
// unsupported".
func TestStaffBlogPost_NullCreatorUpdater(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	cases := []struct {
		name        string
		id          string
		status      string
		publishedMS int64
		creator     any
		updater     any
	}{
		{"published-null-both", "mig-pub", "published", 5000, nil, nil},
		// The draft passes the gate through the CASE ELSE branch: a NULL
		// creator has no role weight, so any staff role sees it.
		{"draft-null-both", "mig-draft", "draft", 0, nil, nil},
		// Mixed shape: a NULL creator but a stamped updater (the first
		// staff edit of a migrated row).
		{"published-null-creator", "mig-mixed", "published", 5000, nil, "admin1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := db.Exec(
				`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
					creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
				 VALUES (?, 'Migrated', '', '', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
				 	?, ?, ?, NULLIF(?, 0), 1000, 1000, 1)`, c.id, c.status, c.creator, c.updater, c.publishedMS); err != nil {
				t.Fatalf("seed migrated post: %v", err)
			}

			post, err := GetStaffBlogPost(ctx, db, c.id, "mod1", 1)
			if err != nil {
				t.Fatalf("GetStaffBlogPost(%s): %v (NULL ids must not fail the scan)", c.id, err)
			}
			if c.creator == nil && (post.CreatorID != "" || post.Creator != nil) {
				t.Errorf("post = %+v, want empty CreatorID and nil Creator for a NULL creator", post)
			}
			if c.updater == nil && (post.UpdaterID != "" || post.Updater != nil) {
				t.Errorf("post = %+v, want empty UpdaterID and nil Updater for a NULL updater", post)
			}
			if c.creator != nil && (post.CreatorID != "admin1" || post.Creator == nil) {
				t.Errorf("post = %+v, want the stamped creator", post)
			}
		})
	}
}

// TestUpdateBlogPost_MigratedRow pins the edit path on a NULL-creator
// migrated row: the staff pre-read (staffReadGate) AND the
// revision-guarded update must work.
func TestUpdateBlogPost_MigratedRow(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	if _, err := db.Exec(
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
		 VALUES ('mig1', 'Migrated', '', '', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
		 	'published', NULL, NULL, 5000, 1000, 1000, 1)`); err != nil {
		t.Fatalf("seed migrated post: %v", err)
	}

	// The pre-read a handler performs before the write (staffReadGate).
	if _, err := GetStaffBlogPost(ctx, db, "mig1", "mod1", 1); err != nil {
		t.Fatalf("staff pre-read: %v", err)
	}

	post, _, err := UpdateBlogPost(ctx, db, "mig1", 1, UpdateBlogPostParams{
		Title:        "Migrated",
		Subtitle:     "",
		Description:  "",
		ThumbnailURL: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
		Status:       "published",
		UpdaterID:    "mod1",
		NowMS:        9000,
	})
	if err != nil {
		t.Fatalf("UpdateBlogPost: %v", err)
	}
	if post.Revision != 2 || post.UpdaterID != "mod1" {
		t.Errorf("post = revision %d updater %q, want 2 / mod1", post.Revision, post.UpdaterID)
	}
}

// TestListStaffBlogPosts_Filters pins the two optional filters: status is an
// exact match, q is matched as words against the FOLDED title (every word
// must appear), and both compose with the visibility gate and with each
// other.
func TestListStaffBlogPosts_Filters(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	// Titles chosen so the fold matters: case, a Greek accent, and a final
	// sigma. Creator is admin1 (weight 2), so the super-admin sees every row.
	for _, p := range []struct {
		id, title, status string
	}{
		{"p1", "One Piece", "published"},
		{"p2", "Άμλετ", "draft"},
		{"p3", "Τέλος Οδυσσεύς", "archived"},
		{"p4", "one piece 2", "published"},
		{"p5", "Dr. Stone", "published"},
	} {
		_, _, err := CreateBlogPost(ctx, db, CreateBlogPostParams{
			ID:            p.id,
			Title:         p.title,
			Subtitle:      "",
			Description:   "",
			ThumbnailURL:  "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
			Status:        p.status,
			CreatorID:     "admin1",
			UpdaterID:     "admin1",
			PublishedAtMS: 5000,
			NowMS:         5000,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", p.id, err)
		}
	}

	for _, tc := range []struct {
		name   string
		filter StaffContentFilter
		want   []string
	}{
		{"no filter", StaffContentFilter{}, []string{"p1", "p2", "p3", "p4", "p5"}},
		{"status published", StaffContentFilter{Status: "published"}, []string{"p1", "p4", "p5"}},
		{"status archived", StaffContentFilter{Status: "archived"}, []string{"p3"}},
		{"status draft", StaffContentFilter{Status: "draft"}, []string{"p2"}},
		// Latin case folds, and the substring may sit anywhere in the title.
		{"case-insensitive latin", StaffContentFilter{Query: "PIECE"}, []string{"p1", "p4"}},
		{"mid-title substring", StaffContentFilter{Query: "iece"}, []string{"p1", "p4"}},
		// Greek: the needle is accent-free and uppercase, the titles are not.
		{"accent-free greek", StaffContentFilter{Query: "ΑΜΛΕΤ"}, []string{"p2"}},
		// Final sigma: the needle's ς must match the title's ς (both fold).
		{"final sigma", StaffContentFilter{Query: "ΟΔΥΣΣΕΎΣ"}, []string{"p3"}},
		{"both filters", StaffContentFilter{Status: "published", Query: "one piece"}, []string{"p1", "p4"}},
		{"filter matches nothing", StaffContentFilter{Status: "draft", Query: "piece"}, nil},
		// The query is split into words before folding: a space-joined
		// query reaches a punctuation-separated title, which is what the public
		// search has always done (its FTS5 query is cut the same way). Order does
		// not matter — the words are ANDed, not phrase-matched.
		{"word boundaries", StaffContentFilter{Query: "dr stone"}, []string{"p5"}},
		{"punctuation in the needle", StaffContentFilter{Query: "dr.stone"}, []string{"p5"}},
		{"order does not matter", StaffContentFilter{Query: "piece one"}, []string{"p1", "p4"}},
		{"greek word boundaries", StaffContentFilter{Query: "τελος οδυσσευσ"}, []string{"p3"}},
		// Every word must appear: a query whose second word is absent matches
		// nothing, even though its first word does.
		{"every word must match", StaffContentFilter{Query: "dr house"}, nil},
		// A query with no letters and no digits is NO filter, not an empty
		// substring test: the alternative is `instr(x, '')`, which matches every
		// row — a non-empty q would silently stop filtering. Punctuation-only
		// input therefore matches every row.
		{"punctuation only", StaffContentFilter{Query: "..."}, []string{"p1", "p2", "p3", "p4", "p5"}},
		{"needle strips to nothing", StaffContentFilter{Query: "\u0301"}, []string{"p1", "p2", "p3", "p4", "p5"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts, _, err := ListStaffBlogPosts(ctx, db, 10, nil, "sa1", 3, tc.filter)
			if err != nil {
				t.Fatalf("ListStaffBlogPosts: %v", err)
			}
			got := make([]string, 0, len(posts))
			for _, p := range posts {
				got = append(got, p.ID)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ids = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ids = %v, want %v", got, tc.want)
				}
			}
		})
	}

	// The gate still runs UNDER the filter: a moderator cannot filter their
	// way to another creator's draft even with a title that matches.
	posts, _, err := ListStaffBlogPosts(ctx, db, 10, nil, "mod1", 1, StaffContentFilter{Query: "άμλετ"})
	if err != nil {
		t.Fatalf("moderator filter: %v", err)
	}
	if len(posts) != 0 {
		t.Errorf("moderator saw %d drafts, want 0 (the gate runs with the filter)", len(posts))
	}

	// A filtered page keeps the keyset contract: limit 1 returns the first
	// match and hasNext when a second match exists.
	page1, hasNext, err := ListStaffBlogPosts(ctx, db, 1, nil, "sa1", 3, StaffContentFilter{Query: "piece"})
	if err != nil || !hasNext || len(page1) != 1 {
		t.Fatalf("filtered page1 = %+v hasNext %v err %v, want 1 row + next", page1, hasNext, err)
	}
	page2, _, err := ListStaffBlogPosts(ctx, db, 1,
		&AdminPageKey{UpdatedAtMS: page1[0].UpdatedAtMS, ID: page1[0].ID}, "sa1", 3,
		StaffContentFilter{Query: "piece"})
	if err != nil {
		t.Fatalf("filtered page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID == page1[0].ID {
		t.Fatalf("filtered page2 = %+v, want the next distinct match", page2)
	}
}

func TestListStaffBlogPosts_KeysetAndGate(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "a", "published", 1000)
	createPost(t, db, "b", "draft", 0)
	createPost(t, db, "c", "published", 3000)

	// The moderator sees only the published rows, newest-updated first
	// (all updated at 5000 — id ASC is the tie-breaker).
	posts, hasNext, err := ListStaffBlogPosts(ctx, db, 10, nil, "mod1", 1, StaffContentFilter{})
	if err != nil {
		t.Fatalf("ListStaffBlogPosts: %v", err)
	}
	if hasNext {
		t.Error("hasNext = true, want false for 2 rows")
	}
	if len(posts) != 2 || posts[0].ID != "a" || posts[1].ID != "c" {
		t.Fatalf("posts = %+v, want [a c] (published only, id ASC tie-breaker)", posts)
	}
	for _, p := range posts {
		if p.Status != "published" {
			t.Errorf("post %s status = %q, want published", p.ID, p.Status)
		}
	}

	// The super-admin sees all three (the draft's creator is an admin).
	posts, _, err = ListStaffBlogPosts(ctx, db, 10, nil, "sa1", 3, StaffContentFilter{})
	if err != nil {
		t.Fatalf("ListStaffBlogPosts: %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("super-admin sees %d rows, want 3", len(posts))
	}

	// Keyset: page size 1, second page starts after the first row.
	page1, hasNext, err := ListStaffBlogPosts(ctx, db, 1, nil, "sa1", 3, StaffContentFilter{})
	if err != nil || !hasNext || len(page1) != 1 {
		t.Fatalf("page1 = %+v hasNext %v err %v, want 1 row + next", page1, hasNext, err)
	}
	page2, _, err := ListStaffBlogPosts(ctx, db, 1,
		&AdminPageKey{UpdatedAtMS: page1[0].UpdatedAtMS, ID: page1[0].ID}, "sa1", 3, StaffContentFilter{})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID == page1[0].ID {
		t.Fatalf("page2 = %+v, want the next distinct row", page2)
	}
}

// TestCountStaffBlogPosts_GateAndFilter proves the count shares the staff
// list's gate and status filter: a row the gate hides is not counted, and
// the count equals the read under each filter.
func TestCountStaffBlogPosts_GateAndFilter(t *testing.T) {
	t.Parallel()
	db := setupBlogAdminStore(t)
	ctx := t.Context()

	createPost(t, db, "draft1", "draft", 0)      // creator admin1
	createPost(t, db, "pub1", "published", 5000) // creator admin1

	// The moderator's gate hides the admin's draft: 1, not 2.
	moderatorCount, err := CountStaffBlogPosts(ctx, db, "mod1", 1, StaffContentFilter{})
	if err != nil {
		t.Fatalf("CountStaffBlogPosts (moderator): %v", err)
	}
	if moderatorCount != 1 {
		t.Errorf("moderator count = %d, want 1 (the gate hides the admin draft)", moderatorCount)
	}

	// The super-admin passes the gate for both rows; the status filter then
	// narrows the count exactly like the read.
	for _, tc := range []struct {
		name   string
		status string
		want   int
	}{
		{"no filter", "", 2},
		{"published", "published", 1},
		{"draft", "draft", 1},
		{"archived", "archived", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := StaffContentFilter{Status: tc.status}
			count, err := CountStaffBlogPosts(ctx, db, "sa1", 3, filter)
			if err != nil {
				t.Fatalf("CountStaffBlogPosts (status %q): %v", tc.status, err)
			}
			if count != tc.want {
				t.Errorf("status %q: count = %d, want %d", tc.status, count, tc.want)
			}
			posts, _, err := ListStaffBlogPosts(ctx, db, 10, nil, "sa1", 3, filter)
			if err != nil {
				t.Fatalf("ListStaffBlogPosts (status %q): %v", tc.status, err)
			}
			if len(posts) != count {
				t.Errorf("status %q: list returned %d rows, count says %d — the mirror drifted", tc.status, len(posts), count)
			}
		})
	}

	// The gate still runs under the filter: the moderator counts no drafts.
	filtered, err := CountStaffBlogPosts(ctx, db, "mod1", 1, StaffContentFilter{Status: "draft"})
	if err != nil {
		t.Fatalf("CountStaffBlogPosts (moderator, draft): %v", err)
	}
	if filtered != 0 {
		t.Errorf("moderator draft count = %d, want 0 (gate runs under the filter)", filtered)
	}
}
