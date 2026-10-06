package database

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildDSN(t *testing.T) {
	dsn, err := buildDSN("/data/db")
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}

	// Must be a file: URI (Uniform Resource Identifier).
	if !strings.HasPrefix(dsn, "file:") {
		t.Fatalf("DSN must start with file:, got %q", dsn)
	}

	// Must contain the fixed filename.
	if !strings.Contains(dsn, FileName) {
		t.Fatalf("DSN must contain %q, got %q", FileName, dsn)
	}

	// URL-decode to check pragma values (net/url encodes special characters).
	decoded, err := url.QueryUnescape(dsn)
	if err != nil {
		t.Fatalf("failed to decode DSN: %v", err)
	}

	// Must contain all required pragmas.
	required := []string{
		"foreign_keys(ON)",
		"busy_timeout(5000)",
		"synchronous(FULL)",
		"trusted_schema(OFF)",
		"_txlock=immediate",
		"_dqs=0",
	}
	for _, r := range required {
		if !strings.Contains(decoded, r) {
			t.Errorf("DSN missing %q: %s", r, dsn)
		}
	}
}

func TestOpenDB(t *testing.T) {
	db := openTestDB(t)

	// PingContext should succeed.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext: %v", err)
	}
}

func TestOpenDB_NonexistentDir(t *testing.T) {
	_, err := openDB("/nonexistent/dir/for/testing")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestOpenDB_FileNotDir(t *testing.T) {
	// Create a temp file and pass it as the data directory.
	f, err := os.CreateTemp(testDataDir(t), "notadir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, err = openDB(f.Name())
	if err == nil {
		t.Fatal("expected error when data directory is a file")
	}
}

// TestOpenDB_RefusesGroupAccessibleDir pins the owner-only data-directory
// rule: a pre-existing directory with group/other access is refused at open.
func TestOpenDB_RefusesGroupAccessibleDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes are not enforceable on Windows")
	}
	dir := testDataDir(t)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := openDB(dir)
	if err == nil {
		t.Fatal("expected error for a group-accessible data directory")
	}
	if !strings.Contains(err.Error(), "owner-only") {
		t.Errorf("error should name the owner-only rule, got: %v", err)
	}
}

// TestOpenDB_RefusesMissingMainWithRollbackRemnant pins the interrupted
// migration-rollback guard: with the main file missing and a
// `.broken-<millis>` remnant (or the activation `.tmp`) present, opening must
// fail closed instead of creating an empty database in its place.
func TestOpenDB_RefusesMissingMainWithRollbackRemnant(t *testing.T) {
	dir := testDataDir(t)
	remnant := filepath.Join(dir, FileName+".broken-123")
	if err := os.WriteFile(remnant, []byte("retained live bytes"), 0o600); err != nil {
		t.Fatalf("write remnant: %v", err)
	}

	_, err := OpenMaintenance(Config{DataDir: dir})
	if err == nil {
		t.Fatal("expected fail-closed open beside a rollback remnant")
	}
	if !strings.Contains(err.Error(), "interrupted-rollback artifact") {
		t.Errorf("error should name the interrupted rollback, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, FileName)); !os.IsNotExist(statErr) {
		t.Error("a live database was created despite the guard")
	}

	// The operator's recovery resolves it: with the remnant gone, a fresh
	// (legitimately empty) data directory bootstraps as usual.
	if err := os.Remove(remnant); err != nil {
		t.Fatalf("remove remnant: %v", err)
	}
	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open after removing the remnant: %v", err)
	}
	db.Close()
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("bootstrap did not create the database: %v", err)
	}
}

// TestOpenDB_RemnantBesideLiveMainIsAllowed pins the guard's scope: remnants
// beside a present live main (a crash after activation, before cleanup) are
// leftover junk, not a reason to refuse work.
func TestOpenDB_RemnantBesideLiveMainIsAllowed(t *testing.T) {
	dir := testDataDir(t)
	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	db.Close()
	if err := os.WriteFile(filepath.Join(dir, FileName+".broken-123"), []byte("junk"), 0o600); err != nil {
		t.Fatalf("write remnant: %v", err)
	}
	db, err = Open(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open with live main + remnant: %v", err)
	}
	db.Close()
}

// TestOpeners_RefuseRestoreMarker pins the maintenance-surface gate: while a
// restore marker exists, the migration/import openers and the migration
// status read all fail closed (the staged restore's own marker-exempt open of
// its operation subdirectory is not affected).
func TestOpeners_RefuseRestoreMarker(t *testing.T) {
	dir := testDataDir(t)
	if err := acquireRestoreMarker(dir); err != nil {
		t.Fatalf("acquireRestoreMarker: %v", err)
	}

	if _, err := OpenMaintenance(Config{DataDir: dir}); err == nil {
		t.Error("OpenMaintenance: got nil, want marker refusal")
	}
	if _, err := MigrationStatus(dir); err == nil {
		t.Error("MigrationStatus: got nil, want marker refusal")
	}
	if _, err := OpenApplication(Config{DataDir: dir}); err == nil {
		t.Error("OpenApplication: got nil, want marker refusal")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); !os.IsNotExist(err) {
		t.Error("a live database was created despite the marker gate")
	}
}

func TestDBPoolSettings(t *testing.T) {
	db := openTestDB(t)

	stats := db.Stats()
	if stats.MaxOpenConnections != maxOpenConns {
		t.Errorf("got %d, want MaxOpenConns=%d", stats.MaxOpenConnections, maxOpenConns)
	}
}

func TestWALMode(t *testing.T) {
	db := openTestDB(t)

	// Verify that WAL (Write-Ahead Log) mode is active.

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("got %q, want journal_mode=wal", mode)
	}
}

func TestConnectionSettings(t *testing.T) {
	db := openTestDB(t)

	// Verify all required PRAGMA settings are active on the connection.
	// synchronous=2 means FULL (fsync after every WAL commit for durability).

	checks := []struct {
		query    string
		expected string
		label    string
	}{
		{"PRAGMA foreign_keys", "1", "foreign_keys"},
		{"PRAGMA busy_timeout", "5000", "busy_timeout"},
		{"PRAGMA synchronous", "2", "synchronous"}, // 2 = FULL
		{"PRAGMA trusted_schema", "0", "trusted_schema"},
	}

	for _, c := range checks {
		t.Run(c.label, func(t *testing.T) {
			var val string
			if err := db.QueryRow(c.query).Scan(&val); err != nil {
				t.Fatalf("query %s: %v", c.label, err)
			}
			if val != c.expected {
				t.Errorf("%s: got %q, want %q", c.label, val, c.expected)
			}
		})
	}
}

// TestMultipleConnectionSettings proves that required pragmas apply to
// every connection in the pool, not just the first one.
func TestMultipleConnectionSettings(t *testing.T) {
	db := openTestDB(t)

	// Acquire multiple connections concurrently and verify each one.
	const connCount = 4
	conns := make([]*sql.Conn, 0, connCount)
	for i := range connCount {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatalf("acquire conn %d: %v", i, err)
		}
		conns = append(conns, conn)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	for i, conn := range conns {
		var fk int
		if err := conn.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d foreign_keys: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("conn %d: got %d, want foreign_keys=1", i, fk)
		}
	}
}

func TestDBHealthChecker(t *testing.T) {
	db, dir := openTestDBAt(t)
	// Readiness now requires the applied migration history to match the
	// embedded schema — a migrated database must check
	// as ready.
	if err := Apply(db); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	checker := DBHealthChecker{DB: db, DataDir: dir}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err != nil {
		t.Fatalf("DBHealthChecker.Check: %v", err)
	}
}

// TestDBHealthChecker_NoMigrationHistory pins the readiness rule: a
// database without a matching migration history must
// report not-ready, even when the pool answers pings.
func TestDBHealthChecker_NoMigrationHistory(t *testing.T) {
	db, dir := openTestDBAt(t)
	checker := DBHealthChecker{DB: db, DataDir: dir}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := checker.Check(ctx); err == nil {
		t.Fatal("expected readiness failure for an unmigrated database")
	}
}

func TestDBHealthChecker_ClosedDB(t *testing.T) {
	db, dir := openTestDBAt(t)
	db.Close()

	checker := DBHealthChecker{DB: db, DataDir: dir}
	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("expected error from closed database")
	}
}

func TestCloseDatabase(t *testing.T) {
	db := openTestDB(t)

	if err := CloseDatabase(db); err != nil {
		t.Fatalf("CloseDatabase: %v", err)
	}
	// The documented idempotent contract: the second close returns nil.
	if err := CloseDatabase(db); err != nil {
		t.Fatalf("second CloseDatabase = %v, want nil", err)
	}
}
