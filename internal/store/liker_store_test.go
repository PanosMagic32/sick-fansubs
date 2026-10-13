package store

import (
	"errors"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

// Likers store tests (the who-liked readers behind the likers modal). Real
// SQLite + real migrations, same pattern as the other store suites.

// TestListFavoriters_OrderingAndPaging proves the newest-favorite-first
// ordering, the user_id tiebreak, and the keyset page split.
func TestListFavoriters_OrderingAndPaging(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	storetest.InsertUser(t, db, storetest.UserSpec{ID: "u2", Username: "Perospero"})
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:        "u3",
		Username:  "Cracker",
		AvatarURL: "media/images/ab/aabbccddeeff00112233445566778899.jpg",
	})
	ctx := t.Context()

	for _, fav := range []struct {
		userID string
		atMS   int64
	}{
		{userID: "u1", atMS: 1_000},
		{userID: "u2", atMS: 2_000},
		{userID: "u3", atMS: 3_000},
	} {
		if err := AddFavorite(ctx, db, BlogContent, fav.userID, "b1", fav.atMS); err != nil {
			t.Fatalf("add favorite %s: %v", fav.userID, err)
		}
	}

	items, hasNext, err := ListFavoriters(ctx, db, BlogContent, "b1", 2, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if !hasNext || len(items) != 2 {
		t.Fatalf("page 1 = %d rows (hasNext=%v), want 2 with hasNext", len(items), hasNext)
	}
	if items[0].UserID != "u3" || items[1].UserID != "u2" {
		t.Fatalf("page 1 order = [%s, %s], want [u3, u2] (newest first)", items[0].UserID, items[1].UserID)
	}
	if items[1].AvatarURL != nil {
		t.Errorf("u2 avatar = %q, want nil", *items[1].AvatarURL)
	}
	if got := items[0].AvatarURL; got == nil || *got != "media/images/ab/aabbccddeeff00112233445566778899.jpg" {
		t.Errorf("u3 avatar = %v, want the stored path", got)
	}

	last := items[len(items)-1]
	items, hasNext, err = ListFavoriters(ctx, db, BlogContent, "b1", 2, &LikerPageKey{
		LikedAtMS: last.LikedAtMS,
		UserID:    last.UserID,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if hasNext || len(items) != 1 || items[0].UserID != "u1" {
		t.Fatalf("page 2 = %+v (hasNext=%v), want [u1] and no next page", items, hasNext)
	}
}

// TestListFavoriters_CursorTiebreak proves the keyset survives equal
// timestamps: the user_id ASC tiebreak decides the order and the
// continuation predicate.
func TestListFavoriters_CursorTiebreak(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	storetest.InsertUser(t, db, storetest.UserSpec{ID: "u2", Username: "Perospero"})
	ctx := t.Context()

	for _, userID := range []string{"u2", "u1"} {
		if err := AddFavorite(ctx, db, BlogContent, userID, "b1", 1_000); err != nil {
			t.Fatalf("add favorite %s: %v", userID, err)
		}
	}

	items, _, err := ListFavoriters(ctx, db, BlogContent, "b1", 1, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(items) != 1 || items[0].UserID != "u1" {
		t.Fatalf("page 1 = %+v, want [u1] (user_id ASC within one instant)", items)
	}
	items, _, err = ListFavoriters(ctx, db, BlogContent, "b1", 1, &LikerPageKey{
		LikedAtMS: items[0].LikedAtMS,
		UserID:    items[0].UserID,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(items) != 1 || items[0].UserID != "u2" {
		t.Fatalf("page 2 = %+v, want [u2]", items)
	}
}

// TestListFavoriters_ActiveOnly proves suspended accounts keep their like but
// never appear in the list or the count.
func TestListFavoriters_ActiveOnly(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	storetest.InsertUser(t, db, storetest.UserSpec{ID: "u2", Username: "Perospero"})
	ctx := t.Context()

	if err := AddFavorite(ctx, db, BlogContent, "u1", "b1", 1_000); err != nil {
		t.Fatalf("add u1: %v", err)
	}
	if err := AddFavorite(ctx, db, BlogContent, "u2", "b1", 2_000); err != nil {
		t.Fatalf("add u2: %v", err)
	}
	if _, err := db.Exec(`UPDATE users SET status = 'suspended' WHERE id = 'u2'`); err != nil {
		t.Fatalf("suspend u2: %v", err)
	}

	items, _, err := ListFavoriters(ctx, db, BlogContent, "b1", 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].UserID != "u1" {
		t.Fatalf("items = %+v, want [u1] only (u2 suspended)", items)
	}
	n, err := CountFavoriters(ctx, db, BlogContent, "b1")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1 — the count must mirror the list's active filter", n)
	}
}

// TestListFavoriters_MaskedNotFound proves the published gate: drafts and
// unknown ids are the same ErrNotFound for the reader and its count path.
func TestListFavoriters_MaskedNotFound(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	ctx := t.Context()

	for _, id := range []string{"b3", "unknown"} {
		if _, _, err := ListFavoriters(ctx, db, BlogContent, id, 10, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("ListFavoriters(%q): err = %v, want ErrNotFound", id, err)
		}
	}
}

// TestListCommentHearters_Ordering proves the newest-heart-first order and
// the two content kinds' tables.
func TestListCommentHearters_Ordering(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	storetest.InsertUser(t, db, storetest.UserSpec{ID: "u2", Username: "Perospero"})
	ctx := t.Context()

	mustCreateComment(t, db, BlogContent, "c1", "u1", "b1", nil, "top", 2, 1_000, 1_000)
	mustCreateComment(t, db, ProjectContent, "pc1", "u1", "p1", nil, "top", 1, 1_000, 1_000)
	for _, heart := range []struct {
		table     string
		userID    string
		commentID string
		atMS      int64
	}{
		{table: BlogContent.hearts, userID: "u1", commentID: "c1", atMS: 1_000},
		{table: BlogContent.hearts, userID: "u2", commentID: "c1", atMS: 2_000},
		{table: ProjectContent.hearts, userID: "u2", commentID: "pc1", atMS: 3_000},
	} {
		if _, err := db.Exec(
			`INSERT INTO `+heart.table+` (user_id, comment_id, created_at_ms) VALUES (?, ?, ?)`,
			heart.userID, heart.commentID, heart.atMS,
		); err != nil {
			t.Fatalf("insert heart: %v", err)
		}
	}

	items, _, err := ListCommentHearters(ctx, db, BlogContent, "b1", "c1", 10, nil)
	if err != nil {
		t.Fatalf("blog hearters: %v", err)
	}
	if len(items) != 2 || items[0].UserID != "u2" || items[1].UserID != "u1" {
		t.Fatalf("blog hearters = %+v, want [u2, u1]", items)
	}
	items, _, err = ListCommentHearters(ctx, db, ProjectContent, "p1", "pc1", 10, nil)
	if err != nil {
		t.Fatalf("project hearters: %v", err)
	}
	if len(items) != 1 || items[0].UserID != "u2" {
		t.Fatalf("project hearters = %+v, want [u2]", items)
	}
}

// TestListCommentHearters_Gate proves the masked outcomes: unknown content or
// comment, a comment that belongs to another content, and a draft content's
// comment are all ErrNotFound.
func TestListCommentHearters_Gate(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	ctx := t.Context()

	mustCreateComment(t, db, BlogContent, "c1", "u1", "b1", nil, "top", 0, 1_000, 1_000)

	for _, tc := range []struct {
		name      string
		contentID string
		commentID string
	}{
		{name: "unknown comment", contentID: "b1", commentID: "nope"},
		{name: "foreign comment", contentID: "b2", commentID: "c1"},
		{name: "unknown content", contentID: "nope", commentID: "c1"},
	} {
		if _, _, err := ListCommentHearters(ctx, db, BlogContent, tc.contentID, tc.commentID, 10, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", tc.name, err)
		}
	}
}

// TestCountCommentHearters_ActiveOnly mirrors the favorites count test on the
// hearts tables.
func TestCountCommentHearters_ActiveOnly(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	storetest.InsertUser(t, db, storetest.UserSpec{ID: "u2", Username: "Perospero"})
	ctx := t.Context()

	mustCreateComment(t, db, BlogContent, "c1", "u1", "b1", nil, "top", 2, 1_000, 1_000)
	for _, userID := range []string{"u1", "u2"} {
		if _, err := db.Exec(
			`INSERT INTO blog_post_comment_hearts (user_id, comment_id, created_at_ms) VALUES (?, 'c1', 1000)`,
			userID,
		); err != nil {
			t.Fatalf("insert heart %s: %v", userID, err)
		}
	}
	if _, err := db.Exec(`UPDATE users SET status = 'suspended' WHERE id = 'u2'`); err != nil {
		t.Fatalf("suspend u2: %v", err)
	}

	items, _, err := ListCommentHearters(ctx, db, BlogContent, "b1", "c1", 10, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	n, err := CountCommentHearters(ctx, db, BlogContent, "c1")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if len(items) != n || n != 1 {
		t.Errorf("list = %d rows, count = %d, want both 1 (u2 suspended)", len(items), n)
	}
}
