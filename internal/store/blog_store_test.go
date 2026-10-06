package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

// Blog detail.

// mustCreateDownload inserts one blog_post_downloads row.
func mustCreateDownload(t *testing.T, db *sql.DB, postID, resolution string, position int, magnet, torrent *string) {
	t.Helper()

	const q = `INSERT INTO blog_post_downloads
		(id, blog_post_id, resolution, magnet_link, torrent_link, position, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, 1)`
	id := fmt.Sprintf("d-%s-%d", resolution, position)
	if _, err := db.Exec(q, id, postID, resolution, magnet, torrent, position); err != nil {
		t.Fatalf("insert download %q: %v", id, err)
	}
}

// TestGetPublishedBlogPost_Found proves the detail lookup: the full row with
// the creator/updater joins and the download rows ordered by position.
func TestGetPublishedBlogPost_Found(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "creator", "creator", "creator@example.com")
	if _, err := db.Exec(`UPDATE users SET avatar_url = 'https://example.com/c.png' WHERE id = 'u1'`); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	mustCreateUser(t, db, "u2", "editor", "editor", "editor@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE blog_posts
		SET creator_id = 'u1', updater_id = 'u2', description = 'desc', updated_at_ms = 3100
		WHERE id = 'p1'`); err != nil {
		t.Fatalf("set creator/updater/description: %v", err)
	}

	mag := "magnet:?xt=urn:btih:aaa"
	tor := "https://example.com/t.torrent"
	mag4k := "magnet:?xt=urn:btih:bbb"
	// Inserted deliberately OUT of display order (2160p/position 1 first):
	// the lookup must order by position, not by insertion order.
	mustCreateDownload(t, db, "p1", "2160p", 1, &mag4k, nil)
	mustCreateDownload(t, db, "p1", "1080p", 0, &mag, &tor)

	post, downloads, err := GetPublishedBlogPost(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if post.ID != "p1" || post.Title != "T" || post.Description != "desc" || post.PublishedAtMS != 3000 {
		t.Errorf("post = %+v", post)
	}
	if post.UpdatedAtMS != 3100 {
		t.Errorf("updatedAtMS = %d, want 3100", post.UpdatedAtMS)
	}
	if post.Creator == nil || post.Creator.ID != "u1" || post.Creator.Username != "creator" ||
		post.Creator.AvatarURL == nil || *post.Creator.AvatarURL != "https://example.com/c.png" {
		t.Errorf("creator = %+v, want u1/creator with avatar", post.Creator)
	}
	if post.Updater == nil || post.Updater.ID != "u2" || post.Updater.Username != "editor" ||
		post.Updater.AvatarURL != nil {
		t.Errorf("updater = %+v, want u2/editor without avatar", post.Updater)
	}

	if len(downloads) != 2 {
		t.Fatalf("downloads = %d rows, want 2", len(downloads))
	}
	if downloads[0].Label != "1080p" || downloads[0].MagnetLink == nil || *downloads[0].MagnetLink != mag ||
		downloads[0].TorrentLink == nil || *downloads[0].TorrentLink != tor {
		t.Errorf("download[0] = %+v", downloads[0])
	}
	if downloads[1].Label != "2160p" || downloads[1].MagnetLink == nil || downloads[1].TorrentLink != nil {
		t.Errorf("download[1] = %+v, want 2160p with NULL torrent", downloads[1])
	}
}

// TestGetPublishedBlogPost_CreatorNull proves the nullable projection: a post
// whose creator_id/updater_id are NULL yields nil refs, not fake users.
func TestGetPublishedBlogPost_CreatorNull(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")

	post, _, err := GetPublishedBlogPost(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if post.Creator != nil {
		t.Errorf("creator = %+v, want nil (NULL creator_id)", post.Creator)
	}
	if post.Updater != nil {
		t.Errorf("updater = %+v, want nil (NULL updater_id)", post.Updater)
	}
}

// TestGetPublishedBlogPost_UpdaterOnly proves the realistic post-migration
// mixed shape: the legacy export can carry updatedBy with an orphaned or
// absent creator, so creator_id stays NULL while updater_id resolves. The
// two joins must stay independent.
func TestGetPublishedBlogPost_UpdaterOnly(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u2", "editor", "editor", "editor@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE blog_posts SET updater_id = 'u2' WHERE id = 'p1'`); err != nil {
		t.Fatalf("set updater: %v", err)
	}

	post, _, err := GetPublishedBlogPost(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if post.Creator != nil {
		t.Errorf("creator = %+v, want nil (NULL creator_id)", post.Creator)
	}
	if post.Updater == nil || post.Updater.ID != "u2" {
		t.Errorf("updater = %+v, want u2", post.Updater)
	}
}

// TestGetPublishedBlogPost_UserRefsDeleted proves the LEFT JOINs survive user
// deletion: both foreign keys are ON DELETE SET NULL, so a post whose
// creator/updater were deleted yields nil refs — same projection as no
// reference at all.
func TestGetPublishedBlogPost_UserRefsDeleted(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "creator", "creator", "creator@example.com")
	mustCreateUser(t, db, "u2", "editor", "editor", "editor@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE blog_posts SET creator_id = 'u1', updater_id = 'u2' WHERE id = 'p1'`); err != nil {
		t.Fatalf("set refs: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE id IN ('u1', 'u2')`); err != nil {
		t.Fatalf("delete users: %v", err)
	}

	post, _, err := GetPublishedBlogPost(ctx, db, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if post.Creator != nil {
		t.Errorf("creator = %+v, want nil (creator user deleted)", post.Creator)
	}
	if post.Updater != nil {
		t.Errorf("updater = %+v, want nil (updater user deleted)", post.Updater)
	}
}

// TestGetPublishedBlogPost_NotFoundAndMasked proves deliberate masking:
// unknown ids AND non-public rows (draft, archived,
// NULL published_at_ms) all answer ErrNotFound — the caller cannot tell
// which ids exist.
func TestGetPublishedBlogPost_NotFoundAndMasked(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateBlogPost(t, db, BlogPostSummary{ID: "draft", Title: "D", ThumbnailURL: "https://example.com/d.jpg", PublishedAtMS: 9000}, "draft")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "archived", Title: "A", ThumbnailURL: "https://example.com/a.jpg", PublishedAtMS: 8000}, "archived")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "null-published", Title: "N", ThumbnailURL: "https://example.com/n.jpg"}, "published")

	for _, id := range []string{"unknown", "draft", "archived", "null-published"} {
		_, _, err := GetPublishedBlogPost(ctx, db, id)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("id %q: err = %v, want ErrNotFound", id, err)
		}
	}
}

// TestListPublishedBlogPosts_CreatorJoin proves the list fields:
// rows carry description, updated_at_ms, and the creator ref with the
// avatar (nullable), while posts without a creator stay nil.
func TestListPublishedBlogPosts_CreatorJoin(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "creator", "creator", "creator@example.com")
	if _, err := db.Exec(`UPDATE users SET avatar_url = 'https://example.com/a.png' WHERE id = 'u1'`); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "with", Title: "W", ThumbnailURL: "https://example.com/w.jpg", PublishedAtMS: 3000}, "published")
	if _, err := db.Exec(`UPDATE blog_posts
		SET creator_id = 'u1', description = 'desc', updated_at_ms = 3100
		WHERE id = 'with'`); err != nil {
		t.Fatalf("set creator/description: %v", err)
	}
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "without", Title: "N", ThumbnailURL: "https://example.com/n.jpg", PublishedAtMS: 2000}, "published")

	posts, _, err := ListPublishedBlogPosts(ctx, db, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("got %d rows, want 2", len(posts))
	}

	first := posts[0]
	if first.ID != "with" || first.Description != "desc" || first.UpdatedAtMS != 3100 {
		t.Errorf("first = %+v, want with/desc/3100", first)
	}
	if first.Creator == nil || first.Creator.ID != "u1" || first.Creator.Username != "creator" ||
		first.Creator.AvatarURL == nil || *first.Creator.AvatarURL != "https://example.com/a.png" {
		t.Errorf("first.Creator = %+v, want u1/creator with avatar", first.Creator)
	}

	second := posts[1]
	if second.Creator != nil {
		t.Errorf("second.Creator = %+v, want nil (no creator_id)", second.Creator)
	}
}

// TestListPublishedBlogPosts_NewestFirst proves the accepted ordering:
// published_at_ms DESC with id ASC as the tie-breaker.
func TestListPublishedBlogPosts_NewestFirst(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateBlogPost(t, db, BlogPostSummary{ID: "old", Title: "Old", ThumbnailURL: "https://example.com/o.jpg", PublishedAtMS: 1000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "new", Title: "New", ThumbnailURL: "https://example.com/n.jpg", PublishedAtMS: 3000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "mid-b", Title: "Mid B", ThumbnailURL: "https://example.com/mb.jpg", PublishedAtMS: 2000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "mid-a", Title: "Mid A", ThumbnailURL: "https://example.com/ma.jpg", PublishedAtMS: 2000}, "published")

	posts, hasNext, err := ListPublishedBlogPosts(ctx, db, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext {
		t.Error("hasNext = true, want false (4 rows < limit 10)")
	}

	var got []string
	for _, p := range posts {
		got = append(got, p.ID)
	}
	want := []string{"new", "mid-a", "mid-b", "old"}
	if len(got) != len(want) {
		t.Fatalf("got %d rows %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (order %v vs %v)", i, got[i], want[i], got, want)
		}
	}
}

// TestListPublishedBlogPosts_ExcludesNonPublicRows proves the public filter:
// drafts and rows without a publish time never appear, regardless of their
// position in the ordering.
func TestListPublishedBlogPosts_ExcludesNonPublicRows(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateBlogPost(t, db, BlogPostSummary{ID: "published", Title: "P", ThumbnailURL: "https://example.com/p.jpg", PublishedAtMS: 1000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "draft", Title: "D", ThumbnailURL: "https://example.com/d.jpg", PublishedAtMS: 9000}, "draft")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "archived", Title: "A", ThumbnailURL: "https://example.com/a.jpg", PublishedAtMS: 8000}, "archived")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "published-null", Title: "N", ThumbnailURL: "https://example.com/n.jpg"}, "published")

	posts, hasNext, err := ListPublishedBlogPosts(ctx, db, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext {
		t.Error("hasNext = true, want false")
	}
	if len(posts) != 1 || posts[0].ID != "published" {
		t.Errorf("got %v, want exactly [published]", posts)
	}
}

// TestCountPublishedContent_ExcludesNonPublicRows proves the public count
// applies the same visibility filter as the list read it mirrors: only the
// published, stamped row is counted, and the count equals the page walk.
func TestCountPublishedContent_ExcludesNonPublicRows(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateBlogPost(t, db, BlogPostSummary{ID: "published", Title: "P", ThumbnailURL: "https://example.com/p.jpg", PublishedAtMS: 1000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "draft", Title: "D", ThumbnailURL: "https://example.com/d.jpg", PublishedAtMS: 9000}, "draft")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "archived", Title: "A", ThumbnailURL: "https://example.com/a.jpg", PublishedAtMS: 8000}, "archived")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "published-null", Title: "N", ThumbnailURL: "https://example.com/n.jpg"}, "published")

	n, err := CountPublishedContent(ctx, db, BlogContent)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1 (the single published, stamped row)", n)
	}

	posts, _, err := ListPublishedBlogPosts(ctx, db, 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != n {
		t.Errorf("list returned %d rows, count says %d — the mirror drifted", len(posts), n)
	}
}

// TestListPublishedBlogPosts_KeysetPagesCoverExactlyOnce proves that
// consecutive pages formed from the returned rows cover the full ordering
// with no duplicates and no omissions, exercising both the
// published_at_ms < ? branch and the published_at_ms = ? AND id > ? branch.
func TestListPublishedBlogPosts_KeysetPagesCoverExactlyOnce(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	// Full ordering: p5(5000), p4(4000), p3a(3000), p3b(3000), p2(2000).
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p2", Title: "T", ThumbnailURL: "https://example.com/2.jpg", PublishedAtMS: 2000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p3b", Title: "T", ThumbnailURL: "https://example.com/3b.jpg", PublishedAtMS: 3000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p3a", Title: "T", ThumbnailURL: "https://example.com/3a.jpg", PublishedAtMS: 3000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p4", Title: "T", ThumbnailURL: "https://example.com/4.jpg", PublishedAtMS: 4000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p5", Title: "T", ThumbnailURL: "https://example.com/5.jpg", PublishedAtMS: 5000}, "published")

	page1, hasNext, err := ListPublishedBlogPosts(ctx, db, 2, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if ids(page1) != "p5,p4" {
		t.Errorf("page 1: got %v, want [p5 p4]", ids(page1))
	}
	if !hasNext {
		t.Error("page 1 hasNext = false, want true")
	}

	// Page 2 continues strictly after (4000, p4): the next publish time down,
	// which is the tied pair at 3000.
	key1 := &PageKey{PublishedAtMS: page1[1].PublishedAtMS, ID: page1[1].ID}
	page2, hasNext, err := ListPublishedBlogPosts(ctx, db, 2, key1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if ids(page2) != "p3a,p3b" {
		t.Errorf("page 2: got %v, want [p3a p3b]", ids(page2))
	}
	if !hasNext {
		t.Error("page 2 hasNext = false, want true")
	}

	// Page 3 continues strictly after (3000, p3b): only the older post remains.
	key2 := &PageKey{PublishedAtMS: page2[1].PublishedAtMS, ID: page2[1].ID}
	page3, hasNext, err := ListPublishedBlogPosts(ctx, db, 2, key2)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if ids(page3) != "p2" {
		t.Errorf("page 3: got %v, want [p2]", ids(page3))
	}
	if hasNext {
		t.Error("page 3 hasNext = true, want false (final page)")
	}
}

// TestListPublishedBlogPosts_KeysetStableAcrossInserts proves the reason the
// contract uses keyset pagination: a row published between two page requests
// prepends the ordering and does NOT shift later pages (offset paging would
// repeat a row on page 2).
func TestListPublishedBlogPosts_KeysetStableAcrossInserts(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateBlogPost(t, db, BlogPostSummary{ID: "a", Title: "T", ThumbnailURL: "https://example.com/a.jpg", PublishedAtMS: 3000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "b", Title: "T", ThumbnailURL: "https://example.com/b.jpg", PublishedAtMS: 2000}, "published")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "c", Title: "T", ThumbnailURL: "https://example.com/c.jpg", PublishedAtMS: 1000}, "published")

	page1, _, err := ListPublishedBlogPosts(ctx, db, 1, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}

	// A newer post appears after the first page was read.
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "newest", Title: "T", ThumbnailURL: "https://example.com/n.jpg", PublishedAtMS: 4000}, "published")

	key := &PageKey{PublishedAtMS: page1[0].PublishedAtMS, ID: page1[0].ID}
	page2, _, err := ListPublishedBlogPosts(ctx, db, 1, key)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if ids(page2) != "b" {
		t.Errorf("page 2: got %v, want [b] (insert must not shift the cursor page)", ids(page2))
	}
}

// TestListPublishedBlogPosts_EmptyTable proves the empty-collection outcome:
// an empty items slice and no next page.
func TestListPublishedBlogPosts_EmptyTable(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	posts, hasNext, err := ListPublishedBlogPosts(ctx, db, 20, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 0 {
		t.Errorf("got %v, want no posts", posts)
	}
	if hasNext {
		t.Error("hasNext = true, want false")
	}
}

// TestBlogPostIndicatorCounts pins the live-derived indicator counts
// on BOTH projections — the list summary and the detail
// lookup: correlated COUNT subqueries over blog_post_comments (top-level +
// replies count once each) and blog_post_favorites, with no materialized
// columns anywhere.
func TestBlogPostIndicatorCounts(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")
	mustCreateBlogPost(t, db, BlogPostSummary{ID: "p1", Title: "T", ThumbnailURL: "https://example.com/t.jpg", PublishedAtMS: 3000}, "published")
	mustCreateComment(t, db, BlogContent, "c1", "u1", "p1", nil, "top", 0, 1000, 1000)
	mustCreateComment(t, db, BlogContent, "r1", "u2", "p1", new("c1"), "reply", 0, 2000, 2000)
	if err := AddFavorite(ctx, db, BlogContent, "u1", "p1", 5000); err != nil {
		t.Fatalf("add favorite u1: %v", err)
	}
	if err := AddFavorite(ctx, db, BlogContent, "u2", "p1", 6000); err != nil {
		t.Fatalf("add favorite u2: %v", err)
	}

	posts, hasNext, err := ListPublishedBlogPosts(ctx, db, 20, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if hasNext || len(posts) != 1 {
		t.Fatalf("list = %d items (hasNext=%v), want exactly 1", len(posts), hasNext)
	}
	if posts[0].CommentCount != 2 || posts[0].FavoriteCount != 2 {
		t.Errorf("list counts = (%d comments, %d favorites), want (2, 2)",
			posts[0].CommentCount, posts[0].FavoriteCount)
	}

	post, _, err := GetPublishedBlogPost(ctx, db, "p1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if post.CommentCount != 2 || post.FavoriteCount != 2 {
		t.Errorf("detail counts = (%d comments, %d favorites), want (2, 2)",
			post.CommentCount, post.FavoriteCount)
	}
}

// ids renders the IDs of a page for compact failure messages.
func ids(posts []BlogPostSummary) string {
	if len(posts) == 0 {
		return "(empty)"
	}
	out := posts[0].ID
	for _, p := range posts[1:] {
		out += "," + p.ID
	}
	return out
}
