package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// backupsDirName is the owner-only backup subdirectory beneath the data
// directory (docs/patterns/go/sqlite.md: backups live on the same filesystem).
const backupsDirName = "backups"

// backupPageBatch is the finite number of pages copied per Backup.Step
// (docs/patterns/go/sqlite.md: cancellation cannot interrupt a running step,
// so the batch size bounds that exposure).
const backupPageBatch = 128

// backupRetryDelay is the bounded retry sleep for SQLITE_BUSY/SQLITE_LOCKED
// during the copy loop. The deadline check bounds the total retry time.
const backupRetryDelay = 50 * time.Millisecond

// backupConnAcquireTimeout bounds how long we wait for a dedicated source
// connection from the pool.
const backupConnAcquireTimeout = 5 * time.Second

// backupDriverConn is the small private interface exposing modernc's online
// backup API. Driver-specific backup behavior is reached only through
// sql.Conn.Raw; the connection and *sqlite.Backup never escape the callback.
type backupDriverConn interface {
	NewBackup(dstURI string) (*sqlite.Backup, error)
}

// MigrationStatus reports the applied/embedded migration counts without
// applying anything; it refuses a present restore marker
// (docs/patterns/go/sqlite.md; internal/database/AGENTS.md).
func MigrationStatus(dataDir string) (MigrationCounts, error) {
	if err := ensureNoRestoreMarker(dataDir); err != nil {
		return MigrationCounts{}, err
	}
	db, err := Open(Config{DataDir: dataDir})
	if err != nil {
		return MigrationCounts{}, err
	}
	defer db.Close()

	embedded, err := loadedMigrations()
	if err != nil {
		return MigrationCounts{}, err
	}
	applied, err := appliedMigrations(db)
	if err != nil {
		return MigrationCounts{}, err
	}
	return MigrationCounts{Applied: len(applied), Embedded: len(embedded)}, nil
}

// MigrationCounts is the applied/embedded migration count pair.
type MigrationCounts struct {
	Applied  int
	Embedded int
}

// backupHistoryCheck validates a backup's migration history. The pre-
// migration backup carries the OLDER-prefix history of the artifact that
// created it (docs/patterns/go/sqlite.md); the daily backup
// is created by the same artifact that will serve it, so it
// must carry the EXACT history. The deploy trigger sits between the two:
// it runs before the deploy's migration, so the NEW backup image validates
// the OLD artifact against the NEW embedded set — a contiguous prefix, not
// the exact set.
type backupHistoryCheck func(db *sql.DB) error

// BackupForMigration creates the migration command's pre-migration backup in
// data/backups/ as pre-migrate-<millis>.db, validated as a contiguous prefix
// of the embedded history (internal/database/AGENTS.md).
func BackupForMigration(dataDir string, deadline time.Time) (string, error) {
	return createBackupCore(dataDir, filepath.Join(dataDir, backupsDirName),
		fmt.Sprintf("pre-migrate-%d.db", time.Now().UnixMilli()), deadline, migrationPrefixOK)
}

// BackupForDaily creates the daily-backup artifact: the same
// validated online backup as BackupForMigration, published under an arbitrary
// destination directory (the date-stamped backup unit folder) with the given
// filename, and validated against the EXACT embedded migration history — the
// daily backup is created and restored by the same serving artifact. The
// restore-marker gate applies (the daily backup runs inside the single-owner
// unready window; a marker means that window is broken).
func BackupForDaily(dataDir, destDir, fileName string, deadline time.Time) (string, error) {
	return createBackupCore(dataDir, destDir, fileName, deadline, migrationHistoryOK)
}

// BackupForPreMigration creates the deploy-trigger rollback unit:
// the same unit layout as BackupForDaily, but
// validated as a CONTIGUOUS PREFIX of the embedded set instead of the exact
// set. The deploy trigger backs up BEFORE migrating — when the new tag ships
// a new migration, the live database is one (or more) versions behind the
// NEW backup image's embedded set, and the exact check fails closed exactly
// when the rollback point is needed. The prefix check keeps every other
// fail-closed guarantee (gaps, extras, drift, malformed rows); only
// "applied == embedded" is relaxed. The unit stays restorable: the staged
// restore validates prefix history and upgrades the restored database to
// the artifact's embedded set on activation.
func BackupForPreMigration(dataDir, destDir, fileName string, deadline time.Time) (string, error) {
	return createBackupCore(dataDir, destDir, fileName, deadline, migrationPrefixOK)
}

// createBackupCore is the shared online-backup workflow (docs/patterns/go/sqlite.md)
// parameterized by publication destination and the history check.
func createBackupCore(dataDir, destDir, fileName string, deadline time.Time, history backupHistoryCheck) (string, error) {
	// A restore in progress makes the unready window invalid — fail closed
	// before touching SQLite (the marker gate the strict opener uses).
	if err := ensureNoRestoreMarker(dataDir); err != nil {
		return "", err
	}

	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", fmt.Errorf("database: create backup directory: %w", err)
	}

	tmpPath := filepath.Join(destDir, ".tmp-"+randomHex(8)+".db")
	// Best-effort cleanup of temp names the caller never sees; the returned
	// error is the failure, and a stray temp file is never promoted.
	cleanup := func() {
		for _, p := range []string{tmpPath, tmpPath + "-wal", tmpPath + "-shm"} {
			_ = os.Remove(p)
		}
	}

	dsn, err := buildDSN(dataDir)
	if err != nil {
		return "", err
	}
	srcDB, err := openDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("database: open backup source: %w", err)
	}
	defer srcDB.Close()

	copyErr := backupCopyPages(srcDB, tmpPath, deadline)
	if copyErr != nil {
		srcDB.Close()
		cleanup()
		return "", copyErr
	}
	if err := srcDB.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("database: close backup source: %w", err)
	}

	if err := verifyBackupArtifactWith(tmpPath, history); err != nil {
		cleanup()
		return "", err
	}

	if err := syncFile(tmpPath); err != nil {
		cleanup()
		return "", fmt.Errorf("database: sync backup artifact: %w", err)
	}
	finalPath := filepath.Join(destDir, fileName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		cleanup()
		return "", fmt.Errorf("database: publish backup: %w", err)
	}
	if err := syncDir(destDir); err != nil {
		return "", fmt.Errorf("database: sync backup directory: %w", err)
	}

	return finalPath, nil
}

// backupCopyPages runs the bounded page-copy loop over the modernc backup
// API. The driver connection and Backup object stay inside this function's
// Raw callback (docs/patterns/go/sqlite.md). The Finish cleanup error is
// preserved and joined with the primary error (docs/patterns/go/sqlite.md:
// "preserve both the primary and cleanup error when both occur").
func backupCopyPages(srcDB *sql.DB, dstPath string, deadline time.Time) (retErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), backupConnAcquireTimeout)
	defer cancel()

	srcConn, err := srcDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("database: acquire backup source connection: %w", err)
	}
	defer srcConn.Close()

	return srcConn.Raw(func(driverConn any) (retErr error) {
		c, ok := driverConn.(backupDriverConn)
		if !ok {
			return fmt.Errorf("database: driver connection does not support online backup")
		}

		b, err := c.NewBackup(buildDSNForFile(dstPath))
		if err != nil {
			return fmt.Errorf("database: start backup: %w", err)
		}
		// Finish must run exactly once: mark it BEFORE the explicit call, so a
		// Finish error does not make the deferred call run a second time. The
		// deferred path joins the cleanup error with the primary one.
		finished := false
		defer func() {
			if !finished {
				retErr = errors.Join(retErr, b.Finish())
			}
		}()

		// The destination file exists now that the destination connection
		// is open — restrict it before the first page copy
		// (docs/patterns/go/sqlite.md).
		if err := os.Chmod(dstPath, 0o600); err != nil {
			return fmt.Errorf("database: restrict backup destination: %w", err)
		}

		for {
			if time.Now().After(deadline) {
				return errors.New("database: backup deadline exceeded")
			}
			more, err := b.Step(backupPageBatch)
			if err != nil {
				// Bounded busy/locked handling: retry until the deadline.
				se, ok := errors.AsType[*sqlite.Error](err)
				if ok && (se.Code() == sqlite3.SQLITE_BUSY || se.Code() == sqlite3.SQLITE_LOCKED) {
					time.Sleep(backupRetryDelay)
					continue
				}
				return fmt.Errorf("database: backup step: %w", err)
			}
			if !more {
				break
			}
		}

		finished = true
		if err := b.Finish(); err != nil {
			return fmt.Errorf("database: backup finish: %w", err)
		}
		return nil
	})
}

// verifyBackupArtifact reopens a completed backup independently and validates
// it as a contiguous prefix of the embedded history — the restore-facing
// check (docs/patterns/go/sqlite.md rule 11).
func verifyBackupArtifact(path string) error {
	return verifyBackupArtifactWith(path, migrationPrefixOK)
}

// verifyBackupArtifactWith is verifyBackupArtifact with the history check
// chosen by the caller: exact (daily backup) or contiguous prefix
// (pre-migration backup).
//
// The open is READ-WRITE on purpose: a backup artifact is WAL-mode (the
// online backup API copies the source's journal mode), and opening WAL
// read-only makes the driver materialize a -shm sidecar — which fails on a
// read-only filesystem and leaves files behind on a writable one. Callers
// validate a private copy in a writable location (StagedRestore) or an
// artifact in a writable directory (backup creation), so the rw open's
// sidecars are always legal and cleaned on close. The artifact itself is
// never mutated by these checks.
func verifyBackupArtifactWith(path string, history backupHistoryCheck) error {
	db, err := openDSN(buildDSNForFile(path))
	if err != nil {
		return fmt.Errorf("database: reopen backup artifact: %w", err)
	}
	defer db.Close()

	if err := history(db); err != nil {
		return fmt.Errorf("database: backup migration history: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("database: backup quick_check: %w", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return fmt.Errorf("database: backup quick_check scan: %w", err)
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("database: backup quick_check iterate: %w", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("database: backup quick_check: got %v, want exactly [ok]", results)
	}

	fkRows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("database: backup foreign_key_check: %w", err)
	}
	defer fkRows.Close()
	var fkViolations int
	for fkRows.Next() {
		fkViolations++
	}
	if err := fkRows.Err(); err != nil {
		return fmt.Errorf("database: backup foreign_key_check iterate: %w", err)
	}
	if fkViolations != 0 {
		return fmt.Errorf("database: backup foreign_key_check: %d violation(s)", fkViolations)
	}

	return nil
}
