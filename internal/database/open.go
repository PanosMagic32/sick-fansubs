package database

import (
	"database/sql"
	"fmt"
)

// OpenApplication opens the database with strict migration-history validation.
//
// This is the opener used by the serving process. It first fails closed on a
// present restore marker (docs/patterns/go/sqlite.md: while a staged restore is
// in progress — or a previous one crashed — the application must not touch the
// database at all), then opens and checks that the applied migration history
// exactly matches the embedded set. Any discrepancy (missing, extra, checksum
// drift, name mismatch) is an error and the database is closed.
//
// See: docs/patterns/go/sqlite.md
func OpenApplication(config Config) (*sql.DB, error) {
	// The marker check runs before any directory work, sql.Open, Ping, WAL
	// initialization, or file access (docs/patterns/go/sqlite.md).
	if err := ensureNoRestoreMarker(config.DataDir); err != nil {
		return nil, err
	}
	return openStrict(config.DataDir)
}

// openStrict is the marker-exempt strict open: the database is opened with
// the full pool/WAL/settings setup and the exact-history check runs. The
// restore command reuses it for the post-activation check against the
// activated file while it owns the marker — the only excluded gate is the
// marker the command itself controls (docs/patterns/go/sqlite.md).
func openStrict(dataDir string) (*sql.DB, error) {
	db, err := openDB(dataDir)
	if err != nil {
		return nil, err
	}

	if err := migrationHistoryOK(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: migration check failed: %w", err)
	}

	return db, nil
}

// OpenMaintenance opens the live data directory for migration and import
// operations, refusing a present restore marker first; unlike the strict
// opener it permits an absent metadata table or a valid older prefix and
// applies pending migrations (internal/database/AGENTS.md).
func OpenMaintenance(config Config) (*sql.DB, error) {
	if err := ensureNoRestoreMarker(config.DataDir); err != nil {
		return nil, err
	}
	return openMaintenance(config)
}

// openMaintenance is the marker-exempt maintenance open: the staged-restore
// workflow opens its own operation subdirectory through it while its marker
// exists in the live data directory (docs/patterns/go/sqlite.md rule 12).
func openMaintenance(config Config) (*sql.DB, error) {
	db, err := openDB(config.DataDir)
	if err != nil {
		return nil, err
	}

	// Apply migrations (bootstraps metadata table if absent).
	if err := Apply(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: migration failed: %w", err)
	}

	// After migration, verify the history matches the embedded set.
	// This catches unknown future versions that the artifact cannot support.
	if err := migrationHistoryOK(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: migration check failed: %w", err)
	}

	// Verify the live schema matches the Go-declared expectations
	// (docs/patterns/go/sqlite.md): STRICT tables, columns, keys, and
	// indexes, with no unexpected objects. Data-dependent checks (foreign
	// keys) stay out of this opener and run in the explicit maintenance
	// flows (migrate, import, staged restore), never in readiness.
	if err := VerifySchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: schema verification failed: %w", err)
	}

	return db, nil
}
