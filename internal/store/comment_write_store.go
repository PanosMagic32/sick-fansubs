package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CreateComment inserts one top-level comment on published content and, per
// follower of that content other than the author, one 'comment' notification
// row — all in one transaction. The events are the push fan-out input.
func CreateComment(ctx context.Context, db *sql.DB, k ContentKind, contentID, userID, body, id string, createdAtMS int64) ([]NotificationEvent, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin comment: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO `+k.comments+` (id, user_id, `+k.commentFK+`, body, hearts_count, created_at_ms, updated_at_ms)
		SELECT ?, ?, ?, ?, 0, ?, ?
		WHERE EXISTS (
			SELECT 1 FROM `+k.table+`
			WHERE id = ? AND status = 'published' AND published_at_ms IS NOT NULL
		)`, id, userID, contentID, body, createdAtMS, createdAtMS, contentID)
	if err != nil {
		if isForeignKeyViolation(err) {
			// The user was deleted between the session lookup and this
			// insert — the public outcome is the masked 404 (the favorites
			// precedent).
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: create comment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("store: create comment rows affected: %w", err)
	}
	if n == 0 {
		return nil, ErrNotFound
	}

	// The notification events carry the content title (the push payload
	// needs it) — read it in-tx so the events agree with the committed
	// snapshot.
	var contentTitle string
	if err := tx.QueryRowContext(ctx,
		`SELECT title FROM `+k.table+` WHERE id = ?`, contentID).Scan(&contentTitle); err != nil {
		return nil, fmt.Errorf("store: comment content title: %w", err)
	}

	// 'comment' fan-out: one notification
	// row per follower of the content — the author excluded (self-
	// suppression, the reply precedent) — in the create transaction.
	events, err := notifyFollowersTx(ctx, tx, "comment", userID, k, contentID, &id, contentTitle, createdAtMS)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit comment: %w", err)
	}
	return events, nil
}

// CreateCommentReply inserts one reply to a top-level comment and, unless the
// parent author is the replier, the comment_reply notification row — one
// transaction. Outcomes: ErrNotFound (masked) or ErrInvalidParent (depth 1).
func CreateCommentReply(ctx context.Context, db *sql.DB, k ContentKind, contentID, parentID, userID, body, replyID, notificationID string, createdAtMS int64) (NotificationEvent, bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: begin reply: %w", err)
	}
	defer tx.Rollback()

	var (
		parentUserID string
		parentIsTop  bool
		parentActive bool
		contentTitle string
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT c.user_id, c.parent_id IS NULL, pu.status = 'active', p.title FROM `+k.comments+` c
		JOIN `+k.table+` p ON p.id = c.`+k.commentFK+`
		JOIN users pu ON pu.id = c.user_id
		WHERE c.id = ? AND c.`+k.commentFK+` = ?`,
		parentID, contentID).Scan(&parentUserID, &parentIsTop, &parentActive, &contentTitle); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Unknown OR foreign — indistinguishable by design.
			return NotificationEvent{}, false, ErrNotFound
		}
		return NotificationEvent{}, false, fmt.Errorf("store: reply parent lookup: %w", err)
	}
	if !parentIsTop {
		return NotificationEvent{}, false, ErrInvalidParent
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO `+k.comments+` (id, user_id, `+k.commentFK+`, parent_id, body, hearts_count, created_at_ms, updated_at_ms)
		SELECT ?, ?, ?, ?, ?, 0, ?, ?
		WHERE EXISTS (
			SELECT 1 FROM `+k.comments+` pc
			WHERE pc.id = ? AND pc.`+k.commentFK+` = ? AND pc.parent_id IS NULL
		)
		AND EXISTS (
			SELECT 1 FROM `+k.table+`
			WHERE id = ? AND status = 'published' AND published_at_ms IS NOT NULL
		)`,
		replyID, userID, contentID, parentID, body, createdAtMS, createdAtMS,
		parentID, contentID, contentID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return NotificationEvent{}, false, ErrNotFound
		}
		return NotificationEvent{}, false, fmt.Errorf("store: insert reply: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: insert reply rows affected: %w", err)
	}
	if n == 0 {
		// The parent vanished between the lookup and the insert, or the
		// content is not published — indistinguishable by design (the
		// EXISTS re-check keeps the check and the write atomic).
		return NotificationEvent{}, false, ErrNotFound
	}

	var event NotificationEvent
	if parentUserID != userID && parentActive {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_notifications (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
			VALUES (?, ?, 'comment_reply', ?, ?, ?, ?, ?)`,
			notificationID, parentUserID, userID, k.kind, contentID, replyID, createdAtMS); err != nil {
			if isForeignKeyViolation(err) {
				return NotificationEvent{}, false, ErrNotFound
			}
			return NotificationEvent{}, false, fmt.Errorf("store: insert reply notification: %w", err)
		}
		event = NotificationEvent{
			RecipientID:  parentUserID,
			Kind:         "comment_reply",
			ContentKind:  k.kind,
			ContentID:    contentID,
			CommentID:    replyID,
			ContentTitle: contentTitle,
		}
	}

	if err := tx.Commit(); err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: commit reply: %w", err)
	}
	return event, event.RecipientID != "", nil
}

// UpdateComment applies an author-only body edit (last-write-wins) and
// returns the re-read item. Outcomes: ErrNotFound (masked) and ErrNotAuthor.
func UpdateComment(ctx context.Context, db *sql.DB, k ContentKind, contentID, commentID, userID, body string, updatedAtMS int64) (*Comment, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin comment update: %w", err)
	}
	defer tx.Rollback()

	var (
		authorID  string
		published bool
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT c.user_id, (p.status = 'published' AND p.published_at_ms IS NOT NULL)
		FROM `+k.comments+` c
		JOIN `+k.table+` p ON p.id = c.`+k.commentFK+`
		WHERE c.id = ? AND c.`+k.commentFK+` = ?`, commentID, contentID).Scan(&authorID, &published); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: comment update lookup: %w", err)
	}
	if !published {
		return nil, ErrNotFound
	}
	if authorID != userID {
		return nil, ErrNotAuthor
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE `+k.comments+` SET body = ?, updated_at_ms = ? WHERE id = ?`,
		body, updatedAtMS, commentID)
	if err != nil {
		return nil, fmt.Errorf("store: update comment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("store: update comment rows affected: %w", err)
	}
	if n == 0 {
		// Deleted between the lookup and the update — the benign TOCTOU
		// (the disambiguation-SELECT precedent).
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit comment update: %w", err)
	}

	return getCommentWithReplies(ctx, db, k, contentID, commentID, &userID)
}

// CommentAuthorID returns one comment's author id and whether its content
// is visible (published) — the delete handler's authorization inputs.
// Content-scoped: a foreign comment is ErrNotFound; the visibility flag lets
// the handler answer the same masked 404 as the content detail for a
// non-author below the staff floor, so the gate never becomes an existence
// oracle on unpublished content.
func CommentAuthorID(ctx context.Context, db *sql.DB, k ContentKind, contentID, commentID string) (string, bool, error) {
	var (
		authorID  string
		published bool
	)
	if err := db.QueryRowContext(ctx,
		`SELECT c.user_id, COALESCE(p.status = 'published' AND p.published_at_ms IS NOT NULL, 0)
		 FROM `+k.comments+` c
		 JOIN `+k.table+` p ON p.id = c.`+k.commentFK+`
		 WHERE c.id = ? AND c.`+k.commentFK+` = ?`,
		commentID, contentID).Scan(&authorID, &published); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, fmt.Errorf("store: comment author lookup: %w", err)
	}
	return authorID, published, nil
}

// DeleteComment hard-deletes one comment, cascading its replies and hearts,
// in one transaction. A non-author delete emits the ACTORLESS
// comment_removed notice to the author; a self-delete emits nothing, and an
// inactive author is skipped (the active-recipient rule) — the delete
// itself still lands.
func DeleteComment(ctx context.Context, db *sql.DB, k ContentKind, commentID, actorID, notificationID string, createdAtMS int64) (NotificationEvent, bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: begin comment delete: %w", err)
	}
	defer tx.Rollback()

	// The author plus the content id and title are read BEFORE the delete:
	// the row is about to vanish, and the polymorphic notification ref needs
	// all three (the title feeds the push payload).
	var (
		authorID     string
		contentID    string
		contentTitle string
		authorActive bool
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT c.user_id, c.`+k.commentFK+`, p.title, au.status = 'active' FROM `+k.comments+` c
		JOIN `+k.table+` p ON p.id = c.`+k.commentFK+`
		JOIN users au ON au.id = c.user_id
		WHERE c.id = ?`, commentID).Scan(&authorID, &contentID, &contentTitle, &authorActive); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NotificationEvent{}, false, ErrNotFound
		}
		return NotificationEvent{}, false, fmt.Errorf("store: comment delete lookup: %w", err)
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM `+k.comments+` WHERE id = ?`, commentID)
	if err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: delete comment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: delete comment rows affected: %w", err)
	}
	if n == 0 {
		return NotificationEvent{}, false, ErrNotFound
	}

	var event NotificationEvent
	if authorID != actorID && authorActive {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_notifications (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
			VALUES (?, ?, 'comment_removed', NULL, ?, ?, ?, ?)`,
			notificationID, authorID, k.kind, contentID, commentID, createdAtMS); err != nil {
			if isForeignKeyViolation(err) {
				// The recipient vanished between the read and the insert — their
				// comments cascade away with them, so the masked 404 is the
				// honest outcome (the reply precedent).
				return NotificationEvent{}, false, ErrNotFound
			}
			return NotificationEvent{}, false, fmt.Errorf("store: insert comment_removed notification: %w", err)
		}
		event = NotificationEvent{
			RecipientID:  authorID,
			Kind:         "comment_removed",
			ContentKind:  k.kind,
			ContentID:    contentID,
			CommentID:    commentID,
			ContentTitle: contentTitle,
		}
	}

	if err := tx.Commit(); err != nil {
		return NotificationEvent{}, false, fmt.Errorf("store: commit comment delete: %w", err)
	}
	return event, event.RecipientID != "", nil
}

// getCommentWithReplies reads one comment (with its nested replies when
// top-level) for a response — the PATCH handler's item.
func getCommentWithReplies(ctx context.Context, db *sql.DB, k ContentKind, contentID, commentID string, viewerID *string) (*Comment, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+commentItemSelect+`
		FROM `+k.comments+` c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN `+k.hearts+` h ON h.comment_id = c.id AND h.user_id = ?
		WHERE c.id = ? AND c.`+k.commentFK+` = ?`,
		viewerID, commentID, contentID)
	c, err := scanComment(row.Scan, nil)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get comment: %w", err)
	}

	var parentID sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT parent_id FROM `+k.comments+` WHERE id = ?`, commentID).Scan(&parentID); err != nil {
		return nil, fmt.Errorf("store: get comment parent: %w", err)
	}
	if parentID.Valid {
		// The reply shape carries no nested replies — leave Replies nil so
		// the handler can distinguish top-level items (the "replies = same
		// minus replies" wire rule).
		return &c, nil
	}

	items, err := attachReplies(ctx, db, k, contentID, viewerID, []Comment{c}, "")
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}
