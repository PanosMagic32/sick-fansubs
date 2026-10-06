package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// appliedMigrations reads the current migration state from schema_migrations.
//
// Returns a map of version → migration row for all applied migrations.
// If the table does not exist yet (valid pre-bootstrap state), returns an
// empty map. Any OTHER query error is real (e.g. a drifted table shape) and
// must propagate — swallowing every error as "no table" would make real
// database errors masquerade as "needs migration".
func appliedMigrations(db *sql.DB) (map[int]migrationRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return appliedMigrationsCtx(ctx, db)
}

// appliedMigrationsCtx is appliedMigrations under the caller's context.
func appliedMigrationsCtx(ctx context.Context, db *sql.DB) (map[int]migrationRow, error) {
	// Existence probe first: a clean error-free way to distinguish
	// "metadata table not yet created" from a genuine query failure.
	var exists int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'",
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("migrate: probe schema_migrations: %w", err)
	}
	if exists == 0 {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx,
		"SELECT version, name, checksum, applied_at_ms FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("migrate: query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]migrationRow)
	for rows.Next() {
		var r migrationRow
		if err := rows.Scan(&r.Version, &r.Name, &r.Checksum, &r.AppliedAtMs); err != nil {
			return nil, fmt.Errorf("migrate: scan migration row: %w", err)
		}
		applied[r.Version] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate: iterate migration rows: %w", err)
	}

	return applied, nil
}

type migrationRow struct {
	Version     int
	Name        string
	Checksum    []byte
	AppliedAtMs int64
}

// appliedPrefixOK rejects a stored history that is not a contiguous prefix of
// the embedded set (absent metadata or zero rows pass — the bootstrap and
// first apply states). It runs before Apply writes anything.
func appliedPrefixOK(db *sql.DB) error {
	embedded, err := loadedMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedMigrations(db)
	if err != nil {
		return err
	}
	return checkAppliedPrefix(applied, embedded)
}

// checkRowInvariants rejects a stored row that violates the metadata
// contract, regardless of the history shape: tampered or hand-built metadata
// fails closed before any comparison.
func checkRowInvariants(applied map[int]migrationRow) error {
	for version, row := range applied {
		if version < 1 {
			return fmt.Errorf("migrate: invalid stored version %d", version)
		}
		if row.Name == "" {
			return fmt.Errorf("migrate: version %d: stored name is empty", version)
		}
		if len(row.Checksum) != 32 {
			return fmt.Errorf("migrate: version %d: stored checksum length %d, want 32", version, len(row.Checksum))
		}
		if row.AppliedAtMs < 0 {
			return fmt.Errorf("migrate: version %d: stored applied_at_ms %d is negative", version, row.AppliedAtMs)
		}
	}
	return nil
}

// migrationPrefixOK checks that the applied history is a valid CONTIGUOUS
// PREFIX of the embedded set (versions 1..N with matching names and
// checksums, no gaps, no extras).
//
// This is the validation for pre-migration backups (docs/patterns/go/sqlite.md,
// the backup rules): a backup taken just before this artifact migrates carries
// the OLDER prefix history, so it cannot satisfy migrationHistoryOK. The
// prefix check still fails closed on gaps, extras, drift, and malformed rows —
// only the "applied == embedded" requirement is relaxed.
//
// See: docs/patterns/go/sqlite.md
func migrationPrefixOK(db *sql.DB) error {
	embedded, err := loadedMigrations()
	if err != nil {
		return err
	}

	applied, err := appliedMigrations(db)
	if err != nil {
		return err
	}

	// A backup of a not-yet-migrated database has no history rows — reject:
	// the safety net only backs up databases with real history.
	if len(applied) == 0 {
		return errors.New("migrate: no migration history found in backup artifact")
	}

	return checkAppliedPrefix(applied, embedded)
}

// checkAppliedPrefix validates that the applied versions form the contiguous
// prefix 1..N of the embedded set with matching names and checksums — no gaps,
// no extras, no drift. An empty map passes.
func checkAppliedPrefix(applied map[int]migrationRow, embedded []migration) error {
	if err := checkRowInvariants(applied); err != nil {
		return err
	}

	if len(applied) > len(embedded) {
		return fmt.Errorf("migrate: applied %d versions exceeds embedded %d", len(applied), len(embedded))
	}

	// Every applied version 1..N must exist in the embedded set with the
	// same name and checksum.
	for v := 1; v <= len(applied); v++ {
		row, ok := applied[v]
		if !ok {
			return fmt.Errorf("migrate: applied history has a gap at version %d", v)
		}
		if v > len(embedded) || embedded[v-1].Version != v {
			return fmt.Errorf("migrate: version %d not in embedded set", v)
		}
		if row.Name != embedded[v-1].Name {
			return fmt.Errorf("migrate: version %d name mismatch: stored %q, embedded %q", v, row.Name, embedded[v-1].Name)
		}
		if string(row.Checksum) != string(embedded[v-1].Checksum) {
			return fmt.Errorf("migrate: version %d checksum mismatch", v)
		}
	}

	return nil
}

// migrationHistoryOK checks that the applied migrations exactly match the
// embedded set (same versions, names, and checksums, with no gaps, no extras,
// and no unknown versions). This is the application opener's strict check.
//
// See: docs/patterns/go/sqlite.md
func migrationHistoryOK(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return migrationHistoryOKCtx(ctx, db)
}

// migrationHistoryOKCtx is migrationHistoryOK under the caller's context —
// the readiness probe threads its own bounded context here.
func migrationHistoryOKCtx(ctx context.Context, db *sql.DB) error {
	embedded, err := loadedMigrations()
	if err != nil {
		return err
	}

	applied, err := appliedMigrationsCtx(ctx, db)
	if err != nil {
		return err
	}

	// The metadata table must exist and contain rows.
	if len(applied) == 0 {
		return errors.New("migrate: no migration history found — database has not been migrated")
	}

	// Validate every stored row's invariants in Go (docs/patterns/go/sqlite.md):
	// the CHECK constraints normally enforce these, but a tampered or
	// hand-built metadata table must fail the strict check regardless.
	if err := checkRowInvariants(applied); err != nil {
		return err
	}

	if len(applied) != len(embedded) {
		return fmt.Errorf("migrate: migration count mismatch: applied %d, embedded %d", len(applied), len(embedded))
	}

	for _, m := range embedded {
		row, ok := applied[m.Version]
		if !ok {
			return fmt.Errorf("migrate: embedded version %d (%s) not found in applied history", m.Version, m.Name)
		}
		if row.Name != m.Name {
			return fmt.Errorf("migrate: version %d name mismatch: stored %q, embedded %q", m.Version, row.Name, m.Name)
		}
		if string(row.Checksum) != string(m.Checksum) {
			return fmt.Errorf("migrate: version %d checksum mismatch", m.Version)
		}
	}

	// Check for extra versions (applied but not embedded — future schema).
	for v, row := range applied {
		found := false
		for _, m := range embedded {
			if m.Version == v {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("migrate: extra migration version %d (%s) in applied history not in embedded set", v, row.Name)
		}
	}

	return nil
}
