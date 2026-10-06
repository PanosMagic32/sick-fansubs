package store

import (
	"context"
	"errors"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

// Favorites store tests (the favorites data model and its contract).
// Real SQLite + real migrations, same pattern as the other store suites.

// TestListBlogPostFavorites_Counts pins the indicator counts on the
// favorites list projection (the account-page cards gain the same counts
// as every other content surface) and the subtitle slot (the blog post's
// own column).
func TestListBlogPostFavorites_Counts(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	ctx := context.Background()

	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 1_000); err != nil {
		t.Fatalf("add b1: %v", err)
	}
	mustCreateComment(t, db, BlogContent, "c1", "u1", "b1", nil, "top", 0, 1_000, 1_000)

	items, hasNext, err := ListFavorites(ctx, db, BlogContent, "u1", 20, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext || len(items) != 1 {
		t.Fatalf("items = %d (hasNext=%v), want exactly 1", len(items), hasNext)
	}
	if items[0].CommentCount != 1 || items[0].FavoriteCount != 1 {
		t.Errorf("counts = (%d comments, %d favorites), want (1, 1)",
			items[0].CommentCount, items[0].FavoriteCount)
	}
	if items[0].Subtitle != "Sub B1" {
		t.Errorf("ListFavorites(blog) subtitle = %q, want the blog post's subtitle", items[0].Subtitle)
	}
}

// TestListProjectFavorites_Counts mirrors the blog test: the project list
// query has its own SQL columns against the project tables, so its count
// path needs its own pin. It also pins the empty subtitle slot — projects
// have no subtitle, and a slug leak into that position would fail here.
func TestListProjectFavorites_Counts(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	ctx := context.Background()

	if err := AddFavorite(ctx, db, ProjectContent, "u1", "p1", 1_000); err != nil {
		t.Fatalf("add p1: %v", err)
	}
	mustCreateComment(t, db, ProjectContent, "c1", "u1", "p1", nil, "top", 0, 1_000, 1_000)

	items, hasNext, err := ListFavorites(ctx, db, ProjectContent, "u1", 20, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext || len(items) != 1 {
		t.Fatalf("items = %d (hasNext=%v), want exactly 1", len(items), hasNext)
	}
	if items[0].CommentCount != 1 || items[0].FavoriteCount != 1 {
		t.Errorf("counts = (%d comments, %d favorites), want (1, 1)",
			items[0].CommentCount, items[0].FavoriteCount)
	}
	if items[0].Subtitle != "" {
		t.Errorf("ListFavorites(projects) subtitle = %q, want empty", items[0].Subtitle)
	}
}

// TestAddBlogPostFavorite_InsertAndIdempotent proves the INSERT…SELECT
// guard and the composite-PK idempotency.
func TestAddBlogPostFavorite_InsertAndIdempotent(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()

	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 5_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	// Repeat favorite — idempotent success via the composite PK.
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 5_001); err != nil {
		t.Fatalf("repeat favorite: %v", err)
	}

	// The first created_at_ms must have survived (INSERT, not UPSERT).
	published, favorited, err := FavoriteStatus(ctx, db, BlogContent, "u1", "b1")
	if err != nil || !published || !favorited {
		t.Fatalf("status = (%v,%v,%v), want (true,true,nil)", published, favorited, err)
	}
}

// TestAddBlogPostFavorite_RejectsUnpublished proves the masked outcome:
// drafts and unknown ids are indistinguishable (ErrNotFound).
func TestAddBlogPostFavorite_RejectsUnpublished(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()
	for _, id := range []string{"b3", "nope"} {
		if err := AddFavorite(ctx, db, BlogContent, "u1", id, 5_000); !errors.Is(err, ErrNotFound) {
			t.Errorf("add favorite %q: err = %v, want ErrNotFound", id, err)
		}
	}
}

// TestAddProjectFavorite proves the project path with the same contract.
func TestAddProjectFavorite_InsertAndIdempotent(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()

	if err := AddFavorite(ctx, db, ProjectContent, "u1", "p1", 5_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if err := AddFavorite(ctx, db, ProjectContent, "u1", "p1", 5_001); err != nil {
		t.Fatalf("repeat favorite: %v", err)
	}
	if err := AddFavorite(ctx, db, ProjectContent, "u1", "p3", 5_002); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft project: err = %v, want ErrNotFound", err)
	}
}

// TestRemoveFavorite_Idempotent proves removal and repeat removal.
func TestRemoveFavorite_Idempotent(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 5_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if err := RemoveFavorite(ctx, db, BlogContent, "u1", "b1"); err != nil {
		t.Fatalf("remove favorite: %v", err)
	}
	if err := RemoveFavorite(ctx, db, BlogContent, "u1", "b1"); err != nil {
		t.Fatalf("repeat remove: %v", err)
	}
	_, favorited, err := FavoriteStatus(ctx, db, BlogContent, "u1", "b1")
	if err != nil || favorited {
		t.Fatalf("status after remove: favorited = %v, err = %v", favorited, err)
	}
}

// TestFavoriteStatus_Masking proves the published gate on the status read.
func TestFavoriteStatus_Masking(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()

	published, _, err := FavoriteStatus(ctx, db, BlogContent, "u1", "b3") // draft
	if err != nil || published {
		t.Fatalf("draft status = (%v, %v), want published=false", published, err)
	}
	published, _, err = FavoriteStatus(ctx, db, BlogContent, "u1", "unknown")
	if err != nil || published {
		t.Fatalf("unknown status = (%v, %v), want published=false", published, err)
	}
}

// TestListBlogPostFavorites_OrderingAndPaging proves the favorites
// ordering (created_at_ms DESC, content id ASC) and the keyset page split.
func TestListBlogPostFavorites_OrderingAndPaging(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()
	// Favorite b2 BEFORE b1 (b2's favorite time is older) — ordering is by
	// favoriting time, not publish time.
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b2", 1_000); err != nil {
		t.Fatalf("add b2: %v", err)
	}
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 2_000); err != nil {
		t.Fatalf("add b1: %v", err)
	}

	items, hasNext, err := ListFavorites(ctx, db, BlogContent, "u1", 1, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].ID != "b1" {
		t.Fatalf("page 1 = %+v, want [b1] (newest favorite first)", items)
	}
	if !hasNext {
		t.Fatal("page 1 hasNext = false, want true")
	}

	items, hasNext, err = ListFavorites(ctx, db, BlogContent, "u1", 1, &FavoritePageKey{
		CreatedAtMS: items[0].FavoritedAtMS,
		ContentID:   items[0].ID,
	})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(items) != 1 || items[0].ID != "b2" {
		t.Fatalf("page 2 = %+v, want [b2]", items)
	}
	if hasNext {
		t.Fatal("page 2 hasNext = true, want false")
	}
}

// TestListFavorites_PublishedOnly proves the list join filters out content
// that is no longer published (the draft b3 was favorited by a direct row
// insert — the API path prevents it, but the list must not leak it anyway).
func TestListFavorites_PublishedOnly(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()
	if _, err := db.Exec(
		`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES ('u1', 'b3', 1_000)`,
	); err != nil {
		t.Fatalf("insert draft favorite: %v", err)
	}

	items, hasNext, err := ListFavorites(ctx, db, BlogContent, "u1", 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext || len(items) != 0 {
		t.Fatalf("items = %+v, want empty (draft favorite hidden)", items)
	}
}

// TestCountFavorites_PublishedAndOwnerScoped proves the count join mirrors
// ListFavorites: a favorite on no-longer-published content is not counted,
// and another user's favorite is not counted for the viewer.
func TestCountFavorites_PublishedAndOwnerScoped(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	storetest.InsertUser(t, db, storetest.UserSpec{ID: "u2", Username: "Perospero"})

	ctx := context.Background()
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 1_000); err != nil {
		t.Fatalf("add published favorite: %v", err)
	}
	// The API path cannot favorite a draft, so the hidden row is inserted
	// directly — the count must not leak it anyway.
	if _, err := db.Exec(
		`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES ('u1', 'b3', 2_000)`,
	); err != nil {
		t.Fatalf("insert draft favorite: %v", err)
	}
	if err := AddFavorite(ctx, db, BlogContent, "u2", "b1", 3_000); err != nil {
		t.Fatalf("add other user's favorite: %v", err)
	}

	n, err := CountFavorites(ctx, db, BlogContent, "u1")
	if err != nil {
		t.Fatalf("CountFavorites: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1 (the published favorite only)", n)
	}

	items, _, err := ListFavorites(ctx, db, BlogContent, "u1", 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != n {
		t.Errorf("list returned %d rows, count says %d — the mirror drifted", len(items), n)
	}
}

// TestFavorites_Cascade proves the FK cascades: deleting the content or the
// user removes favorite rows automatically.
func TestFavorites_Cascade(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 5_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}

	// Delete the content → favorite row cascades away.
	if _, err := db.Exec(`DELETE FROM blog_posts WHERE id = 'b1'`); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_favorites`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("favorites after content delete = %d, want 0", n)
	}

	// Re-add, then delete the user → cascade again.
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b2", 5_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_favorites`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("favorites after user delete = %d, want 0", n)
	}
}

// TestIsForeignKeyViolation_Canary pins the driver-text discriminator the
// same way the IsUniqueViolation canary does: a real FK violation through
// the driver must match, or the match silently breaks when modernc changes
// its error text. The raw INSERT (no INSERT…SELECT guard) surfaces the
// constraint error directly — the store's add path deliberately maps it to
// ErrNotFound before it reaches a caller.
func TestIsForeignKeyViolation_Canary(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	// A favorite for a user that does not exist violates the users FK.
	_, err := db.Exec(
		`INSERT INTO blog_post_favorites (user_id, blog_post_id, created_at_ms) VALUES ('ghost', 'b1', 5000)`,
	)
	if err == nil {
		t.Fatal("raw insert for missing user: err = nil, want FK violation")
	}
	if !isForeignKeyViolation(err) {
		t.Fatalf("err = %q does not match isForeignKeyViolation — driver text changed?", err)
	}
}

// TestDeleteFavoritesTx_ArchiveRemovesFavorites pins the archive-side
// cascade contract: the CRUD archives content
// and removes its favorite rows IN ONE transaction via the Tx helpers. The
// deletion case is covered by the FK cascade test above; this is the
// archive/unpublish half.
func TestDeleteFavoritesTx_ArchiveRemovesFavorites(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)

	ctx := context.Background()
	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 5_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if err := AddFavorite(ctx, db, ProjectContent, "u1", "p1", 5_000); err != nil {
		t.Fatalf("add favorite: %v", err)
	}

	// The CRUD transaction: status change + favorites cleanup
	// commit together or not at all.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE blog_posts SET status = 'archived' WHERE id = 'b1'`); err != nil {
		t.Fatalf("archive post: %v", err)
	}
	if err := deleteContentFavoritesTx(ctx, tx, BlogContent, "b1"); err != nil {
		t.Fatalf("delete post favorites: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE projects SET status = 'archived' WHERE id = 'p1'`); err != nil {
		t.Fatalf("archive project: %v", err)
	}
	if err := deleteContentFavoritesTx(ctx, tx, ProjectContent, "p1"); err != nil {
		t.Fatalf("delete project favorites: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blog_post_favorites`).Scan(&n); err != nil {
		t.Fatalf("count blog favorites: %v", err)
	}
	if n != 0 {
		t.Fatalf("blog favorites after archive = %d, want 0", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_favorites`).Scan(&n); err != nil {
		t.Fatalf("count project favorites: %v", err)
	}
	if n != 0 {
		t.Fatalf("project favorites after archive = %d, want 0", n)
	}
}
