package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/id"
)

// AuditWriter persists audit.Record values into the audit_events table —
// the durable half of the dual-write audit trail. It
// implements audit.Writer; cmd/api registers one after opening the database.
//
// The write stamps created_at_ms from the Now clock (time.Now when nil — the
// application clock, never a schema default) and generates the row ID through
// id.New, the shared 32-hex random identifier. Empty actor/target/target_role
// strings become SQL NULL: the contract distinguishes "no actor" (NULL) from
// an identifier.
//
// Write returns an error for any failure; the audit package logs it and
// never propagates it to the audited operation.
type AuditWriter struct {
	DB  *sql.DB
	Now func() time.Time
}

// Write persists one audit record.
func (w *AuditWriter) Write(ctx context.Context, rec audit.Record) error {
	eventID, err := id.New()
	if err != nil {
		return fmt.Errorf("store: audit event id: %w", err)
	}
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	_, err = w.DB.ExecContext(ctx, `
		INSERT INTO audit_events
			(id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		eventID, rec.Event, rec.Result, nullableString(rec.ActorID), nullableString(rec.TargetID),
		nullableString(rec.TargetRole), rec.RequestID, rec.RemoteAddr, now().UnixMilli())
	if err != nil {
		return fmt.Errorf("store: insert audit event: %w", err)
	}
	return nil
}

// AuditEvent is one row of the audit ledger as the super-admin browser reads
// it. The nullable identity columns surface as empty strings — the read half
// of the nullableString write pair.
type AuditEvent struct {
	ID          string
	Event       string
	Result      string
	ActorID     string
	TargetID    string
	TargetRole  string
	RequestID   string
	RemoteAddr  string
	CreatedAtMS int64
}

// AuditEventFilter narrows ListAuditEvents. Event is an exact match; empty
// means every kind. The caller validates the value against the accepted
// vocabulary (audit.EventNames) before it reaches here.
type AuditEventFilter struct {
	Event string
	Limit int
}

// ListAuditEvents returns the newest audit events, newest first. It is a
// bounded TAIL read, not a keyset page: the browser looks at recent activity,
// and createdAt/ID descending is a total order, so no cursor is needed.
//
// idx_audit_events_created serves the ordering: a kind filter is applied to
// the newest rows as they are scanned and the read stops at the limit. The
// filter VALUE must still come from the accepted vocabulary — not for SQL
// safety (it is a bound parameter) but because the wire contract allows only
// those kinds (422 otherwise), and a value outside the vocabulary could only
// ever match nothing.
//
// The WHERE builder mirrors ListStaffUsers (the repo's idiom for an optional
// filter); with one filter it is two shapes, kept built rather than split so
// the ordering clause and the argument list stay in one place.
func ListAuditEvents(ctx context.Context, db *sql.DB, filter AuditEventFilter) ([]AuditEvent, error) {
	var where strings.Builder
	where.WriteString(`SELECT id, event, result, actor_id, target_id, target_role, request_id, remote_addr, created_at_ms
		FROM audit_events WHERE 1 = 1`)
	args := make([]any, 0, 2)

	if filter.Event != "" {
		where.WriteString(` AND event = ?`)
		args = append(args, filter.Event)
	}

	query := where.String() + ` ORDER BY created_at_ms DESC, id DESC LIMIT ?`
	args = append(args, filter.Limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list audit events: %w", err)
	}
	defer rows.Close()

	events := make([]AuditEvent, 0, filter.Limit)
	for rows.Next() {
		var e AuditEvent
		var actorID, targetID, targetRole sql.NullString
		if err := rows.Scan(&e.ID, &e.Event, &e.Result, &actorID, &targetID, &targetRole,
			&e.RequestID, &e.RemoteAddr, &e.CreatedAtMS); err != nil {
			return nil, fmt.Errorf("store: scan audit event: %w", err)
		}
		e.ActorID = nullStringValue(actorID)
		e.TargetID = nullStringValue(targetID)
		e.TargetRole = nullStringValue(targetRole)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate audit events: %w", err)
	}
	return events, nil
}

// DeleteExpiredAuditEvents deletes audit events older than cutoffMS in ONE
// bounded batch (bounded-batch deletes on the created_at_ms index — a large
// backlog must never hold the SQLite writer for an unbounded delete; the
// loop lives in cmd/api, the session-cleanup split). It returns the number
// of rows removed.
//
// The id-IN-subquery shape is the DeleteExpiredSessions precedent — the
// limit applies to the scanned rows, not the outer delete. notification_reads
// rows follow via their ON DELETE CASCADE foreign key, so the read state
// cleans up with the events (the window must stay ≥ backup retention —
// 365 ≥ 10 — so a restored backup's event history still covers the
// reconciliation window).
func DeleteExpiredAuditEvents(ctx context.Context, db *sql.DB, cutoffMS int64, limit int) (int64, error) {
	const q = `DELETE FROM audit_events WHERE id IN (
		SELECT id FROM audit_events WHERE created_at_ms < ? LIMIT ?
	)`

	res, err := db.ExecContext(ctx, q, cutoffMS, limit)
	if err != nil {
		return 0, fmt.Errorf("store: delete expired audit events: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete expired audit events rows affected: %w", err)
	}
	return n, nil
}
