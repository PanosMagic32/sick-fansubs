package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

// canonicalMedia is a stand-in for the layout grammar the production caller
// passes: it collapses the three legacy local forms onto the canonical path
// and drops another host. internal/store must not import internal/media
// (codestyle rule), so the store test pins only the store's use of the seam.
func canonicalMedia(ref string) (string, bool) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	canonical := "media/images/aa/" + id + ".jpg"
	canonicalJPEG := "media/images/aa/" + id + ".jpeg"
	switch {
	case ref == canonical,
		ref == "media/images/"+id+".jpg",
		ref == "media/images/cd/"+id+".jpg",
		ref == "media/images/"+id+".jpg?v=2",
		ref == "https://sickfansubs.com/media/images/"+id+".jpg",
		ref == "https://sickfansubs.com:443/media/images/"+id+".jpg":
		return canonical, true
	case ref == "media/images/"+id+".jpeg",
		ref == "https://sickfansubs.com/media/images/"+id+".jpeg":
		return canonicalJPEG, true
	case strings.HasPrefix(ref, "https://other.example/"):
		return "", false
	default:
		return ref, true
	}
}

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

	set, err := ReferencedMediaPaths(ctx, db, canonicalMedia)
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

// TestMediaReferences_LegacyFormsProtectCanonicalFile pins that a stored
// reference written before the layout rule landed — the flat served form, a
// wrong subdirectory, or a same-origin absolute URL — still enters the keep
// set and the point guard under its canonical path.
func TestMediaReferences_LegacyFormsProtectCanonicalFile(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := t.Context()

	const (
		id        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		canonical = "media/images/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
		flat      = "media/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
		wrongDir  = "media/images/cd/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
		absolute  = "https://sickfansubs.com/media/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
		absPort   = "https://sickfansubs.com:443/media/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
		query     = "media/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg?v=2"
		otherHost = "https://other.example/media/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"
	)
	insertBlogThumb(t, db, "b1", flat)
	insertBlogThumb(t, db, "b2", wrongDir)
	insertProjectThumb(t, db, "p1", "slug-1", absolute)
	insertBlogThumb(t, db, "b3", absPort)
	insertBlogThumb(t, db, "b4", query)
	insertBlogThumb(t, db, "b5", otherHost)

	set, err := ReferencedMediaPaths(ctx, db, canonicalMedia)
	if err != nil {
		t.Fatalf("ReferencedMediaPaths: %v", err)
	}
	if _, ok := set[canonical]; !ok {
		t.Fatalf("set missing canonical path %q: %v", canonical, set)
	}
	if len(set) != 1 {
		t.Errorf("set size = %d, want 1 (five legacy forms collapse, another host drops): %v", len(set), set)
	}

	got, err := MediaPathReferenced(ctx, db, canonical, canonicalMedia)
	if err != nil {
		t.Fatalf("MediaPathReferenced: %v", err)
	}
	if !got {
		t.Error("canonical path reported unreferenced despite legacy stored forms")
	}
}

// TestMediaReferences_LegacyJPEGProtectsFile pins the serving parser's
// `.jpeg` form: the write gate never creates it, but a hand-placed file is
// served, so its stored reference must keep the canonical jpeg path.
func TestMediaReferences_LegacyJPEGProtectsFile(t *testing.T) {
	t.Parallel()

	db := openStoreDB(t)
	ctx := t.Context()

	const (
		stored    = "media/images/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpeg"
		canonical = "media/images/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpeg"
	)
	insertBlogThumb(t, db, "b1", stored)

	got, err := MediaPathReferenced(ctx, db, canonical, canonicalMedia)
	if err != nil {
		t.Fatalf("MediaPathReferenced: %v", err)
	}
	if !got {
		t.Error("jpeg reference did not protect the canonical jpeg path")
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
		got, err := MediaPathReferenced(ctx, db, tc.path, canonicalMedia)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: referenced = %v, want %v", tc.name, got, tc.want)
		}
	}
}
