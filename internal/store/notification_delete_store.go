package store

import (
	"context"
	"database/sql"
	"fmt"
)

// The notification delete/dismissal surface: the per-item delete, clear-read,
// and delete-all. The feed's read path (list, counts, read state) lives in
// notification_store.go; both halves share the visibility predicate and the
// notification_reads table.

// DeleteNotification removes ONE entry from the viewer's feed, in both id
// spaces:
//
//   - a user_notifications id must belong to the viewer and stay inside the
//     viewer's branch (below the staff floor the re-gated draft kind is not
//     addressable); the row is deleted;
//   - a visible, not-yet-dismissed audit_events id gets a per-viewer
//     DISMISSAL (notification_reads.dismissed_at_ms) — the audit ledger row
//     itself is never deleted, and the dismissal stamps read_at_ms too, so
//     the unread badge's anti-join stays valid.
//
// Unknown, foreign, invisible, and already-dismissed ids report false and the
// handler answers the masked 404: deleting never reveals that an entry exists
// outside the viewer's scope, and a repeat delete of the same id is a 404
// because the entry is gone from the feed.
func DeleteNotification(ctx context.Context, db *sql.DB, viewerID, notificationID string, viewerRoleWeight int, includeStaff bool, nowMS int64) (bool, error) {
	userWhere := `WHERE id = ? AND recipient_id = ?`
	args := []any{notificationID, viewerID}
	if !includeStaff {
		userWhere += ` AND kind <> 'draft_activity'`
	}
	res, err := db.ExecContext(ctx, `
		DELETE FROM user_notifications `+userWhere, args...)
	if err != nil {
		return false, fmt.Errorf("store: delete notification: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: delete notification rows affected: %w", err)
	}
	if n > 0 {
		return true, nil
	}

	if !includeStaff {
		return false, nil
	}

	res, err = db.ExecContext(ctx, `
		INSERT INTO notification_reads (user_id, event_id, read_at_ms, dismissed_at_ms)
		SELECT ?, e.id, ?, ?
		FROM audit_events e
		WHERE e.id = ? AND `+visibilityPredicate+`
			AND NOT EXISTS (
				SELECT 1 FROM notification_reads d
				WHERE d.user_id = ? AND d.event_id = e.id AND d.dismissed_at_ms IS NOT NULL)
		ON CONFLICT(user_id, event_id) DO UPDATE SET dismissed_at_ms = excluded.dismissed_at_ms`,
		// Bind order follows the SQL text: viewer, the two stamps, the
		// id, the predicate's role weight (its trailing placeholder), and
		// the NOT EXISTS viewer.
		viewerID, nowMS, nowMS, notificationID, viewerRoleWeight, viewerID)
	if err != nil {
		return false, fmt.Errorf("store: dismiss notification: %w", err)
	}
	dismissed, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: dismiss notification rows affected: %w", err)
	}
	return dismissed > 0, nil
}

// DeleteReadNotifications removes every READ user_notifications row of the
// viewer — the "clear what I have already read" action.
// Unread rows always survive: the action can never discard something the
// viewer has not looked at. Below the staff floor the re-gated draft kind is
// excluded as well. The audit half is untouched (DeleteAllNotifications owns
// the per-viewer dismissal sweep).
func DeleteReadNotifications(ctx context.Context, db *sql.DB, viewerID string, includeStaff bool) (int64, error) {
	userWhere := `WHERE recipient_id = ? AND read_at_ms IS NOT NULL`
	args := []any{viewerID}
	if !includeStaff {
		userWhere += ` AND kind <> 'draft_activity'`
	}
	res, err := db.ExecContext(ctx, `
		DELETE FROM user_notifications `+userWhere, args...)
	if err != nil {
		return 0, fmt.Errorf("store: delete read notifications: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete read notifications rows affected: %w", err)
	}
	return n, nil
}

// DeleteAllNotifications removes the viewer's whole feed in one transaction:
// every user_notifications row of the viewer (read and unread alike, with the
// below-floor draft gate applied) and a per-viewer dismissal of every
// visible, not-yet-dismissed audit event. The audit ledger rows themselves
// are never deleted. Returns the total number of entries that left the feed;
// a second call finds nothing and returns 0.
func DeleteAllNotifications(ctx context.Context, db *sql.DB, viewerID string, viewerRoleWeight int, includeStaff bool, nowMS int64) (int64, error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return 0, fmt.Errorf("store: begin delete-all notifications: %w", err)
	}
	defer tx.Rollback()

	userWhere := `WHERE recipient_id = ?`
	args := []any{viewerID}
	if !includeStaff {
		userWhere += ` AND kind <> 'draft_activity'`
	}
	res, err := tx.ExecContext(ctx, `
		DELETE FROM user_notifications `+userWhere, args...)
	if err != nil {
		return 0, fmt.Errorf("store: delete all notifications (user space): %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete all notifications rows affected: %w", err)
	}

	if !includeStaff {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("store: commit delete-all notifications: %w", err)
		}
		return n, nil
	}

	// The upsert absorbs the read-only rows: a read entry is dismissed in
	// place, never duplicated (the composite PK).
	res, err = tx.ExecContext(ctx, `
		INSERT INTO notification_reads (user_id, event_id, read_at_ms, dismissed_at_ms)
		SELECT ?, e.id, ?, ?
		FROM audit_events e
		WHERE `+visibilityPredicate+`
			AND NOT EXISTS (
				SELECT 1 FROM notification_reads d
				WHERE d.user_id = ? AND d.event_id = e.id AND d.dismissed_at_ms IS NOT NULL)
		ON CONFLICT(user_id, event_id) DO UPDATE SET dismissed_at_ms = excluded.dismissed_at_ms`,
		// Bind order follows the SQL text: viewer, the two stamps, the
		// predicate's role weight (its trailing placeholder), and the
		// NOT EXISTS viewer.
		viewerID, nowMS, nowMS, viewerRoleWeight, viewerID)
	if err != nil {
		return 0, fmt.Errorf("store: dismiss all notifications: %w", err)
	}
	dismissed, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: dismiss all notifications rows affected: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit delete-all notifications: %w", err)
	}
	return n + dismissed, nil
}
