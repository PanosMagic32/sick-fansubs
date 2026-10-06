// Package database provides SQLite database access for the Sick-Fansubs
// application: DSN (Data Source Name) construction, the connection pool, WAL
// (Write-Ahead Log) initialization, pragma verification, schema migrations,
// and the readiness check. It owns no HTTP, business-logic, or domain-model
// concerns. The detailed contracts live in internal/database/AGENTS.md.
//
// # Design principles
//
//   - The modernc.org/sqlite driver through its connector — pure Go, no CGO.
//   - DSN constructed from a data directory and a fixed filename with net/url,
//     never from an untrusted raw connection string.
//   - Four-connection pool with connection-level pragmas enforced at open:
//     foreign_keys, busy_timeout(5000), synchronous(FULL), trusted_schema(OFF),
//     _txlock=immediate, _dqs=0.
//   - WAL mode set explicitly and verified after open.
//   - Two migration-aware openers: the strict application opener (exact
//     migration history) and the maintenance opener (permits absent or
//     older-prefix history).
//   - Migrations are embedded forward .sql files with SHA-256 (Secure Hash
//     Algorithm, 256-bit) checksums.
//
// The driver is [modernc.org/sqlite]; statements run through [database/sql].
package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"modernc.org/sqlite"
)

// FileName is the fixed database filename appended to the data directory.
const FileName = "sick-fansubs.db"

// Pool settings.
const (
	maxOpenConns = 4
	maxIdleConns = 4
)

// healthCheckTimeout bounds one readiness check's work. The probe's caller
// context cancels earlier; this is the fallback ceiling.
const healthCheckTimeout = 10 * time.Second

// Config holds the runtime database configuration.
type Config struct {
	// DataDir is the absolute path to the directory containing the database file.
	// It must exist, be owner-only (POSIX), and be writable by the current process.
	DataDir string
}

// buildDSN constructs a file: URI (Uniform Resource Identifier) from the
// data directory and fixed filename.
//
// It resolves the path to absolute before constructing the URI (modernc/sqlite
// requires an absolute path in the file: scheme); a resolution failure is an error.
//
// The resulting DSN (Data Source Name) looks like:
//
//	file:/absolute/path/to/data/sick-fansubs.db?_pragma=foreign_keys(ON)&...
func buildDSN(dataDir string) (string, error) {
	absPath, err := filepath.Abs(filepath.Join(dataDir, FileName))
	if err != nil {
		return "", fmt.Errorf("database: resolve database path: %w", err)
	}
	return buildDSNForFile(absPath), nil
}

// openDSN returns a pool for dsn through the driver's connector, so the pool
// binds to the driver value instead of looking one up by registered name. A
// malformed query string is rejected here; everything else fails at connect.
func openDSN(dsn string) (*sql.DB, error) {
	connector, err := sqlite.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

// openDB validates the data directory, constructs the DSN, opens the pool,
// configures pool limits, pings, and sets up WAL mode.
//
// It returns the opened *sql.DB and any error. Callers are responsible for
// closing the database when done.
func openDB(dataDir string) (*sql.DB, error) {
	// Validate the data directory exists and is a directory.
	info, err := os.Stat(dataDir)
	if err != nil {
		return nil, fmt.Errorf("database: data directory %q: %w", dataDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("database: data directory %q is not a directory", dataDir)
	}
	// The directory is the protection boundary for the database file and its
	// sidecars: group/other access is refused, never silently accepted. POSIX
	// modes do not exist on Windows, so the check is skipped there.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			return nil, fmt.Errorf("database: data directory %q is not owner-only (mode %04o)", dataDir, perm)
		}
	}
	// A missing main file beside interrupted-rollback remnants must fail before
	// the connection below would create an empty database in its place.
	if err := ensureNoInterruptedRollback(dataDir); err != nil {
		return nil, err
	}

	dsn, err := buildDSN(dataDir)
	if err != nil {
		return nil, err
	}

	db, err := openDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("database: open: %w", err)
	}

	// Configure pool before any workload begins.
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)

	// Verify connectivity with a bounded timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	// Set and verify WAL journal mode.
	// WAL is a persistent database setting — once set, it survives reopens.
	// We do this on a dedicated connection to avoid affecting the pool state.
	if err := ensureWAL(db); err != nil {
		db.Close()
		return nil, err
	}

	// Verify connection-level settings on a real connection.
	if err := verifyConnSettings(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// ensureWAL sets journal_mode=WAL (Write-Ahead Log) and verifies the returned mode.
//
// WAL allows concurrent readers alongside a single writer, improving
// performance for read-heavy workloads. It is a persistent setting —
// once set, it survives database reopens.
//
// We use a dedicated single connection (not from the pool) so that the
// journal_mode change is applied exactly once and verified independently.
func ensureWAL(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("database: acquire WAL connection: %w", err)
	}
	defer conn.Close()

	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return fmt.Errorf("database: set WAL: %w", err)
	}
	if mode != "wal" {
		return fmt.Errorf("database: expected journal_mode=wal, got %q", mode)
	}
	return nil
}

// verifyConnSettings checks that required connection-level pragmas are active.
//
// PRAGMA statements are SQLite-specific commands that query or modify the
// database engine's behavior (as opposed to data manipulation). They control
// settings like foreign key enforcement, busy timeout, durability, etc.
//
// This function acquires a dedicated connection and runs PRAGMA queries to
// verify that all required settings are in effect. If any setting is incorrect,
// it returns an error describing the mismatch.
//
// Tests must hold multiple sql.Conn values concurrently to prove that settings
// apply to every connection, not just the first one.
func verifyConnSettings(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("database: acquire verify connection: %w", err)
	}
	defer conn.Close()

	checks := []struct {
		query    string
		expected string
		label    string
	}{
		{"PRAGMA foreign_keys", "1", "foreign_keys"},
		{"PRAGMA busy_timeout", "5000", "busy_timeout"},
		{"PRAGMA synchronous", "2", "synchronous"}, // 2 = FULL
		{"PRAGMA trusted_schema", "0", "trusted_schema"},
		// _dqs and _txlock are driver-level parameters (set in the DSN),
		// not SQLite PRAGMA statements. They cannot be queried via PRAGMA.
		// _dqs=0 is tested implicitly: a double-quoted string like "hello"
		// would cause a syntax error rather than being treated as a literal.
		// _txlock=immediate is tested through lock-behavior observation.
	}

	for _, c := range checks {
		var val string
		if err := conn.QueryRowContext(ctx, c.query).Scan(&val); err != nil {
			return fmt.Errorf("database: verify %s: %w", c.label, err)
		}
		if val != c.expected {
			return fmt.Errorf("database: %s: expected %q, got %q", c.label, c.expected, val)
		}
	}

	return nil
}

// Open opens a SQLite database with full pool configuration, WAL mode,
// and connection-setting verification. It does not check migration history.
//
// Use OpenApplication or OpenMaintenance for migration-aware opening.
// Use this directly only for ad-hoc operations (e.g., shell, diagnostics).
func Open(config Config) (*sql.DB, error) {
	return openDB(config.DataDir)
}

// DBHealthChecker adapts *sql.DB to the handler.HealthChecker interface. The
// interface is defined in the handler package (the consumer), and this struct
// satisfies it structurally — no import needed.
type DBHealthChecker struct {
	DB      *sql.DB
	DataDir string
}

// Check reports whether the database is ready to serve: the restore-marker
// gate, a ping, the migration-history check, and schema verification, all
// under the caller's context bounded by healthCheckTimeout. The full contract
// lives in internal/database/AGENTS.md.
func (c DBHealthChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	if err := ensureNoRestoreMarker(c.DataDir); err != nil {
		return err
	}
	if err := c.DB.PingContext(ctx); err != nil {
		return fmt.Errorf("database: health ping: %w", err)
	}
	if err := migrationHistoryOKCtx(ctx, c.DB); err != nil {
		return fmt.Errorf("database: migration history: %w", err)
	}
	if err := verifySchemaCtx(ctx, c.DB); err != nil {
		return fmt.Errorf("database: schema verification: %w", err)
	}
	return nil
}

// CloseDatabase closes the database pool.
//
// It is safe to call with nil or on an already-closed database.
// database/sql's Close() is idempotent — subsequent calls return nil.
func CloseDatabase(db *sql.DB) error {
	if db == nil {
		return nil
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("database: close: %w", err)
	}
	return nil
}
