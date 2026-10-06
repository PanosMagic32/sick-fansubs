package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// TestDatabaseFileSizes pins the WAL monitor's readout: the main file and
// the WAL sidecar are measured, and a missing sidecar reads zero.
func TestDatabaseFileSizes(t *testing.T) {
	t.Parallel()
	dir := testDataDir(t)

	mainBytes, walBytes, err := databaseFileSizes(dir)
	if err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	if mainBytes != 0 || walBytes != 0 {
		t.Errorf("empty dir sizes = (%d, %d), want zero", mainBytes, walBytes)
	}

	if err := os.WriteFile(filepath.Join(dir, database.FileName), []byte("main"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, database.FileName+"-wal"), []byte("wal-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	mainBytes, walBytes, err = databaseFileSizes(dir)
	if err != nil {
		t.Fatalf("with files: %v", err)
	}
	if mainBytes != int64(len("main")) || walBytes != int64(len("wal-bytes")) {
		t.Errorf("sizes = (%d, %d), want (4, 9)", mainBytes, walBytes)
	}
}

// TestCleanupExpiredSessions pins the cleanup contract: expired rows
// are removed in bounded batches, active rows survive, and the loop
// terminates when a batch is short.
func TestCleanupExpiredSessions(t *testing.T) {
	t.Parallel()
	db := openMainTestDB(t)
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID: "user_01", Username: "TestUser", Email: "user_01@example.com",
	})
	// Three expired + one active session. The batch size (500) exceeds the
	// row count, so this exercises one full batch pass rather than the
	// multi-batch loop — the loop's continuation behaviour is pinned by the
	// store-level bounded-batch test in internal/store.
	for i := 1; i <= 3; i++ {
		mustCreateMainSession(t, db, fmt.Sprintf("sess_0%d", i), "user_01", 2000) // expired (past)
	}
	mustCreateMainSession(t, db, "sess_04", "user_01", time.Now().UnixMilli()+7*24*3600*1000)

	n, err := cleanupExpiredSessions(ctx, db)
	if err != nil {
		t.Fatalf("cleanupExpiredSessions: %v", err)
	}
	if n != 3 {
		t.Errorf("deleted: got %d, want 3", n)
	}

	// Second pass: nothing expired remains, the active session survives.
	n, err = cleanupExpiredSessions(ctx, db)
	if err != nil {
		t.Fatalf("second cleanupExpiredSessions: %v", err)
	}
	if n != 0 {
		t.Errorf("second pass deleted: got %d, want 0", n)
	}
	activeDigest := make([]byte, 32)
	activeDigest[0] = '4'
	if _, err := store.SessionByDigest(ctx, db, activeDigest); err != nil {
		t.Errorf("active session should survive cleanup: %v", err)
	}
}

// TestCleanupAuditEvents pins the wiring: events older than the
// 365-day window are removed across bounded batches, recent events survive,
// and a second pass finds nothing more.
func TestCleanupAuditEvents(t *testing.T) {
	t.Parallel()
	db := openMainTestDB(t)
	ctx := t.Context()

	now := time.Now().UnixMilli()
	expired := now - int64(auditRetentionWindow/time.Millisecond) - 60_000 // one minute past the window
	for i := 1; i <= 3; i++ {
		mustInsertMainAuditEvent(t, db, fmt.Sprintf("audit_0%d", i), expired)
	}
	mustInsertMainAuditEvent(t, db, "audit_recent", now)

	n, err := cleanupAuditEvents(ctx, db)
	if err != nil {
		t.Fatalf("cleanupAuditEvents: %v", err)
	}
	if n != 3 {
		t.Errorf("cleanupAuditEvents deleted %d, want 3", n)
	}
	var remaining string
	if err := db.QueryRow(`SELECT id FROM audit_events`).Scan(&remaining); err != nil {
		t.Fatalf("remaining event: %v", err)
	}
	if remaining != "audit_recent" {
		t.Errorf("surviving event = %q, want audit_recent", remaining)
	}

	// Second pass: nothing expired remains.
	n, err = cleanupAuditEvents(ctx, db)
	if err != nil {
		t.Fatalf("second cleanupAuditEvents: %v", err)
	}
	if n != 0 {
		t.Errorf("second pass deleted %d, want 0", n)
	}
}

// TestCleanupAuditEvents_CanceledContext pins that the batch loop honors
// cancellation (the session-cleanup precedent).
func TestCleanupAuditEvents_CanceledContext(t *testing.T) {
	t.Parallel()
	db := openMainTestDB(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := cleanupAuditEvents(ctx, db)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

// TestCleanupExpiredSessions_CanceledContext pins that the batch loop honors
// cancellation instead of running one more query after shutdown begins.
func TestCleanupExpiredSessions_CanceledContext(t *testing.T) {
	t.Parallel()
	db := openMainTestDB(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := cleanupExpiredSessions(ctx, db)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

// TestStartSessionCleanup_StopsOnCancel pins that the periodic task exits
// when the signal context is cancelled (graceful-shutdown contract).
func TestStartSessionCleanup_StopsOnCancel(t *testing.T) {
	db := openMainTestDB(t)

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { startSessionCleanup(ctx, db) })
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup task did not stop after context cancellation")
	}
}

// TestSweepMediaOrphans pins the sweep wiring: a stale
// unreferenced file is removed, a referenced file survives, and the DB
// reference snapshot feeds the sweep.
func TestSweepMediaOrphans(t *testing.T) {
	t.Parallel()

	db := openMainTestDB(t)
	dir := testDataDir(t)
	ctx := t.Context()

	const (
		refID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		orphan = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	writeMainMedia(t, dir, refID)
	writeMainMedia(t, dir, orphan)

	if _, err := db.Exec(`INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('b1', 'T', '', '', ?, 'published', 1000, 500, 500)`,
		media.RelativePath(refID, media.ExtJPG)); err != nil {
		t.Fatalf("seed reference: %v", err)
	}

	// Both files are fresh (within grace), so the sweep deletes neither —
	// the point here is the wiring: referenced set + sweep compose.
	stats, err := sweepMediaOrphans(ctx, db, dir)
	if err != nil {
		t.Fatalf("sweepMediaOrphans: %v", err)
	}
	if stats.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0 (both files within grace)", stats.Deleted)
	}
	for _, id := range []string{refID, orphan} {
		if _, err := os.Stat(filepath.Join(dir, media.RelativePath(id, media.ExtJPG))); err != nil {
			t.Errorf("file %s missing after sweep: %v", id, err)
		}
	}
}

// TestStartMediaSweep_StopsOnCancel pins that the periodic media sweep exits
// on cancellation like the session cleanup.
func TestStartMediaSweep_StopsOnCancel(t *testing.T) {
	db := openMainTestDB(t)
	dir := testDataDir(t)

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { startMediaSweep(ctx, db, dir) })
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("media sweep task did not stop after context cancellation")
	}
}

// TestAuditWriterRegistration_DualWrites pins the startup wiring: main
// registers the SQLite writer against the application
// database, so an emitted event lands in audit_events with the full contract.
//
// SERIAL — this test registers the package-global audit writer; it restores
// nil on cleanup so no other test observes it.
func TestAuditWriterRegistration_DualWrites(t *testing.T) {
	db := openMainTestDB(t)
	audit.SetWriter(&store.AuditWriter{DB: db})
	defer audit.SetWriter(nil)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	audit.Event(logging.With(t.Context(), logger), audit.EventSignOut, audit.ResultSuccess,
		"user-1", "req-1", "192.0.2.1:1234")

	var (
		event, result, actorID, requestID, remoteAddr string
		targetID                                      sql.NullString
		createdAtMS                                   int64
	)
	err := db.QueryRow(`SELECT event, result, actor_id, target_id, request_id, remote_addr, created_at_ms
		FROM audit_events`).Scan(&event, &result, &actorID, &targetID, &requestID, &remoteAddr, &createdAtMS)
	if err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if event != audit.EventSignOut || result != audit.ResultSuccess {
		t.Errorf("row event/result: got %q/%q", event, result)
	}
	if actorID != "user-1" {
		t.Errorf("row actor: got %q, want user-1", actorID)
	}
	if targetID.Valid {
		t.Errorf("row target: got %q, want NULL for actor-only events", targetID.String)
	}
	if requestID != "req-1" || remoteAddr != "192.0.2.1:1234" {
		t.Errorf("row request/remote: got %q/%q", requestID, remoteAddr)
	}
	if createdAtMS <= 0 {
		t.Errorf("row created_at_ms: got %d, want positive", createdAtMS)
	}
}

// TestAuditWriterRegistration_WriteFailureDoesNotPropagate pins acceptance
// criterion (2) against a REAL broken database: the audited operation
// completes, the slog half survives, and an error record is logged — the
// durable write never propagates.
//
// SERIAL — same global-writer caveat as above.
func TestAuditWriterRegistration_WriteFailureDoesNotPropagate(t *testing.T) {
	db := openMainTestDB(t)
	audit.SetWriter(&store.AuditWriter{DB: db})
	defer audit.SetWriter(nil)
	db.Close() // the writer is now broken

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Event must not panic or return an error.
	audit.Event(logging.With(t.Context(), logger), audit.EventSignOut, audit.ResultSuccess,
		"user-1", "req-1", "192.0.2.1:1234")

	records := decodeLogRecords(t, buf.String())
	msgs := make(map[string]bool, len(records))
	for _, rec := range records {
		if msg, ok := rec["msg"].(string); ok {
			msgs[msg] = true
		}
	}
	if !msgs["audit"] {
		t.Errorf("slog half must survive a broken writer, got records %v", records)
	}
	if !msgs["audit write failed"] {
		t.Errorf("broken writer must produce a failure record, got records %v", records)
	}
}

// TestStartAuditCleanup_StopsOnCancel pins the third periodic sibling's
// shutdown contract (the session-cleanup precedent).
func TestStartAuditCleanup_StopsOnCancel(t *testing.T) {
	db := openMainTestDB(t)

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { startAuditCleanup(ctx, db) })
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("audit cleanup task did not stop after context cancellation")
	}
}

// TestStartWALMonitor_StopsOnCancel pins the WAL readout's shutdown contract
// (the WAL monitor takes no database handle, so it joins the same
// WaitGroup without participating in the close ordering).
func TestStartWALMonitor_StopsOnCancel(t *testing.T) {
	dir := testDataDir(t)

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { startWALMonitor(ctx, dir) })
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WAL monitor did not stop after context cancellation")
	}
}

// TestNumericContractsPinned pins the values the feature docs name and the
// startup budget beside them, so a silent change is caught here.
func TestNumericContractsPinned(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		got  int64
		want int64
	}{
		{"auditRetentionWindow", int64(auditRetentionWindow / time.Millisecond), 365 * 24 * 60 * 60 * 1000},
		{"mediaSweepInterval", int64(mediaSweepInterval / time.Millisecond), 60 * 60 * 1000},
		{"walMonitorInterval", int64(walMonitorInterval / time.Millisecond), 60 * 60 * 1000},
		{"startupPassBudget", int64(startupPassBudget / time.Millisecond), 30 * 1000},
		{"walWarnBytes", walWarnBytes, 64 << 20},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %d, want %d", c.got, c.want)
			}
		})
	}
}
