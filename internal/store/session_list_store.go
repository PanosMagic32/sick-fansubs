package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SessionListItem is one row of the self-service session list. ClientLabel is
// empty when the stored column is NULL — the classifier recognized nothing.
type SessionListItem struct {
	ID          string
	ClientLabel string
	CreatedAtMS int64
	ExpiresAtMS int64
}

// SessionPageKey is the session list's keyset continuation tuple for the
// ordering created_at_ms DESC, id ASC: "strictly after" is an older instant,
// or the same instant with a larger (final tie-breaker) id.
type SessionPageKey struct {
	CreatedAtMS int64
	ID          string
}

// ListSessions returns one page of a user's ACTIVE sessions (expires_at_ms
// beyond the caller's nowMS — the window is in the query, so an expired row
// is absent before cleanup), newest first, plus whether another page follows.
func ListSessions(ctx context.Context, db *sql.DB, userID string, nowMS int64, limit int, after *SessionPageKey) ([]SessionListItem, bool, error) {
	// Two explicit query shapes keep the keyset predicate visible instead of
	// hiding it behind a NULL-binding trick (the favorites-list pattern).
	const (
		columns = `id, client_label, created_at_ms, expires_at_ms`
		from    = ` FROM sessions WHERE user_id = ? AND expires_at_ms > ?`
		order   = ` ORDER BY created_at_ms DESC, id ASC LIMIT ?`
	)

	var (
		query string
		args  []any
	)
	if after == nil {
		query = `SELECT ` + columns + from + order
		args = []any{userID, nowMS, limit + 1}
	} else {
		query = `SELECT ` + columns + from +
			` AND (created_at_ms < ? OR (created_at_ms = ? AND id > ?))` + order
		args = []any{userID, nowMS, after.CreatedAtMS, after.CreatedAtMS, after.ID, limit + 1}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var items []SessionListItem
	for rows.Next() {
		var (
			it    SessionListItem
			label sql.NullString
		)
		if err := rows.Scan(&it.ID, &label, &it.CreatedAtMS, &it.ExpiresAtMS); err != nil {
			return nil, false, fmt.Errorf("store: scan session item: %w", err)
		}
		it.ClientLabel = nullStringValue(label)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate sessions: %w", err)
	}

	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}

// CountSessions returns the caller's active-session count — the list's
// "of N" number, under the same expiry window.
func CountSessions(ctx context.Context, db *sql.DB, userID string, nowMS int64) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND expires_at_ms > ?`,
		userID, nowMS).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count sessions: %w", err)
	}
	return n, nil
}
