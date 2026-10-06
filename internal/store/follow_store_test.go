package store

import (
	"context"
	"errors"
	"testing"
)

// Follows store tests: the content_follows toggle behind the detail-page
// bell. Real SQLite + real migrations, same pattern as the favorites suite
// (the published-only gate, the composite-PK idempotency, and the masked
// outcomes are the contract the follow mirror reuses).

func TestAddBlogPostFollow_InsertAndIdempotent(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db) // reuses the same published/draft content fixtures
	ctx := context.Background()

	if err := AddFollow(ctx, db, BlogContent, "u1", "b1", 1_000); err != nil {
		t.Fatalf("add follow: %v", err)
	}
	// Repeat follow hits the composite PK — idempotent success.
	if err := AddFollow(ctx, db, BlogContent, "u1", "b1", 2_000); err != nil {
		t.Fatalf("repeat follow: %v", err)
	}

	published, following, err := FollowStatus(ctx, db, BlogContent, "u1", "b1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !published || !following {
		t.Errorf("status = (published=%v, following=%v), want (true, true)", published, following)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_follows`).Scan(&n); err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if n != 1 {
		t.Errorf("follow rows: got %d, want 1 (idempotent)", n)
	}
}

func TestAddBlogPostFollow_GatesUnpublished(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	ctx := context.Background()

	if err := AddFollow(ctx, db, BlogContent, "u1", "b3", 1_000); !errors.Is(err, ErrNotFound) {
		t.Errorf("follow draft: got %v, want ErrNotFound", err)
	}
	if err := AddFollow(ctx, db, BlogContent, "u1", "missing", 1_000); !errors.Is(err, ErrNotFound) {
		t.Errorf("follow unknown: got %v, want ErrNotFound", err)
	}
	if err := AddFollow(ctx, db, BlogContent, "ghost", "b1", 1_000); !errors.Is(err, ErrNotFound) {
		t.Errorf("follow by deleted user: got %v, want ErrNotFound (FK mapped)", err)
	}
}

func TestProjectFollow_StatusAndRemove(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	ctx := context.Background()

	// Not following yet — published + false.
	published, following, err := FollowStatus(ctx, db, ProjectContent, "u1", "p1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !published || following {
		t.Errorf("status = (published=%v, following=%v), want (true, false)", published, following)
	}

	if err := AddFollow(ctx, db, ProjectContent, "u1", "p1", 1_000); err != nil {
		t.Fatalf("add follow: %v", err)
	}
	if err := RemoveFollow(ctx, db, ProjectContent, "u1", "p1"); err != nil {
		t.Fatalf("remove follow: %v", err)
	}
	// Repeat remove is idempotent.
	if err := RemoveFollow(ctx, db, ProjectContent, "u1", "p1"); err != nil {
		t.Fatalf("repeat remove follow: %v", err)
	}

	_, following, err = FollowStatus(ctx, db, ProjectContent, "u1", "p1")
	if err != nil {
		t.Fatalf("status after remove: %v", err)
	}
	if following {
		t.Errorf("following after remove: got true, want false")
	}

	// The unpublished mask: a draft project reads as unpublished even
	// though a follow row can never exist for it.
	published, _, err = FollowStatus(ctx, db, ProjectContent, "u1", "p3")
	if err != nil {
		t.Fatalf("status draft: %v", err)
	}
	if published {
		t.Errorf("draft project status: got published=true, want false")
	}
}

// TestFollowRows_CascadeWithUser pins the user-deletion cascade: a deleted
// follower's rows die with them (content_follows references users ON DELETE
// CASCADE).
func TestFollowRows_CascadeWithUser(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	seedFavoriteFixtures(t, db)
	ctx := context.Background()

	if err := AddFollow(ctx, db, BlogContent, "u1", "b1", 1_000); err != nil {
		t.Fatalf("add follow: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_follows`).Scan(&n); err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if n != 0 {
		t.Errorf("follow rows after user delete: got %d, want 0 (cascade)", n)
	}
}
