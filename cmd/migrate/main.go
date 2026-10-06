// Command migrate applies pending schema migrations to the SQLite database.
//
// This is the explicit offline migration step (docs/patterns/go/sqlite.md):
// the operator runs it while the application is stopped or unready. The
// serving process (cmd/api) opens through the strict application opener and
// never runs migrations itself.
//
// The command opens the database through the maintenance opener, which
// bootstraps the schema_migrations metadata table when absent, applies every
// pending embedded migration (each in its own immediate transaction with
// checksum verification), verifies the applied history exactly matches the
// embedded set, and verifies the live schema against the Go-declared
// expectations (STRICT tables, columns, keys, indexes — no unexpected
// objects). The data-dependent foreign-key check runs last
// (docs/patterns/go/sqlite.md — part of the explicit migration operation,
// never readiness).
//
// Safety net (docs/patterns/go/sqlite.md, migration-scoped rollback): when the
// live database has migration history AND pending migrations to apply, the
// command first creates a validated online backup under
// data/backups/pre-migrate-<millis>.db. If migration or the foreign-key
// check then fails, the command restores that backup over the live files
// (safe: the application is offline for the whole window and nothing wrote
// between backup and rollback) and exits 1. On success the backup is
// retained for manual rollback. Restoring an OLDER backup is cmd/restore's
// staged workflow, not this command's hot rollback.
//
// Usage:
//
//	DATA_DIR=/app/data cmd/migrate
//
// Exit codes: 0 success, 1 on any failure. Outcomes go to structured logs
// on stderr.
//
// This command owns schema migrations only. The legacy-source → SQLite data
// import is a separate command (cmd/importdata).
package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
)

// backupDeadline bounds the pre-migration backup's page-copy loop
// (docs/patterns/go/sqlite.md: the deadline is checked between page batches).
const backupDeadline = 2 * time.Minute

// Migration-scoped function seams so the safety net is testable without
// poisoning embedded migrations (production wiring passes the real ones).
type (
	statusFn  func(string) (database.MigrationCounts, error)
	openFn    func(database.Config) (*sql.DB, error)
	backupFn  func(string, time.Time) (string, error)
	restoreFn func(string, string) error
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

// run executes the schema migration with the production wiring. It is the
// testable seam — main() only wires the logger and the exit code.
func run(logger *slog.Logger) error {
	return runWith(logger, database.MigrationStatus, database.OpenMaintenance,
		database.BackupForMigration, database.RestoreOfflineBackup)
}

// runWith is run with injectable seams: statusFn decides whether a backup
// is needed, openFn performs the maintenance migration, backupFn/restoreFn
// implement the rollback safety net.
func runWith(logger *slog.Logger, status statusFn, open openFn, backup backupFn, restore restoreFn) error {
	// Load and validate configuration (same env contract as cmd/api:
	// DATA_DIR, with the dev default ./data).
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create the data directory (owner-only) before touching SQLite.
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	statusCounts, err := status(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("migration status: %w", err)
	}

	// A database whose history is ahead of this artifact cannot be migrated
	// by it — fail before touching anything (docs/patterns/go/sqlite.md).
	if statusCounts.Applied > statusCounts.Embedded {
		return fmt.Errorf("database history is ahead of this artifact (%d applied, %d embedded) — redeploy the newer artifact instead of downgrading the database",
			statusCounts.Applied, statusCounts.Embedded)
	}

	// Take the safety-net backup only when the live database has real
	// history AND pending migrations — a no-op run cannot break anything.
	var backupPath string
	if statusCounts.Applied > 0 && statusCounts.Applied < statusCounts.Embedded {
		backupPath, err = backup(cfg.DataDir, time.Now().Add(backupDeadline))
		if err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
		logger.Info("pre-migration backup created", "path", backupPath)
	}

	// OpenMaintenance applies pending migrations and verifies the resulting
	// history + live schema (docs/patterns/go/sqlite.md). Every failure mode
	// returns an error here — database/store packages never call log.Fatal
	// or os.Exit themselves.
	db, err := open(database.Config{DataDir: cfg.DataDir})
	if err != nil {
		return rollbackIfBackedUp(logger, cfg.DataDir, backupPath, restore,
			fmt.Errorf("open maintenance: %w", err))
	}

	// Data-dependent relation check (docs/patterns/go/sqlite.md): the explicit
	// migration operation requires zero foreign-key violations. This is NOT
	// part of readiness (readiness does not run integrity scans). A failure
	// here invalidates the migration outcome, so the safety net applies too.
	if err := database.VerifyForeignKeys(db); err != nil {
		db.Close()
		return rollbackIfBackedUp(logger, cfg.DataDir, backupPath, restore,
			fmt.Errorf("foreign key verification: %w", err))
	}

	if err := database.CloseDatabase(db); err != nil {
		return rollbackIfBackedUp(logger, cfg.DataDir, backupPath, restore,
			fmt.Errorf("close after migration: %w", err))
	}

	logger.Info("migrations complete", "dataDir", cfg.DataDir, "backup", backupPath)
	return nil
}

// rollbackIfBackedUp restores the pre-migration backup when the migration
// failed after a backup was taken. The application is offline for the whole
// window, so the restore is a plain file swap (docs/patterns/go/sqlite.md).
func rollbackIfBackedUp(logger *slog.Logger, dataDir, backupPath string, restore restoreFn, cause error) error {
	if backupPath == "" {
		return cause
	}
	if err := restore(dataDir, backupPath); err != nil {
		return fmt.Errorf("%v; and rollback failed: %w", cause, err)
	}
	logger.Error("migration failed; database rolled back to the pre-migration backup",
		"error", cause, "backup", backupPath, "dataDir", dataDir)
	return fmt.Errorf("migration failed; database rolled back to the pre-migration backup %s (%v)", backupPath, cause)
}
