package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SetCommentHeart toggles one viewer's heart on a published comment in one
// transaction: the heart row, the counter delta, and the heart notification
// commit together. A self-heart is ErrSelfHeart; an unheart removes both.
func SetCommentHeart(ctx context.Context, db *sql.DB, k ContentKind, userID, contentID, commentID string, on bool, heartNotificationID string, createdAtMS int64) (NotificationEvent, bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: begin heart toggle: %w", err)
	}
	defer tx.Rollback()

	var (
		authorID     string
		contentTitle string
		authorActive bool
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT c.user_id, p.title, au.status = 'active' FROM `+k.comments+` c
		JOIN `+k.table+` p ON p.id = c.`+k.commentFK+`
		JOIN users au ON au.id = c.user_id
		WHERE c.id = ? AND c.`+k.commentFK+` = ?
			AND p.status = 'published' AND p.published_at_ms IS NOT NULL`,
		commentID, contentID).Scan(&authorID, &contentTitle, &authorActive); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NotificationEvent{}, false, ErrNotFound
		}
		return NotificationEvent{}, false, fmt.Errorf("store: heart gate: %w", err)
	}
	if on && authorID == userID {
		return NotificationEvent{}, false, ErrSelfHeart
	}

	var event NotificationEvent
	if on {
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO `+k.hearts+` (user_id, comment_id, created_at_ms) VALUES (?, ?, ?)`,
			userID, commentID, createdAtMS)
		if err != nil {
			if isForeignKeyViolation(err) {
				return NotificationEvent{}, false, ErrNotFound
			}
			return NotificationEvent{}, false, fmt.Errorf("store: insert heart: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return NotificationEvent{}, false, fmt.Errorf("store: insert heart rows affected: %w", err)
		}
		if n == 1 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE `+k.comments+` SET hearts_count = hearts_count + 1 WHERE id = ?`, commentID); err != nil {
				return NotificationEvent{}, false, fmt.Errorf("store: increment hearts: %w", err)
			}
			// The counter always moves; only the notice respects the
			// active-recipient rule.
			if authorActive {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO user_notifications (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
					VALUES (?, ?, 'heart', ?, ?, ?, ?, ?)`,
					heartNotificationID, authorID, userID, k.kind, contentID, commentID, createdAtMS); err != nil {
					if isForeignKeyViolation(err) {
						return NotificationEvent{}, false, ErrNotFound
					}
					return NotificationEvent{}, false, fmt.Errorf("store: insert heart notification: %w", err)
				}
				event = NotificationEvent{
					RecipientID:  authorID,
					Kind:         "heart",
					ContentKind:  k.kind,
					ContentID:    contentID,
					CommentID:    commentID,
					ContentTitle: contentTitle,
				}
			}
		}
	} else {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM `+k.hearts+` WHERE user_id = ? AND comment_id = ?`, userID, commentID)
		if err != nil {
			return NotificationEvent{}, false, fmt.Errorf("store: delete heart: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return NotificationEvent{}, false, fmt.Errorf("store: delete heart rows affected: %w", err)
		}
		if n == 1 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE `+k.comments+` SET hearts_count = hearts_count - 1 WHERE id = ?`, commentID); err != nil {
				return NotificationEvent{}, false, fmt.Errorf("store: decrement hearts: %w", err)
			}
			// Scoped by content_kind for symmetry with the insert — the
			// actor+comment pair already identifies the row, the extra
			// binding is defense-in-depth.
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM user_notifications WHERE kind = 'heart' AND actor_id = ? AND content_kind = ? AND comment_id = ?`,
				userID, k.kind, commentID); err != nil {
				return NotificationEvent{}, false, fmt.Errorf("store: delete heart notification: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: commit heart toggle: %w", err)
	}
	return event, event.RecipientID != "", nil
}
