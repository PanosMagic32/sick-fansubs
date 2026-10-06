package store

import (
	"database/sql"
	"testing"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/store/storetest"
)

// isHex reports whether c is a lowercase hex digit.
func isHex(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
}

// seedAuditRow inserts one ledger row with the fields the browser reads.
func seedAuditRow(t *testing.T, db *sql.DB, id, event, result, actorID, targetID, targetRole string, createdAtMS int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO audit_events
		(id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, event, result, nullableString(actorID), nullableString(targetID), nullableString(targetRole),
		"req-"+id, "192.0.2.1", createdAtMS); err != nil {
		t.Fatalf("seed audit row %s: %v", id, err)
	}
}

// TestListAuditEvents_NewestFirstFilteredAndBounded pins the browser's read
// contract: newest first, an exact-event filter that the row set actually
// discriminates, the cap, and NULL identities surfacing as empty strings.
func TestListAuditEvents_NewestFirstFilteredAndBounded(t *testing.T) {
	db := openStoreDB(t)
	seedAuditRow(t, db, "e1", audit.EventSignInFailure, audit.ResultFailure, "", "", "", 1_000)
	seedAuditRow(t, db, "e2", audit.EventRoleChanged, audit.ResultSuccess, "actor", "target", "user", 2_000)
	seedAuditRow(t, db, "e3", audit.EventSignInFailure, audit.ResultFailure, "", "", "", 3_000)

	all, err := ListAuditEvents(t.Context(), db, AuditEventFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("rows = %d, want 3", len(all))
	}
	if all[0].ID != "e3" || all[1].ID != "e2" || all[2].ID != "e1" {
		t.Errorf("order = %s, %s, %s — want newest first", all[0].ID, all[1].ID, all[2].ID)
	}

	// The NULL identity columns come back as empty strings, not scan errors.
	if all[0].ActorID != "" || all[0].TargetID != "" || all[0].TargetRole != "" {
		t.Errorf("actor-only row = %+v, want empty identities", all[0])
	}
	if all[0].RemoteAddr != "192.0.2.1" || all[0].RequestID != "req-e3" {
		t.Errorf("forensic fields = %q/%q, want the stored values", all[0].RemoteAddr, all[0].RequestID)
	}
	if all[1].ActorID != "actor" || all[1].TargetRole != "user" {
		t.Errorf("target row = %+v, want the stored identities", all[1])
	}

	filtered, err := ListAuditEvents(t.Context(), db, AuditEventFilter{Event: audit.EventSignInFailure, Limit: 10})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 2 || filtered[0].ID != "e3" || filtered[1].ID != "e1" {
		t.Errorf("filtered = %+v, want only the two sign_in_failure rows, newest first", filtered)
	}

	bounded, err := ListAuditEvents(t.Context(), db, AuditEventFilter{Limit: 1})
	if err != nil {
		t.Fatalf("list bounded: %v", err)
	}
	if len(bounded) != 1 || bounded[0].ID != "e3" {
		t.Errorf("bounded = %+v, want only the newest row", bounded)
	}

	// A same-millisecond pair falls back to the id, so the order is total.
	seedAuditRow(t, db, "e4", audit.EventSignOut, audit.ResultSuccess, "actor", "", "", 3_000)
	tied, err := ListAuditEvents(t.Context(), db, AuditEventFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list tied: %v", err)
	}
	if tied[0].ID != "e4" || tied[1].ID != "e3" {
		t.Errorf("tied pair = %s, %s — want the id tie-breaker (e4 then e3)", tied[0].ID, tied[1].ID)
	}
}

// TestListAuditEvents_EmptyLedger pins the fresh-install answer: no rows is an
// empty slice, not an error and not nil.
func TestListAuditEvents_EmptyLedger(t *testing.T) {
	db := openStoreDB(t)

	events, err := ListAuditEvents(t.Context(), db, AuditEventFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if events == nil || len(events) != 0 {
		t.Errorf("events = %v, want an empty non-nil slice", events)
	}
}

func TestAuditWriter_PersistsRecord(t *testing.T) {
	db := openStoreDB(t)
	fixed := time.UnixMilli(1_700_000_000_000)
	w := &AuditWriter{DB: db, Now: func() time.Time { return fixed }}

	err := w.Write(t.Context(), audit.Record{
		Event:      audit.EventRoleChanged,
		Result:     audit.ResultSuccess,
		ActorID:    "actor-1",
		TargetID:   "target-1",
		TargetRole: "moderator",
		RequestID:  "req-1",
		RemoteAddr: "192.0.2.1:1234",
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	var (
		id, event, result, actorID, targetID, targetRole, requestID, remoteAddr string
		createdAtMS                                                             int64
	)
	err = db.QueryRow(`
		SELECT id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms
		FROM audit_events`).Scan(&id, &event, &result, &actorID, &targetID, &targetRole, &requestID, &remoteAddr, &createdAtMS)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if len(id) != 32 {
		t.Errorf("id length: got %d (%q), want 32", len(id), id)
	}
	for _, c := range id {
		if !isHex(c) {
			t.Errorf("id %q contains non-hex character %q", id, c)
			break
		}
	}
	if event != audit.EventRoleChanged || result != audit.ResultSuccess {
		t.Errorf("event/result: got %q/%q", event, result)
	}
	if actorID != "actor-1" || targetID != "target-1" {
		t.Errorf("actor/target: got %q/%q", actorID, targetID)
	}
	if targetRole != "moderator" {
		t.Errorf("target_role: got %q, want moderator", targetRole)
	}
	if requestID != "req-1" || remoteAddr != "192.0.2.1:1234" {
		t.Errorf("request/remote: got %q/%q", requestID, remoteAddr)
	}
	if createdAtMS != fixed.UnixMilli() {
		t.Errorf("created_at_ms: got %d, want %d (the injected clock)", createdAtMS, fixed.UnixMilli())
	}
}

func TestAuditWriter_NullIdentity(t *testing.T) {
	db := openStoreDB(t)
	w := &AuditWriter{DB: db}

	// Actor unknown (sign_in_failure): both identity columns NULL.
	if err := w.Write(t.Context(), audit.Record{
		Event:      audit.EventSignInFailure,
		Result:     audit.ResultFailure,
		RequestID:  "req-2",
		RemoteAddr: "127.0.0.1:1",
	}); err != nil {
		t.Fatalf("Write unknown actor: %v", err)
	}
	// Actor known, no target (content event): target NULL.
	if err := w.Write(t.Context(), audit.Record{
		Event:      audit.EventContentCreated,
		Result:     audit.ResultSuccess,
		ActorID:    "actor-2",
		RequestID:  "req-3",
		RemoteAddr: "127.0.0.1:2",
	}); err != nil {
		t.Fatalf("Write no target: %v", err)
	}

	rows, err := db.Query("SELECT actor_id, target_id, target_role FROM audit_events ORDER BY created_at_ms")
	if err != nil {
		t.Fatalf("query identities: %v", err)
	}
	defer rows.Close()

	var nulls int
	var cases int
	for rows.Next() {
		cases++
		var actorID, targetID, targetRole *string
		if err := rows.Scan(&actorID, &targetID, &targetRole); err != nil {
			t.Fatalf("scan identities: %v", err)
		}
		if actorID == nil {
			nulls++
		}
		if targetID == nil {
			nulls++
		}
		if targetRole == nil {
			nulls++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate identities: %v", err)
	}
	if cases != 2 {
		t.Fatalf("rows: got %d, want 2", cases)
	}
	// Case 1: actor NULL + target NULL + role NULL; case 2: target NULL +
	// role NULL (content events have no target snapshot).
	if nulls != 5 {
		t.Errorf("NULL identity columns: got %d, want 5 (unknown actor, two targets, two roles)", nulls)
	}
}

// TestAuditWriter_EmptyRemoteAddrStoredEmpty pins the pass-through of an empty
// address (the break-glass shape): the column is NOT NULL, so the writer must
// store ” rather than a NULL or fail the insert.
func TestAuditWriter_EmptyRemoteAddrStoredEmpty(t *testing.T) {
	db := openStoreDB(t)
	w := &AuditWriter{DB: db}

	if err := w.Write(t.Context(), audit.Record{
		Event:     audit.EventPasswordReset,
		Result:    audit.ResultSuccess,
		ActorID:   "maintenance",
		TargetID:  "target-1",
		RequestID: "",
	}); err != nil {
		t.Fatalf("Write empty address: %v", err)
	}

	var remoteAddr string
	if err := db.QueryRow(
		`SELECT remote_addr FROM audit_events WHERE event = ?`, audit.EventPasswordReset,
	).Scan(&remoteAddr); err != nil {
		t.Fatalf("read stored address: %v", err)
	}
	if remoteAddr != "" {
		t.Errorf("stored remote_addr = %q, want empty", remoteAddr)
	}
}

func TestAuditWriter_DistinctIDs(t *testing.T) {
	db := openStoreDB(t)
	w := &AuditWriter{DB: db}

	for i := range 2 {
		if err := w.Write(t.Context(), audit.Record{
			Event:     audit.EventSignOut,
			Result:    audit.ResultSuccess,
			RequestID: "req",
		}); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	var first, second string
	if err := db.QueryRow("SELECT id FROM audit_events ORDER BY created_at_ms LIMIT 1").Scan(&first); err != nil {
		t.Fatalf("read first id: %v", err)
	}
	if err := db.QueryRow("SELECT id FROM audit_events ORDER BY created_at_ms DESC LIMIT 1").Scan(&second); err != nil {
		t.Fatalf("read second id: %v", err)
	}
	if first == second {
		t.Errorf("two writes produced the same id %q", first)
	}
}

func TestAuditWriter_DefaultClock(t *testing.T) {
	db := openStoreDB(t)
	w := &AuditWriter{DB: db} // Now nil → time.Now

	before := time.Now().UnixMilli()
	if err := w.Write(t.Context(), audit.Record{
		Event:     audit.EventSignOut,
		Result:    audit.ResultSuccess,
		RequestID: "req",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after := time.Now().UnixMilli()

	var createdAtMS int64
	if err := db.QueryRow("SELECT created_at_ms FROM audit_events").Scan(&createdAtMS); err != nil {
		t.Fatalf("read created_at_ms: %v", err)
	}
	if createdAtMS < before || createdAtMS > after {
		t.Errorf("created_at_ms %d outside [%d, %d]", createdAtMS, before, after)
	}
}

func TestAuditEvents_ResultCheckEnforced(t *testing.T) {
	db := openStoreDB(t)

	_, err := db.Exec(`
		INSERT INTO audit_events (id, event, result, request_id, remote_addr, created_at_ms)
		VALUES ('x', 'e', 'bogus', '', '', 1)`)
	if err == nil {
		t.Fatal("expected CHECK failure for a result outside success/failure")
	}
}

// TestDeleteExpiredAuditEvents pins the cleanup contract: one call
// removes only events older than the cutoff, bounded by the limit, and the
// notification read state follows via the FK cascade.
func TestDeleteExpiredAuditEvents(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()

	insertEvent := func(id string, createdAtMS int64) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO audit_events (id, event, result, request_id, remote_addr, created_at_ms)
			VALUES (?, 'role_changed', 'success', '', '', ?)`, id, createdAtMS); err != nil {
			t.Fatalf("insert event %s: %v", id, err)
		}
	}
	insertEvent("e_old1", 1)
	insertEvent("e_old2", 2)
	insertEvent("e_old3", 3)
	insertEvent("e_new", 4_000_000_000_000) // recent

	// Read state on one expired event must cascade away with it.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "u1",
		Username:    "U",
		CreatedAtMS: 1,
	})
	if _, err := db.Exec(`
		INSERT INTO notification_reads (user_id, event_id, read_at_ms) VALUES ('u1', 'e_old1', 5)`); err != nil {
		t.Fatalf("insert notification read: %v", err)
	}

	// Batch size 2: the first call removes two of the three expired rows.
	n, err := DeleteExpiredAuditEvents(ctx, db, 3_000_000_000_000, 2)
	if err != nil {
		t.Fatalf("DeleteExpiredAuditEvents: %v", err)
	}
	if n != 2 {
		t.Errorf("first batch deleted %d, want 2", n)
	}

	// The cascade removed the read row pointing at the deleted event.
	var reads int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_reads`).Scan(&reads); err != nil {
		t.Fatalf("count reads: %v", err)
	}
	if reads != 0 {
		t.Errorf("notification_reads rows after cascade: got %d, want 0", reads)
	}

	// Second batch removes the remaining expired row; the recent one survives.
	n, err = DeleteExpiredAuditEvents(ctx, db, 3_000_000_000_000, 2)
	if err != nil {
		t.Fatalf("DeleteExpiredAuditEvents (second): %v", err)
	}
	if n != 1 {
		t.Errorf("second batch deleted %d, want 1", n)
	}
	var remaining string
	if err := db.QueryRow(`SELECT id FROM audit_events`).Scan(&remaining); err != nil {
		t.Fatalf("remaining event: %v", err)
	}
	if remaining != "e_new" {
		t.Errorf("surviving event = %q, want e_new", remaining)
	}
}
