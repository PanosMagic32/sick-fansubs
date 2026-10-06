package main

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"sick-fansubs/internal/database"
)

// The migrate command's wiring is tested end-to-end
// (fresh migrate, idempotent re-run, strict opener acceptance).
func TestRun_MigratesAndIsIdempotent(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := run(logger); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Re-running must be a no-op (already-applied rows verify and skip).
	if err := run(logger); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// The strict application opener must accept the result.
	db, err := database.OpenApplication(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication after migrate: %v", err)
	}
	defer db.Close()

	var versions int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	// The applied count must equal the embedded set — read dynamically so a
	// new migration (e.g. the projects schema) cannot stale this pin.
	status, err := database.MigrationStatus(dir)
	if err != nil {
		t.Fatalf("migration status: %v", err)
	}
	if versions != status.Embedded {
		t.Errorf("applied migrations: got %d, want %d (the embedded set)", versions, status.Embedded)
	}
}

// TestRun_RollsBackOnMigrationFailure proves the safety net end-to-end: a
// pending migration triggers a real pre-migration backup; when the migration
// step then fails, the live database is restored to that backup and the
// original data survives.
func TestRun_RollsBackOnMigrationFailure(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Build a real migrated database with one row.
	if err := run(logger); err != nil {
		t.Fatalf("seed migrate: %v", err)
	}
	db, err := database.OpenApplication(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'Original', '', '', 'https://example.com/t.jpg', 'published', 1000, 500, 500)`); err != nil {
		db.Close()
		t.Fatalf("insert row: %v", err)
	}
	db.Close()

	// Fake seams: pending migrations exist (backup taken), then the
	// maintenance migration step fails — as a poisoned migration would.
	err = runWith(
		logger,
		func(string) (database.MigrationCounts, error) {
			return database.MigrationCounts{Applied: 2, Embedded: 3}, nil
		},
		func(database.Config) (*sql.DB, error) {
			return nil, errors.New("simulated migration failure")
		},
		database.BackupForMigration,
		database.RestoreOfflineBackup,
	)
	if err == nil {
		t.Fatal("runWith: got nil, want error")
	}

	// The strict opener must accept the rolled-back database and the
	// original data must be intact.
	db, err = database.OpenApplication(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("strict open after rollback: %v", err)
	}
	defer db.Close()

	var title string
	if err := db.QueryRow("SELECT title FROM blog_posts WHERE id = 'p1'").Scan(&title); err != nil {
		t.Fatalf("read rolled-back row: %v", err)
	}
	if title != "Original" {
		t.Errorf("title = %q, want Original", title)
	}
}

// TestRun_FailsClosedWhenHistoryAhead proves the migrate command refuses a
// database whose history is newer than this artifact — before any backup or
// migration work (docs/patterns/go/sqlite.md: unknown future migrations are
// rejected).
func TestRun_FailsClosedWhenHistoryAhead(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	backupCalled := false
	err := runWith(
		logger,
		func(string) (database.MigrationCounts, error) {
			return database.MigrationCounts{Applied: 4, Embedded: 3}, nil
		},
		func(database.Config) (*sql.DB, error) {
			t.Error("maintenance opener must not run when history is ahead")
			return nil, errors.New("unreachable")
		},
		func(string, time.Time) (string, error) {
			backupCalled = true
			return "", nil
		},
		database.RestoreOfflineBackup,
	)
	if err == nil {
		t.Fatal("runWith: got nil, want error")
	}
	if backupCalled {
		t.Error("backup must not run when history is ahead of this artifact")
	}
}

// TestRun_SkipsBackupWhenNothingPending proves a no-op migration run takes
// no backup — dev-local runs migrate on every startup and must not pile up
// pre-migrate artifacts when there is nothing to apply.
func TestRun_SkipsBackupWhenNothingPending(t *testing.T) {
	// No t.Parallel: t.Setenv mutates process-global state.
	dir := testDataDir(t)
	t.Setenv("DATA_DIR", dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := run(logger); err != nil {
		t.Fatalf("seed migrate: %v", err)
	}

	backupCalled := false
	err := runWith(
		logger,
		database.MigrationStatus,
		database.OpenMaintenance,
		func(string, time.Time) (string, error) {
			backupCalled = true
			return "", nil
		},
		database.RestoreOfflineBackup,
	)
	if err != nil {
		t.Fatalf("no-op re-run: %v", err)
	}
	if backupCalled {
		t.Error("backup must not run when no migrations are pending")
	}
}

// TestBackupDeadlinePinned pins the safety net's page-copy budget, so a
// silent change is caught here.
func TestBackupDeadlinePinned(t *testing.T) {
	t.Parallel()

	if backupDeadline != 2*time.Minute {
		t.Errorf("backupDeadline = %s, want 2m", backupDeadline)
	}
}
