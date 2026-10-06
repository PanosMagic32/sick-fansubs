package database

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationDir is the filesystem subdirectory containing migration files.
const migrationDir = "migrations"

// migration represents a single forward migration file.
type migration struct {
	Version  int    // parsed from filename (e.g., 0001 → 1)
	Name     string // description part (e.g., "users" from "0001_users.sql")
	Checksum []byte // raw 32-byte SHA-256 (Secure Hash Algorithm 256-bit) digest
	SQL      string // the raw SQL (Structured Query Language) content
}

// loadedMigrations reads, parses, validates, and sorts all embedded migrations.
//
// It returns an error if:
//   - Any filename does not match the expected pattern (NNNN_description.sql).
//   - Duplicate versions or names are found.
//   - Any file cannot be read.
func loadedMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, migrationDir)
	if err != nil {
		return nil, fmt.Errorf("migrate: read migration directory: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	seenVersions := make(map[int]bool)
	seenNames := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, desc, err := parseMigrationFilename(name)
		if err != nil {
			return nil, fmt.Errorf("migrate: %w", err)
		}
		if seenVersions[version] {
			return nil, fmt.Errorf("migrate: duplicate version %d in %q", version, name)
		}
		if seenNames[desc] {
			return nil, fmt.Errorf("migrate: duplicate name %q in %q", desc, name)
		}
		seenVersions[version] = true
		seenNames[desc] = true

		content, err := migrationFS.ReadFile(migrationDir + "/" + name)
		if err != nil {
			return nil, fmt.Errorf("migrate: read %q: %w", name, err)
		}

		sum := sha256.Sum256(content)
		migrations = append(migrations, migration{
			Version:  version,
			Name:     desc,
			Checksum: sum[:],
			SQL:      string(content),
		})
	}

	// Validate contiguous versions starting at 1.
	if len(migrations) == 0 {
		return nil, errors.New("migrate: no migration files found")
	}

	slices.SortFunc(migrations, func(a, b migration) int {
		return cmp.Compare(a.Version, b.Version)
	})

	if migrations[0].Version != 1 {
		return nil, fmt.Errorf("migrate: first migration version must be 1, got %d", migrations[0].Version)
	}
	for i := 1; i < len(migrations); i++ {
		expected := migrations[i-1].Version + 1
		if migrations[i].Version != expected {
			return nil, fmt.Errorf("migrate: non-contiguous versions: found %d after %d, expected %d",
				migrations[i].Version, migrations[i-1].Version, expected)
		}
	}

	return migrations, nil
}

// parseMigrationFilename extracts the integer version and name description
// from a filename like "0001_users.sql". Version "0001" maps to integer 1.
func parseMigrationFilename(name string) (version int, desc string, err error) {
	base := strings.TrimSuffix(name, ".sql")

	// Find the first underscore separator.
	idx := strings.IndexByte(base, '_')
	if idx < 1 {
		return 0, "", fmt.Errorf("invalid migration filename %q: expected NNNN_description.sql", name)
	}

	versionStr := base[:idx]
	if len(versionStr) < 1 {
		return 0, "", fmt.Errorf("invalid migration filename %q: version part too short", name)
	}

	v, err := strconv.Atoi(versionStr)
	if err != nil || v < 1 {
		return 0, "", fmt.Errorf("invalid migration version in %q: %w", name, err)
	}

	desc = base[idx+1:]
	if desc == "" {
		return 0, "", fmt.Errorf("invalid migration filename %q: description part is empty", name)
	}

	return v, desc, nil
}

// bootstrapMigrations creates the schema_migrations metadata table.
//
// This is the only unversioned schema DDL (Data Definition Language). It creates the runner-owned
// metadata table inside its own immediate transaction. After creation,
// it verifies the table structure using PRAGMA introspection.
//
// If the table already exists with the correct structure, this is a no-op.
//
// See: docs/patterns/go/sqlite.md
func bootstrapMigrations(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("migrate: bootstrap transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version      INTEGER PRIMARY KEY CHECK (version > 0),
		name         TEXT    NOT NULL UNIQUE CHECK (length(name) > 0),
		checksum     BLOB    NOT NULL CHECK (length(checksum) = 32),
		applied_at_ms INTEGER NOT NULL CHECK (applied_at_ms >= 0)
	) STRICT`)
	if err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: bootstrap commit: %w", err)
	}

	// Verify the table structure matches expectations using PRAGMA introspection.
	// The rules in docs/patterns/go/sqlite.md require checking column names,
	// types, nullability, primary/unique keys, and STRICT status. The
	// expectations come from the shared schema spec — no duplicated
	// column table here.
	spec := schemaSpec["schema_migrations"]
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := verifyTableColumns(ctx2, db, "schema_migrations", spec); err != nil {
		return fmt.Errorf("migrate: bootstrap table verification: %w", err)
	}
	if err := verifyIndexes(ctx2, db, "schema_migrations", spec); err != nil {
		return fmt.Errorf("migrate: bootstrap table verification: %w", err)
	}

	return nil
}

// Apply runs all pending migrations.
//
// The embedded set is loaded and validated before any database write, and the
// existing metadata — when present — must already be a contiguous prefix of
// that set; a gap would make this run fill in a version whose schema effect a
// later applied migration already produced, leaving a ledger no re-run can
// repair.
//
// For each embedded migration that has not been applied:
//  1. Begin an immediate transaction.
//  2. Re-read the migration row inside the transaction (guard against concurrent runner).
//  3. If already applied with matching name/checksum → skip.
//  4. If already applied with mismatched name/checksum → error.
//  5. Execute the SQL script.
//  6. Insert the migration row with current UTC (Coordinated Universal Time)
//     Unix-millisecond timestamp.
//  7. Commit.
//
// This guarantees at most one runner applies a version.
//
// See: docs/patterns/go/sqlite.md
func Apply(db *sql.DB) error {
	migrations, err := loadedMigrations()
	if err != nil {
		return err
	}

	// An absent metadata table (fresh database) is legal here; a present one
	// must be a valid contiguous prefix before anything is written.
	if err := appliedPrefixOK(db); err != nil {
		return err
	}

	// Ensure the metadata table exists.
	if err := bootstrapMigrations(db); err != nil {
		return err
	}

	for _, m := range migrations {
		if err := applyOne(db, m); err != nil {
			return fmt.Errorf("migrate: version %d (%s): %w", m.Version, m.Name, err)
		}
	}

	return nil
}

// migrationTimeout bounds one migration's transaction: a hung script must
// fail rather than hold the single-writer offline window open. A real
// migration that needs longer raises this deliberately, with the reason
// recorded (cmd/migrate/AGENTS.md).
const migrationTimeout = 30 * time.Second

// applyOne applies a single migration within its own transaction.
func applyOne(db *sql.DB, m migration) error {
	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Re-check within the transaction to guard against concurrent runners.
	var existing struct {
		Name     string
		Checksum []byte
	}
	err = tx.QueryRowContext(ctx,
		"SELECT name, checksum FROM schema_migrations WHERE version = ?", m.Version,
	).Scan(&existing.Name, &existing.Checksum)

	if err == nil {
		// Row exists — verify match.
		if existing.Name != m.Name {
			return fmt.Errorf("name mismatch: stored %q, embedded %q", existing.Name, m.Name)
		}
		if string(existing.Checksum) != string(m.Checksum) {
			return fmt.Errorf("checksum mismatch: stored %x, embedded %x", existing.Checksum, m.Checksum)
		}
		// Already applied correctly — skip.
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("query existing migration: %w", err)
	}

	// Not applied yet — execute the migration script.
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("execute SQL: %w", err)
	}

	// Record the migration.
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, checksum, applied_at_ms) VALUES (?, ?, ?, ?)",
		m.Version, m.Name, m.Checksum, now,
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
