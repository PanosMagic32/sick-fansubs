package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Follows persistence (docs/patterns/go/content-kinds.md): one content_follows
// table with a polymorphic (content_kind, content_id) ref — follows drive the
// 'comment' and 'content_updated' notification kinds (a SEPARATE concept from
// favorites).
//
// The functions mirror the favorites store: every operation is one statement
// (one row's worth of work — no transactions), following is constrained to
// PUBLISHED content via the INSERT…SELECT WHERE EXISTS pattern (the check and
// the write share one snapshot).

// FollowStatus reports whether the content is published and whether the user
// follows it. published=false covers unknown ids, drafts, archived rows, and
// unstamped rows (masked) — following is only meaningful when published=true.
func FollowStatus(ctx context.Context, db *sql.DB, k ContentKind, userID, contentID string) (published, following bool, err error) {
	return followStatus(ctx, db,
		`SELECT
			EXISTS(SELECT 1 FROM `+k.table+`
				WHERE id = ? AND status = 'published' AND published_at_ms IS NOT NULL),
			EXISTS(SELECT 1 FROM content_follows
				WHERE user_id = ? AND content_kind = ? AND content_id = ?)`,
		contentID, userID, k.kind, contentID)
}

func followStatus(ctx context.Context, db *sql.DB, query string, args ...any) (bool, bool, error) {
	var published, following bool
	err := db.QueryRowContext(ctx, query, args...).Scan(&published, &following)
	if err != nil {
		return false, false, fmt.Errorf("store: follow status: %w", err)
	}
	return published, following, nil
}

// AddFollow inserts a follow row for PUBLISHED content, idempotently; unknown
// or unpublished content is the masked ErrNotFound (the favorites contract).
func AddFollow(ctx context.Context, db *sql.DB, k ContentKind, userID, contentID string, createdAtMS int64) error {
	return addFollow(ctx, db,
		`INSERT INTO content_follows (user_id, content_kind, content_id, created_at_ms)
		SELECT ?, ?, ?, ?
		WHERE EXISTS (
			SELECT 1 FROM `+k.table+`
			WHERE id = ? AND status = 'published' AND published_at_ms IS NOT NULL
		)`,
		userID, k.kind, contentID, createdAtMS, contentID)
}

func addFollow(ctx context.Context, db *sql.DB, query string, args ...any) error {
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		if IsUniqueViolation(err) {
			// Already following — idempotent success.
			return nil
		}
		if isForeignKeyViolation(err) {
			// A user deleted between the session lookup and this insert.
			// The public outcome is the masked 404 (the favorites
			// precedent).
			return ErrNotFound
		}
		return fmt.Errorf("store: add follow: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: add follow rows affected: %w", err)
	}
	if n == 0 {
		// The EXISTS guard rejected the content — not published, unstamped,
		// or unknown. Deliberately indistinguishable.
		return ErrNotFound
	}
	return nil
}

// RemoveFollow deletes the follow row if it exists. Idempotent: removing a
// non-follow is success.
func RemoveFollow(ctx context.Context, db *sql.DB, k ContentKind, userID, contentID string) error {
	return removeFollow(ctx, db,
		`DELETE FROM content_follows WHERE user_id = ? AND content_kind = ? AND content_id = ?`,
		userID, k.kind, contentID)
}

func removeFollow(ctx context.Context, db *sql.DB, query string, args ...any) error {
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("store: remove follow: %w", err)
	}
	return nil
}
