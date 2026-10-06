package store

import (
	"context"
	"database/sql"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

func insertBlogThumb(t *testing.T, db *sql.DB, id, thumb string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', '', '', ?, 'published', 1000, 500, 500)`, id, thumb); err != nil {
		t.Fatalf("insert blog post: %v", err)
	}
}

func insertProjectThumb(t *testing.T, db *sql.DB, id, slug, thumb string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', '', ?, ?, 'published', 1000, 500, 500)`, id, slug, thumb); err != nil {
		t.Fatalf("insert project: %v", err)
	}
}

// insertUserAvatar seeds one user row through the shared fixture; a nil
// avatar leaves avatar_url NULL.
func insertUserAvatar(t *testing.T, db *sql.DB, id string, avatar *string) {
	t.Helper()
	spec := storetest.UserSpec{
		ID:          id,
		Username:    "u" + id,
		Email:       id + "@example.com",
		CreatedAtMS: 500,
	}
	if avatar != nil {
		spec.AvatarURL = *avatar
	}
	storetest.InsertUser(t, db, spec)
}

func TestReferencedMediaPaths_SpansBothTablesAndDedupes(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	const (
		shared = "media/images/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
		blog   = "media/images/bb/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.png"
		proj   = "media/images/cc/cccccccccccccccccccccccccccccccc.jpg"
		avatar = "media/images/dd/dddddddddddddddddddddddddddddddd.png"
	)
	// Addressable copies for the *string parameter (Go cannot take the
	// address of an untyped constant).
	sharedRef, avatarRef := shared, avatar
	insertBlogThumb(t, db, "b1", shared)
	insertBlogThumb(t, db, "b2", blog)
	insertProjectThumb(t, db, "p1", "slug-1", shared) // same path in both tables
	insertProjectThumb(t, db, "p2", "slug-2", proj)
	insertUserAvatar(t, db, "u1", &sharedRef) // same path as the content rows — still one set member
	insertUserAvatar(t, db, "u2", &avatarRef)
	insertUserAvatar(t, db, "u3", nil) // no avatar — never a set member

	set, err := ReferencedMediaPaths(ctx, db)
	if err != nil {
		t.Fatalf("ReferencedMediaPaths: %v", err)
	}
	for _, want := range []string{shared, blog, proj, avatar} {
		if _, ok := set[want]; !ok {
			t.Errorf("set missing %q: %v", want, set)
		}
	}
	if len(set) != 4 {
		t.Errorf("set size = %d, want 4 (shared path deduped): %v", len(set), set)
	}
}

func TestMediaPathReferenced_ChecksBothTables(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := context.Background()

	const (
		blogRef = "media/images/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
		projRef = "media/images/bb/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.png"
		avRef   = "media/images/dd/dddddddddddddddddddddddddddddddd.png"
		orphan  = "media/images/cc/cccccccccccccccccccccccccccccccc.jpg"
	)
	insertBlogThumb(t, db, "b1", blogRef)
	insertProjectThumb(t, db, "p1", "slug-1", projRef)
	avRefCopy := avRef // addressable copy for the *string parameter
	insertUserAvatar(t, db, "u1", &avRefCopy)

	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"blog reference", blogRef, true},
		{"project reference", projRef, true},
		{"avatar reference", avRef, true},
		{"unreferenced path", orphan, false},
	} {
		got, err := MediaPathReferenced(ctx, db, tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: referenced = %v, want %v", tc.name, got, tc.want)
		}
	}
}
