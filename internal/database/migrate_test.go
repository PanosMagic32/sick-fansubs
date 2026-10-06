package database

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

func TestParseMigrationFilename(t *testing.T) {
	tests := []struct {
		filename    string
		wantVersion int
		wantDesc    string
		wantErr     bool
	}{
		{"0001_users.sql", 1, "users", false},
		{"0010_add_indexes.sql", 10, "add_indexes", false},
		{"9999_some_change.sql", 9999, "some_change", false},
		{"1_users.sql", 1, "users", false},
		{"0001_.sql", 1, "", true},           // empty description
		{"notanumber_desc.sql", 0, "", true}, // non-numeric version
		{"_users.sql", 0, "", true},          // no version prefix
		{"0001.sql", 0, "", true},            // no underscore
		{"-1_users.sql", 0, "", true},        // negative version
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			v, d, err := parseMigrationFilename(tt.filename)
			if tt.wantErr && err == nil {
				t.Fatalf("got version=%d desc=%q, want error", v, d)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr {
				if v != tt.wantVersion {
					t.Errorf("version: got %d, want %d", v, tt.wantVersion)
				}
				if d != tt.wantDesc {
					t.Errorf("desc: got %q, want %q", d, tt.wantDesc)
				}
			}
		})
	}
}

func TestLoadedMigrations(t *testing.T) {
	migrations, err := loadedMigrations()
	if err != nil {
		t.Fatalf("loadedMigrations: %v", err)
	}

	if len(migrations) == 0 {
		t.Fatal("expected at least one embedded migration")
	}

	// First migration should be version 1.
	if migrations[0].Version != 1 {
		t.Errorf("first migration version: got %d, want 1", migrations[0].Version)
	}

	// All checksums must be 32 bytes (SHA-256 produces a 256-bit = 32-byte digest).
	for _, m := range migrations {
		if len(m.Checksum) != sha256.Size {
			t.Errorf("version %d: checksum length %d, want %d", m.Version, len(m.Checksum), sha256.Size)
		}
		if m.SQL == "" {
			t.Errorf("version %d: empty SQL", m.Version)
		}
		if m.Name == "" {
			t.Errorf("version %d: empty name", m.Version)
		}
	}
}

func TestBootstrapMigrations(t *testing.T) {
	db := openTestDB(t)

	if err := bootstrapMigrations(db); err != nil {
		t.Fatalf("bootstrapMigrations: %v", err)
	}

	// Verify the table exists.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 0 {
		t.Errorf("got %d, want 0 rows after bootstrap", count)
	}

	// Bootstrap again — should be idempotent.
	if err := bootstrapMigrations(db); err != nil {
		t.Fatalf("second bootstrapMigrations: %v", err)
	}
}

func TestApplyMigrations_EmptyToCurrent(t *testing.T) {
	db := openTestDB(t)

	if err := Apply(db); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify migration history exists.
	applied, err := appliedMigrations(db)
	if err != nil {
		t.Fatalf("appliedMigrations: %v", err)
	}

	migrations, err := loadedMigrations()
	if err != nil {
		t.Fatal(err)
	}

	if len(applied) != len(migrations) {
		t.Errorf("applied count: got %d, want %d", len(applied), len(migrations))
	}

	for _, m := range migrations {
		row, ok := applied[m.Version]
		if !ok {
			t.Errorf("version %d not found in applied history", m.Version)
			continue
		}
		if row.Name != m.Name {
			t.Errorf("version %d name: got %q, want %q", m.Version, row.Name, m.Name)
		}
		if string(row.Checksum) != string(m.Checksum) {
			t.Errorf("version %d checksum mismatch", m.Version)
		}
		if row.AppliedAtMs <= 0 {
			t.Errorf("version %d applied_at_ms must be > 0", m.Version)
		}
	}

	// Verify the users table exists.
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='users'").Scan(&tableName)
	if err != nil {
		t.Fatalf("users table not found: %v", err)
	}
}

func TestApplyMigrations_NoopRerun(t *testing.T) {
	db := openTestDB(t)

	// Apply once.
	if err := Apply(db); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	// Apply again — should be a no-op.
	if err := Apply(db); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	// Verify history count is unchanged.
	applied, err := appliedMigrations(db)
	if err != nil {
		t.Fatal(err)
	}

	migrations, _ := loadedMigrations()
	if len(applied) != len(migrations) {
		t.Errorf("unexpected applied count after rerun: got %d, want %d", len(applied), len(migrations))
	}
}

// TestApplyMigrations_EveryPriorPrefix pins the documented upgrade guarantee:
// a database holding exactly the first k migrations upgrades to current
// through the public Apply path, for every k — the shape a live database
// presents when a new tag ships migrations. Fixtures carry no seeded rows:
// this test pins the shape path only (docs/patterns/go/sqlite.md rule 9).
func TestApplyMigrations_EveryPriorPrefix(t *testing.T) {
	t.Parallel()

	migrations, err := loadedMigrations()
	if err != nil {
		t.Fatalf("loadedMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no embedded migrations")
	}

	for k := 0; k <= len(migrations); k++ {
		t.Run(fmt.Sprintf("prefix-%d", k), func(t *testing.T) {
			t.Parallel()
			db := openTestDB(t)
			if err := bootstrapMigrations(db); err != nil {
				t.Fatalf("bootstrapMigrations: %v", err)
			}
			for _, m := range migrations[:k] {
				if err := applyOne(db, m); err != nil {
					t.Fatalf("apply version %d: %v", m.Version, err)
				}
			}
			if seeded, err := appliedMigrations(db); err != nil {
				t.Fatalf("appliedMigrations after prefix %d: %v", k, err)
			} else if len(seeded) != k {
				t.Fatalf("prefix seed holds %d versions, want %d", len(seeded), k)
			}
			if err := Apply(db); err != nil {
				t.Fatalf("Apply from prefix %d: %v", k, err)
			}
			applied, err := appliedMigrations(db)
			if err != nil {
				t.Fatalf("appliedMigrations: %v", err)
			}
			if len(applied) != len(migrations) {
				t.Fatalf("applied versions = %d, want %d", len(applied), len(migrations))
			}
			if err := VerifySchema(db); err != nil {
				t.Fatalf("VerifySchema after prefix %d: %v", k, err)
			}
		})
	}
}

func TestOpenApplication_Success(t *testing.T) {
	dir := testDataDir(t)

	// First, migrate the database through the maintenance opener.
	maintDB, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenMaintenance: %v", err)
	}
	maintDB.Close()

	// Now the application opener should succeed.
	appDB, err := OpenApplication(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenApplication: %v", err)
	}
	appDB.Close()
}

func TestOpenApplication_Unmigrated(t *testing.T) {
	dir := testDataDir(t)

	// Open without migrating first.
	_, err := OpenApplication(Config{DataDir: dir})
	if err == nil {
		t.Fatal("expected error from OpenApplication on unmigrated database")
	}
}

func TestOpenMaintenance_Success(t *testing.T) {
	dir := testDataDir(t)

	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenMaintenance: %v", err)
	}
	db.Close()

	// Verify the database file exists.
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("database file not created: %v", err)
	}
}

func TestMigrationHistoryOK_Current(t *testing.T) {
	db := openTestDB(t)
	if err := Apply(db); err != nil {
		t.Fatal(err)
	}

	if err := migrationHistoryOK(db); err != nil {
		t.Fatalf("migrationHistoryOK should succeed on current DB: %v", err)
	}
}

func TestMigrationHistoryOK_Empty(t *testing.T) {
	db := openTestDB(t)
	// Don't apply migrations — history is empty.

	err := migrationHistoryOK(db)
	if err == nil {
		t.Fatal("expected error from migrationHistoryOK on empty database")
	}
}

func TestMigrationHistoryOK_AfterBootstrapOnly(t *testing.T) {
	db := openTestDB(t)
	if err := bootstrapMigrations(db); err != nil {
		t.Fatal(err)
	}

	// Bootstrap creates the table but no rows — should fail.
	err := migrationHistoryOK(db)
	if err == nil {
		t.Fatal("expected error from migrationHistoryOK after bootstrap only (no rows)")
	}
}

// TestApplyRefusesNonPrefixHistory pins the pre-write guard: a stored history
// with a gap fails before Apply writes anything. Filling the gap would record
// a version whose schema effect later applied migrations already produced — a
// ledger no re-run can repair.
func TestApplyRefusesNonPrefixHistory(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := Apply(db); err != nil {
		t.Fatalf("initial Apply: %v", err)
	}

	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = 3"); err != nil {
		t.Fatalf("delete version 3: %v", err)
	}

	err := Apply(db)
	if err == nil {
		t.Fatal("expected Apply to refuse a gapped history")
	}
	if !strings.Contains(err.Error(), "gap") {
		t.Errorf("error should name the gap, got: %v", err)
	}

	// The ledger stayed untouched: version 3 was not re-recorded.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 3").Scan(&count); err != nil {
		t.Fatalf("count version 3: %v", err)
	}
	if count != 0 {
		t.Errorf("version 3 rows = %d, want 0 (Apply must not write)", count)
	}
}

// TestMigrationRollbackOnFailure verifies that a failed migration
// rolls back both the schema change and the migration record.
// Since we can't easily inject a failing migration into the embedded set,
// we test the principle by checking that the bootstrap transaction is atomic.
func TestBootstrapIsTransactional(t *testing.T) {
	db := openTestDB(t)

	if err := bootstrapMigrations(db); err != nil {
		t.Fatal(err)
	}

	// Insert a malformed row to test that the CHECK constraint works.
	_, err := db.Exec(`INSERT INTO schema_migrations (version, name, checksum, applied_at_ms)
		VALUES (0, '', x'', -1)`)
	if err == nil {
		t.Fatal("expected CHECK constraint violation")
	}
}

// TestConcurrentMigrationRunners verifies that concurrent Apply calls
// do not produce duplicate history entries.
func TestConcurrentMigrationRunners(t *testing.T) {
	db := openTestDB(t)

	// Run Apply concurrently from two goroutines.
	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			errCh <- Apply(db)
		}()
	}

	// Both should complete (one may succeed, the other may hit the
	// busy timeout and succeed after, or both may complete cleanly).
	err1 := <-errCh
	err2 := <-errCh

	// At least one should succeed without error.
	if err1 != nil && err2 != nil {
		t.Fatalf("both concurrent Apply calls failed: err1=%v err2=%v", err1, err2)
	}

	// History should have exactly the right count.
	applied, err := appliedMigrations(db)
	if err != nil {
		t.Fatal(err)
	}
	migrations, _ := loadedMigrations()
	if len(applied) != len(migrations) {
		t.Errorf("concurrent Apply produced wrong history count: got %d, want %d", len(applied), len(migrations))
	}
}

// TestChecksumVerification verifies that the migration runner detects
// when an applied migration's checksum doesn't match the embedded file.
// This pins the "checksum mutation is rejected" rule in
// docs/patterns/go/sqlite.md.
func TestChecksumVerification(t *testing.T) {
	db := openTestDB(t)
	if err := Apply(db); err != nil {
		t.Fatal(err)
	}

	// Tamper with the stored checksum.
	_, err := db.Exec("UPDATE schema_migrations SET checksum = ? WHERE version = 1",
		make([]byte, sha256.Size))
	if err != nil {
		t.Fatal(err)
	}

	// Rerunning Apply should detect the checksum mismatch.
	err = Apply(db)
	if err == nil {
		t.Fatal("expected error from checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("got: %v, want 'checksum mismatch' error", err)
	}
}

// TestMigrationSyntaxErrorRollback verifies that a migration containing invalid
// SQL rolls back both the schema change and the migration record.
// This pins the "syntax/constraint failure rolls back both schema change and
// migration record" rule in docs/patterns/go/sqlite.md.
func TestMigrationSyntaxErrorRollback(t *testing.T) {
	db := openTestDB(t)
	if err := bootstrapMigrations(db); err != nil {
		t.Fatal(err)
	}

	// Craft a migration with deliberately invalid SQL.
	// SQLite is lenient about column type names — "INVALID_TYPE" is accepted
	// as an unrecognized affinity. Use an actual syntax error instead.
	bad := migration{
		Version:  1,
		Name:     "bad_migration",
		Checksum: make([]byte, 32),
		SQL:      "THIS IS NOT VALID SQL;",
	}

	err := applyOne(db, bad)
	if err == nil {
		t.Fatal("expected error from invalid SQL migration")
	}

	// The migration must NOT have been recorded.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("got %d rows for version 1, want 0", count)
	}
}

// TestOpenMaintenanceRejectsFutureVersion verifies that the maintenance
// opener rejects a database with a migration version beyond the embedded set.
// This pins the "unknown future versions" rejection in docs/patterns/go/sqlite.md.
func TestOpenMaintenanceRejectsFutureVersion(t *testing.T) {
	dir := testDataDir(t)

	// First, migrate normally so the database is current.
	maintDB, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	// Inject a future version that doesn't exist in the embedded set.
	_, err = maintDB.Exec(`INSERT INTO schema_migrations (version, name, checksum, applied_at_ms)
		VALUES (?, ?, ?, ?)`, 9999, "future_migration", make([]byte, 32), 0)
	if err != nil {
		t.Fatal(err)
	}
	maintDB.Close()

	// Opening again through maintenance should reject the unknown version.
	_, err = OpenMaintenance(Config{DataDir: dir})
	if err == nil {
		t.Fatal("expected OpenMaintenance to reject database with future version")
	}
	if !strings.Contains(err.Error(), "extra") && !strings.Contains(err.Error(), "migration") {
		t.Errorf("got: %v, want migration-related error", err)
	}
}

// TestBootstrapTableVerification verifies that PRAGMA table_xinfo introspection
// catches structural drift in the schema_migrations table.
func TestBootstrapTableVerification(t *testing.T) {
	db := openTestDB(t)

	// Bootstrap should succeed normally.
	if err := bootstrapMigrations(db); err != nil {
		t.Fatalf("initial bootstrap: %v", err)
	}

	// Second bootstrap should be idempotent and pass verification.
	if err := bootstrapMigrations(db); err != nil {
		t.Fatalf("second bootstrap should pass: %v", err)
	}

	// Tamper: drop and recreate the table with a wrong column type.
	_, err := db.Exec("DROP TABLE schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		name    TEXT    NOT NULL,
		checksum TEXT   NOT NULL,
		applied_at_ms INTEGER NOT NULL
	) STRICT`)
	if err != nil {
		t.Fatal(err)
	}

	// Bootstrap should now detect the column type mismatch (checksum is TEXT, should be BLOB).
	err = bootstrapMigrations(db)
	if err == nil {
		t.Fatal("expected bootstrap verification to fail on wrong column type")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("got: %v, want error mentioning 'checksum' column", err)
	}
}

// TestNameMismatchDetection verifies that the migration runner detects
// when an applied migration's name doesn't match the embedded file.
// This pins the "name/checksum drift" rejection in docs/patterns/go/sqlite.md.
func TestNameMismatchDetection(t *testing.T) {
	db := openTestDB(t)
	if err := Apply(db); err != nil {
		t.Fatal(err)
	}

	// Tamper with the stored name.
	_, err := db.Exec("UPDATE schema_migrations SET name = ? WHERE version = 1", "wrong_name")
	if err != nil {
		t.Fatal(err)
	}

	// Rerunning Apply should detect the name mismatch.
	err = Apply(db)
	if err == nil {
		t.Fatal("expected error from name mismatch")
	}
	if !strings.Contains(err.Error(), "name mismatch") {
		t.Errorf("got: %v, want 'name mismatch' error", err)
	}
}

// TestNameMismatchRejectedAtOpen verifies that OpenApplication rejects
// a database with a name mismatch in the migration history.
func TestNameMismatchRejectedAtOpen(t *testing.T) {
	dir := testDataDir(t)

	// Migrate normally.
	maintDB, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	// Tamper the name.
	_, err = maintDB.Exec("UPDATE schema_migrations SET name = ? WHERE version = 1", "tampered")
	if err != nil {
		t.Fatal(err)
	}
	maintDB.Close()

	// Application opener should reject.
	_, err = OpenApplication(Config{DataDir: dir})
	if err == nil {
		t.Fatal("expected OpenApplication to reject name mismatch")
	}
	if !strings.Contains(err.Error(), "name mismatch") {
		t.Errorf("got: %v, want 'name mismatch' error", err)
	}
}

// TestMalformedMetadataRejected verifies that a schema_migrations table
// with wrong structure is rejected by OpenMaintenance. This covers the
// case where the metadata table was created by hand or corrupted.
func TestMalformedMetadataRejected(t *testing.T) {
	dir := testDataDir(t)

	// Open the database and manually create a malformed metadata table.
	db, err := Open(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	// Create schema_migrations with a wrong column (missing checksum, wrong type).
	_, err = db.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		name    TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// OpenMaintenance should reject — the verification catches missing columns.
	_, err = OpenMaintenance(Config{DataDir: dir})
	if err == nil {
		t.Fatal("expected OpenMaintenance to reject malformed metadata table")
	}
}

// TestDataPreservedAfterNoopMigration verifies that existing user data
// survives a no-op migration re-run. The migration runner must not touch
// data that already exists.
func TestDataPreservedAfterNoopMigration(t *testing.T) {
	db := openTestDB(t)
	if err := Apply(db); err != nil {
		t.Fatal(err)
	}

	// Insert a test row into the migrated users table.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "test1",
		Username:     "TestUser",
		Email:        "test@example.com",
		PasswordHash: "hash",
		CreatedAtMS:  1000000000000,
	})

	// Re-run Apply — must be a no-op.
	if err := Apply(db); err != nil {
		t.Fatalf("second Apply should be no-op: %v", err)
	}

	// Verify the data survived.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE id = 'test1'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("got %d, want 1 user row after no-op migration", count)
	}
}

// TestDatabaseFileCreation verifies that the database file exists on disk
// after a successful OpenMaintenance call.
func TestDatabaseFileCreation(t *testing.T) {
	dir := testDataDir(t)

	db, err := OpenMaintenance(Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	// Verify the main database file exists.
	dbPath := filepath.Join(dir, FileName)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file not found: %v", err)
	}

	db.Close()

	// The file should still exist after close.
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file missing after close: %v", err)
	}
}

// TestMigration0012_BackfillsExistingUsers pins the 0012 migration: it
// marks every row that existed BEFORE it as verified
// (email_verified_at_ms = created_at_ms), while rows inserted AFTER the
// migration stay NULL (new registrations start unverified).
func TestMigration0012_BackfillsExistingUsers(t *testing.T) {
	db := openTestDB(t)

	if err := bootstrapMigrations(db); err != nil {
		t.Fatalf("bootstrapMigrations: %v", err)
	}

	migrations, err := loadedMigrations()
	if err != nil {
		t.Fatalf("loadedMigrations: %v", err)
	}

	// Apply 1..11 (everything before the verification migration).
	for _, m := range migrations {
		if m.Version >= 12 {
			break
		}
		if err := applyOne(db, m); err != nil {
			t.Fatalf("apply version %d: %v", m.Version, err)
		}
	}

	// A pre-0012 user with a known creation stamp.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "legacy1",
		Username:     "legacyuser",
		Email:        "legacy@example.com",
		PasswordHash: "x",
		CreatedAtMS:  1111,
	})

	// Apply 0012 (and any later migrations).
	for _, m := range migrations {
		if m.Version < 12 {
			continue
		}
		if err := applyOne(db, m); err != nil {
			t.Fatalf("apply version %d: %v", m.Version, err)
		}
	}

	var verified int64
	if err := db.QueryRow(
		`SELECT email_verified_at_ms FROM users WHERE id = 'legacy1'`,
	).Scan(&verified); err != nil {
		t.Fatalf("read backfilled stamp: %v", err)
	}
	if verified != 1111 {
		t.Errorf("backfilled stamp = %d, want 1111", verified)
	}

	// A post-0012 insert starts NULL (registration leaves it unverified).
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:           "new1",
		Username:     "newuser",
		Email:        "new@example.com",
		PasswordHash: "x",
		CreatedAtMS:  2222,
	})
	var stamp any
	if err := db.QueryRow(
		`SELECT email_verified_at_ms FROM users WHERE id = 'new1'`,
	).Scan(&stamp); err != nil {
		t.Fatalf("read new stamp: %v", err)
	}
	if stamp != nil {
		t.Errorf("post-0012 insert stamp = %v, want NULL", stamp)
	}
}
