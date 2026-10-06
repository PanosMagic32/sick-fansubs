package database

import (
	"database/sql"
	"os"
	"testing"
)

// testDataDir creates a temporary directory for the test database.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Ensure owner-only access (0700).
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatalf("chmod temp dir: %v", err)
	}
	return dir
}

// openTestDBAt opens a test database at a fresh temp directory. It returns the
// pool and the data directory (some callers need the directory, e.g. the
// restore-marker gate in DBHealthChecker).
func openTestDBAt(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := testDataDir(t)
	db, err := openDB(dir)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dir
}

// openTestDB opens a test database and returns only the pool.
func openTestDB(t *testing.T) *sql.DB {
	db, _ := openTestDBAt(t)
	return db
}
