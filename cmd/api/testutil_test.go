package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store"
)

// writeMainMedia writes a tiny media file for one 32-hex id into the data
// dir (sweep-wiring fixtures).
func writeMainMedia(t *testing.T, dataDir, id string) {
	t.Helper()
	full := filepath.Join(dataDir, media.RelativePath(id, media.ExtJPG))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir media fixture: %v", err)
	}
	if err := os.WriteFile(full, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}
}

// openMainTestDB creates a temporary migrated SQLite database for testing
// the cmd/api lifecycle helpers.
func openMainTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustCreateMainSession(t *testing.T, db *sql.DB, id, userID string, expiresAtMS int64) {
	t.Helper()
	digest := make([]byte, 32)
	digest[0] = id[len(id)-1] // vary per session (sess_01..sess_04 → '1'..'4')
	csrf := make([]byte, 32)
	err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
		ID:          id,
		UserID:      userID,
		TokenDigest: digest,
		CSRF:        csrf,
		AuthVersion: 1,
		CreatedAtMS: 1000,
		ExpiresAtMS: expiresAtMS,
	})
	if err != nil {
		t.Fatalf("CreateSession(%q): %v", id, err)
	}
}

// mustInsertMainAuditEvent seeds one audit_events row (the cleanup
// fixtures — request_id/remote_addr are NOT NULL but carry no meaning here).
func mustInsertMainAuditEvent(t *testing.T, db *sql.DB, id string, createdAtMS int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO audit_events (id, event, result, request_id, remote_addr, created_at_ms)
		VALUES (?, 'role_changed', 'success', '', '', ?)`, id, createdAtMS); err != nil {
		t.Fatalf("insert audit event %q: %v", id, err)
	}
}

// decodeLogRecords parses each JSON log line into a generic record, so tests
// assert decoded fields rather than serialized bytes
// (docs/patterns/go/testing.md).
func decodeLogRecords(t *testing.T, body string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

// testDataDir returns an owner-only temporary directory for tests that open a
// database: database.Open refuses a data directory with group or other access,
// and t.TempDir can inherit wider bits from the environment.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod test data dir: %v", err)
	}
	return dir
}
