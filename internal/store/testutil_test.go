package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store/storetest"
)

// openStoreDB creates a temporary SQLite database with all migrations applied
// and returns the open connection pool. The database is cleaned up when the
// test completes.
//
// This follows the same pattern as internal/database tests: real SQLite,
// real migrations, no mocks.
func openStoreDB(t *testing.T) *sql.DB {
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
	return db
}

// testDataDir returns an owner-only temporary directory for tests that open a
// database: database.Open refuses a data directory with group or other access,
// and t.TempDir can inherit wider bits from the environment.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod test data dir: %v", err)
	}
	return dir
}

// digestOf returns the SHA-256 digest of raw.
func digestOf(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

func nowMS() int64 { return time.Now().UnixMilli() }

// insertUser seeds a user row through the shared fixture and returns the id.
// The password is a real bcrypt verifier so consume-path tests can verify it.
func insertUser(t *testing.T, db *sql.DB, username, email, status string, authVersion int64) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt fixture: %v", err)
	}
	if len(username) > 32 {
		t.Fatalf("fixture username too long")
	}
	id := strings.Repeat("a", 32)
	id = username + id[len(username):]
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:                 id,
		Username:           username,
		Email:              email,
		PasswordHash:       string(hash),
		Status:             status,
		MustChangePassword: true,
		AuthVersion:        authVersion,
		CreatedAtMS:        nowMS(),
	})
	return id
}

// seedSession inserts one session row under the given auth_version.
func seedSession(t *testing.T, db *sql.DB, userID string, authVersion int64) error {
	t.Helper()
	tok := digestOf([]byte("seed-session-token-seed-session-0"))
	now := nowMS()
	_, err := db.ExecContext(t.Context(),
		`INSERT INTO sessions (id, user_id, token_digest, csrf_token, auth_version, created_at_ms, expires_at_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"seed-session-id", userID, tok, digestOf([]byte("csrf-seed-csrf-seed-csrf-seed-0000")), authVersion, now, now+1000)
	return err
}

// mustCreateUser seeds a user row through the shared fixture: the store
// exposes no test-only creation path, and these tests need a user without a
// session.
func mustCreateUser(t *testing.T, db *sql.DB, id, username, usernameCanon, email string) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:            id,
		Username:      username,
		UsernameCanon: usernameCanon,
		Email:         email,
	})
}

// mustCreateSession is a test helper that creates a session and fails the test on error.
func mustCreateSession(t *testing.T, db *sql.DB, id, userID string, digest, csrf []byte, authVersion, createdAtMS, expiresAtMS int64) {
	t.Helper()
	err := CreateSession(t.Context(), db, CreateSessionParams{
		ID:          id,
		UserID:      userID,
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: authVersion,
		CreatedAtMS: createdAtMS,
		ExpiresAtMS: expiresAtMS,
	})
	if err != nil {
		t.Fatalf("mustCreateSession(%q): %v", id, err)
	}
}

// createSessionRow inserts one session with a unique digest derived from seed
// and an optional client label, so a test can control every list input.
func createSessionRow(t *testing.T, db *sql.DB, id, userID string, seed byte, label string, createdMS, expiresMS int64) []byte {
	t.Helper()
	digest := bytes.Repeat([]byte{seed}, 32)
	csrf := bytes.Repeat([]byte{seed + 100}, 32)
	if err := CreateSession(t.Context(), db, CreateSessionParams{
		ID:          id,
		UserID:      userID,
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: 1,
		ClientLabel: label,
		CreatedAtMS: createdMS,
		ExpiresAtMS: expiresMS,
	}); err != nil {
		t.Fatalf("create session %q: %v", id, err)
	}
	return digest
}

// mustCreateBlogPost inserts one blog_posts row with valid constraint values.
// A publishedAtMS of 0 stores NULL published_at_ms; created/updated derive
// from the publish time so the CHECK constraints hold without distracting
// from what each test asserts.
func mustCreateBlogPost(t *testing.T, db *sql.DB, p BlogPostSummary, status string) {
	t.Helper()

	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, '', ?, ?, ?, ?, ?)`

	var published any
	created := int64(1)
	if p.PublishedAtMS > 0 {
		published = p.PublishedAtMS
		created = p.PublishedAtMS - 60_000
		if created <= 0 {
			created = 1
		}
	}

	if _, err := db.Exec(q, p.ID, p.Title, p.Subtitle, p.ThumbnailURL, status,
		published, created, created); err != nil {
		t.Fatalf("insert blog post %q: %v", p.ID, err)
	}
}

// mustCreateComment inserts one comment row directly (the store's create
// paths are under test themselves — fixtures bypass them).
func mustCreateComment(t *testing.T, db *sql.DB, k ContentKind, id, userID, contentID string, parentID *string, body string, hearts int64, createdMS, updatedMS int64) {
	t.Helper()
	var parent any
	if parentID != nil {
		parent = *parentID
	}
	q := fmt.Sprintf(`INSERT INTO %s (id, user_id, %s, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, k.comments, k.commentFK)
	if _, err := db.Exec(q, id, userID, contentID, parent, body, hearts, createdMS, updatedMS); err != nil {
		t.Fatalf("insert comment %q: %v", id, err)
	}
}

// mustCreateHeart inserts one heart row directly (fixtures bypass the counter
// — tests that need the counter/source agreement set `hearts` on
// mustCreateComment to match).
func mustCreateHeart(t *testing.T, db *sql.DB, k ContentKind, userID, commentID string, createdMS int64) {
	t.Helper()
	if _, err := db.Exec(fmt.Sprintf(
		`INSERT INTO %s (user_id, comment_id, created_at_ms) VALUES (?, ?, ?)`, k.hearts),
		userID, commentID, createdMS); err != nil {
		t.Fatalf("insert heart (%s,%s): %v", userID, commentID, err)
	}
}

// mustCreateProject inserts one projects row with valid constraint values.
// A publishedAtMS of 0 stores NULL published_at_ms; created/updated derive
// from the publish time so the CHECK constraints hold without distracting
// from what each test asserts. The slug is derived from the id (unique,
// length >= 1).
func mustCreateProject(t *testing.T, db *sql.DB, p ProjectSummary, status string) {
	t.Helper()

	const q = `INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, '', ?, ?, ?, ?, ?, ?)`

	var published any
	created := int64(1)
	if p.PublishedAtMS > 0 {
		published = p.PublishedAtMS
		created = p.PublishedAtMS - 60_000
		if created <= 0 {
			created = 1
		}
	}

	if _, err := db.Exec(q, p.ID, p.Title, "slug-"+p.ID, p.ThumbnailURL, status,
		published, created, created); err != nil {
		t.Fatalf("insert project %q: %v", p.ID, err)
	}
}

// insertUserNotification seeds one user_notifications row.
func insertUserNotification(t *testing.T, db *sql.DB, id, recipientID, actorID, contentKind, contentID, commentID string, createdMs int64, readMs *int64) {
	t.Helper()
	if _, err := db.Exec(`
			INSERT INTO user_notifications
				(id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms, read_at_ms)
			VALUES (?, ?, 'comment_reply', ?, ?, ?, ?, ?, ?)`,
		id, recipientID, nullableString(actorID), contentKind, contentID, commentID, createdMs, readMs); err != nil {
		t.Fatalf("insert user notification %s: %v", id, err)
	}
}

// seedFavoriteFixtures inserts one user plus a published and a draft blog
// post and project (fixed ids) for the favorites tests.
func seedFavoriteFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       "u1",
		Username: "Katakuri",
	})
	// Published + draft blog posts and projects (the content columns).
	for _, q := range []string{
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b1', 'B1', 'Sub B1', 'blog one', 'https://example.com/b1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b2', 'B2', '', 'blog two', 'https://example.com/b2.jpg', 'published', 3000, 1000, 1000)`,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('b3', 'B3', '', 'draft', 'https://example.com/b3.jpg', 'draft', NULL, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p1', 'P1', 'project one', 'p1', 'https://example.com/p1.jpg', 'published', 2000, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p2', 'P2', 'project two', 'p2', 'https://example.com/p2.jpg', 'published', 3000, 1000, 1000)`,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url, status,
			published_at_ms, created_at_ms, updated_at_ms)
		 VALUES ('p3', 'P3', 'draft project', 'p3', 'https://example.com/p3.jpg', 'draft', NULL, 1000, 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed content: %v", err)
		}
	}
}

// dlLink returns a pointer to s (the download link columns are nullable).
func dlLink(s string) *string { return new(s) }
