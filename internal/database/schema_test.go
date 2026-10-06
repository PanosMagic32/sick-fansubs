package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// The schema-verification suite: the live schema must match the Go-declared
// schemaSpec exactly, fail-closed on drift, and the migration metadata rows
// must satisfy their invariants even when the CHECK constraints were
// bypassed by tampering.

func mustApplySchema(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	if err := Apply(db); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return db
}

func TestVerifySchema_PassesOnFreshMigration(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if err := VerifySchema(db); err != nil {
		t.Fatalf("VerifySchema on a freshly migrated database: %v", err)
	}
}

func TestVerifySchema_DeclaredPartialIndexPasses(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	// migration 0010 declares idx_user_notifications_unread as a partial
	// index; VerifySchema must accept it.
	if err := VerifySchema(db); err != nil {
		t.Fatalf("VerifySchema with the declared partial index: %v", err)
	}
}

func TestVerifySchema_UndeclaredPartialIndex(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	// A partial index that no expectation declares must fail verification.
	if _, err := db.Exec("CREATE INDEX idx_hand_added_partial ON users(username) WHERE status = 'active'"); err != nil {
		t.Fatalf("create stray partial index: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for an undeclared partial index")
	}
	if !strings.Contains(err.Error(), "idx_hand_added_partial") {
		t.Errorf("error should name the stray index, got: %v", err)
	}
}

func TestVerifySchema_PartialFlagMustMatch(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	// Drop the declared partial index and recreate it WITHOUT the WHERE
	// clause: the expectation declares partial, so the live non-partial
	// index must fail the match — drift in either direction is caught.
	if _, err := db.Exec("DROP INDEX idx_user_notifications_unread"); err != nil {
		t.Fatalf("drop declared partial index: %v", err)
	}
	if _, err := db.Exec("CREATE INDEX idx_user_notifications_unread ON user_notifications(recipient_id, created_at_ms DESC)"); err != nil {
		t.Fatalf("recreate index without WHERE: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure when the partial flag drifts")
	}
	if !strings.Contains(err.Error(), "idx_user_notifications_unread") {
		t.Errorf("error should name the drifted index, got: %v", err)
	}
}

func TestVerifySchema_MissingTable(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if _, err := db.Exec("DROP TABLE blog_posts"); err != nil {
		t.Fatalf("drop blog_posts: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for a missing table")
	}
	if !strings.Contains(err.Error(), "blog_posts") {
		t.Errorf("error should name the missing table, got: %v", err)
	}
}

func TestVerifySchema_StrayTable(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if _, err := db.Exec("CREATE TABLE hand_added (id TEXT)"); err != nil {
		t.Fatalf("create stray table: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for an unexpected table")
	}
	if !strings.Contains(err.Error(), "hand_added") {
		t.Errorf("error should name the stray table, got: %v", err)
	}
}

func TestVerifySchema_NonStrictReplacement(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	// Recreate an expected table WITHOUT the STRICT keyword — the same
	// name, so the table-existence probe passes but the strict check must fail.
	if _, err := db.Exec("DROP TABLE sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE sessions (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("recreate sessions: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for a non-STRICT table")
	}
	if !strings.Contains(err.Error(), "not STRICT") {
		t.Errorf("error should report the STRICT failure, got: %v", err)
	}
}

func TestVerifySchema_ColumnDrift(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if _, err := db.Exec("ALTER TABLE users ADD COLUMN hand_col TEXT"); err != nil {
		t.Fatalf("add stray column: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for an unexpected column")
	}
	if !strings.Contains(err.Error(), "hand_col") {
		t.Errorf("error should name the stray column, got: %v", err)
	}
}

func TestVerifySchema_MissingIndex(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if _, err := db.Exec("DROP INDEX idx_sessions_user_id"); err != nil {
		t.Fatalf("drop index: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for a missing index")
	}
	if !strings.Contains(err.Error(), "idx_sessions_user_id") {
		t.Errorf("error should name the missing index, got: %v", err)
	}
}

func TestVerifySchema_StrayIndex(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if _, err := db.Exec("CREATE INDEX idx_hand ON users(username)"); err != nil {
		t.Fatalf("create stray index: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for an unexpected index")
	}
}

func TestVerifySchema_IndexDirectionDrift(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	// Same name and columns, wrong direction: the schema declares
	// (recipient_id, created_at_ms DESC, id) — a hand-flipped ASC rebuild
	// must fail closed like any other drift.
	if _, err := db.Exec("DROP INDEX idx_user_notifications_recipient_created"); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if _, err := db.Exec("CREATE INDEX idx_user_notifications_recipient_created ON user_notifications(recipient_id, created_at_ms, id)"); err != nil {
		t.Fatalf("recreate ASC index: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for an index direction drift")
	}
}

// TestVerifySchema_MissingFTSTable proves the FTS virtual tables are part
// of the verified contract: dropping one fails verification.
func TestVerifySchema_MissingFTSTable(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if _, err := db.Exec("DROP TABLE blog_post_search"); err != nil {
		t.Fatalf("drop blog_post_search: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for a missing FTS table")
	}
}

// TestVerifySchema_RealTableWhereFTSExpected proves the fts marker is
// checked, not just table presence: a plain table under an FTS name is
// drift.
func TestVerifySchema_RealTableWhereFTSExpected(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	if _, err := db.Exec("DROP TABLE blog_post_search"); err != nil {
		t.Fatalf("drop blog_post_search: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE blog_post_search (x TEXT)"); err != nil {
		t.Fatalf("recreate as plain table: %v", err)
	}

	err := VerifySchema(db)
	if err == nil {
		t.Fatal("expected verification failure for a non-virtual blog_post_search")
	}
}

// tamperRow rewrites one schema_migrations column value with the CHECK
// constraints disabled, simulating a hand-built or corrupted metadata table.
func tamperRow(t *testing.T, db *sql.DB, setClause string, args ...any) {
	t.Helper()
	ctx := context.Background()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatalf("disable check constraints: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "UPDATE schema_migrations SET "+setClause+" WHERE version = 1", args...); err != nil {
		t.Fatalf("tamper row: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatalf("re-enable check constraints: %v", err)
	}
}

func TestMigrationHistoryOK_TamperedChecksumLength(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	// docs/patterns/go/sqlite.md: row invariants are validated in Go, not only
	// by the CHECK constraints a tampered table can bypass.
	tamperRow(t, db, "checksum = ?", []byte{0x01})

	err := migrationHistoryOK(db)
	if err == nil {
		t.Fatal("expected history failure for a tampered checksum length")
	}
	if !strings.Contains(err.Error(), "checksum length") {
		t.Errorf("error should name the invariant, got: %v", err)
	}
}

func TestMigrationHistoryOK_TamperedTimestamp(t *testing.T) {
	t.Parallel()
	db := mustApplySchema(t)

	tamperRow(t, db, "applied_at_ms = ?", int64(-5))

	err := migrationHistoryOK(db)
	if err == nil {
		t.Fatal("expected history failure for a negative applied_at_ms")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error should name the invariant, got: %v", err)
	}
}

func TestAppliedMigrations_NoTableIsEmptyNotError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	applied, err := appliedMigrations(db)
	if err != nil {
		t.Fatalf("absent metadata table must not be an error: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("got %d rows, want empty history for a pre-bootstrap database", len(applied))
	}
}

func TestAppliedMigrations_RealErrorsPropagate(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// A table with the RIGHT name but the WRONG shape is a real drift, not
	// "no table": it must propagate instead of masquerading as "needs
	// migration".
	if _, err := db.Exec("CREATE TABLE schema_migrations (version INTEGER)"); err != nil {
		t.Fatalf("create drifted metadata table: %v", err)
	}

	_, err := appliedMigrations(db)
	if err == nil {
		t.Fatal("expected a drifted schema_migrations shape to be a real error")
	}
}
