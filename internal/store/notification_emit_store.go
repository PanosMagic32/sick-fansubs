package store

import (
	"context"
	"database/sql"
	"fmt"

	"sick-fansubs/internal/id"
)

// notifyFollowersTx inserts one user_notifications row per follower of the
// content (the actor excluded — self-suppression, the reply precedent) and
// returns the per-follower fan-out events, all inside the CALLER'S
// transaction (a notification failure fails the write). It is the shared
// fan-out of the 'comment' and 'content_updated' emitters — the
// rule-of-three extraction, the Tx-helper precedent: the caller owns the
// transaction, the store owns the SQL.
//
// commentID names the deep-link target for the comment kinds; pass nil for
// content_updated (the content is the target — the event's CommentID stays
// empty). The follower rows are read in the SAME transaction, so no
// follower can vanish between the SELECT and the inserts — the inserts
// carry no FK race mapping, and any failure is an ordinary error.
//
// The per-row notification ids are minted INSIDE the store: the
// single-recipient reply/heart paths keep the handler-minted id seam, but
// a multi-recipient fan-out cannot mint its unbounded id count at the
// handler layer — the deliberate seam split (see the store AGENTS).
// A suspended recipient is skipped: suspension revokes the account, so it
// must also stop the outbound channel (the active-recipient rule every
// emitter shares).
func notifyFollowersTx(ctx context.Context, tx *sql.Tx, notificationKind, actorID string, k ContentKind, contentID string, commentID *string, contentTitle string, createdAtMS int64) ([]NotificationEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT f.user_id FROM content_follows f
		JOIN users u ON u.id = f.user_id
		WHERE f.content_kind = ? AND f.content_id = ? AND f.user_id <> ?
		  AND u.status = 'active'`,
		k.kind, contentID, actorID)
	if err != nil {
		return nil, fmt.Errorf("store: list notification followers: %w", err)
	}
	defer rows.Close()

	var events []NotificationEvent
	for rows.Next() {
		var followerID string
		if err := rows.Scan(&followerID); err != nil {
			return nil, fmt.Errorf("store: scan notification follower: %w", err)
		}
		notificationID, err := id.New()
		if err != nil {
			return nil, fmt.Errorf("store: notification id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_notifications (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			notificationID, followerID, notificationKind, actorID, k.kind, contentID, commentID, createdAtMS); err != nil {
			return nil, fmt.Errorf("store: insert follower notification: %w", err)
		}
		events = append(events, NotificationEvent{
			RecipientID:  followerID,
			Kind:         notificationKind,
			ContentKind:  k.kind,
			ContentID:    contentID,
			CommentID:    commentIDValue(commentID),
			ContentTitle: contentTitle,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate notification followers: %w", err)
	}
	return events, nil
}

// commentIDValue returns the dereferenced comment id, or "" for nil (the
// content-level kinds carry no comment).
func commentIDValue(commentID *string) string {
	if commentID == nil {
		return ""
	}
	return *commentID
}

// Draft-activity actions — the transition a
// 'draft_activity' row reports. The values are the column CHECK's own
// vocabulary (migration 0019) and travel to the wire as draftAction.
const (
	draftActionCreated     = "created"
	draftActionUpdated     = "updated"
	draftActionUnpublished = "unpublished"
)

// notifyDraftActivityTx inserts one 'draft_activity' row per STAFF member who
// opted in and can SEE the row the notice is about, and returns the
// per-recipient fan-out events — all inside the CALLER'S transaction.
// The three emitting transitions are create-as-draft (created),
// a non-published edit (updated), and leaving published (unpublished); draft
// edits that LEAVE the unpublished world are new_content, not this
// kind.
//
// The audience is the draft-visibility gate, not a role list: a notice goes to a user
// whose view of this row is NOT masked — its creator, or a weight strictly
// above the creator's (same-role peers never learn about each other's
// unpublished rows). The role-weight comparison mirrors
// the read gate's own expression, roleWeightCase. On top of the gate:
//
//   - the STAFF floor (moderator+) is stated explicitly, because the gate
//     alone would let a plain user through for a row with no creator (the
//     weight CASE's ELSE -1 arm) if such a user ever held a preference row;
//   - a recipient must have opted in — an explicit push_enabled = 1 row for
//     this kind, NO role default (the new_content broadcast's
//     mirror image, which defaults staff ON);
//   - the ACTOR is excluded (self-suppression, the follower fan-out
//     precedent), so an editor never gets a notice for their own write;
//   - a suspended recipient is skipped (the active-recipient rule).
//
// The preference gates BOTH the in-app row and push for this kind, like
// new_content.
func notifyDraftActivityTx(ctx context.Context, tx *sql.Tx, actorID, action string, k ContentKind, contentID, contentTitle string, createdAtMS int64) ([]NotificationEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT u.id FROM users u
		JOIN `+k.table+` p ON p.id = ?
		LEFT JOIN users c ON c.id = p.creator_id
		WHERE u.id <> ?
		  AND u.status = 'active'
		  AND `+staffRoleClause+`
		  AND (p.creator_id = u.id OR (`+roleWeightCase("c")+`) < (`+roleWeightCase("u")+`))
		  AND EXISTS (
			SELECT 1 FROM notification_preferences np
			WHERE np.user_id = u.id AND np.kind = 'draft_activity' AND np.push_enabled = 1
		  )
		ORDER BY u.id`, contentID, actorID)
	if err != nil {
		return nil, fmt.Errorf("store: list draft activity recipients: %w", err)
	}
	defer rows.Close()

	var events []NotificationEvent
	for rows.Next() {
		var recipientID string
		if err := rows.Scan(&recipientID); err != nil {
			return nil, fmt.Errorf("store: scan draft activity recipient: %w", err)
		}
		notificationID, err := id.New()
		if err != nil {
			return nil, fmt.Errorf("store: notification id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_notifications (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, draft_action, created_at_ms)
			VALUES (?, ?, 'draft_activity', ?, ?, ?, NULL, ?, ?)`,
			notificationID, recipientID, actorID, k.kind, contentID, action, createdAtMS); err != nil {
			return nil, fmt.Errorf("store: insert draft activity notification: %w", err)
		}
		events = append(events, NotificationEvent{
			RecipientID:  recipientID,
			Kind:         "draft_activity",
			ContentKind:  k.kind,
			ContentID:    contentID,
			ContentTitle: contentTitle,
			DraftAction:  action,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate draft activity recipients: %w", err)
	}
	return events, nil
}

// staffRoleClause is the SQL spelling of the staff floor
// (identity.RoleWeight(role) >= RoleWeight(RoleModerator); SQL cannot call Go)
// for the queries that range over users: the new_content broadcast reads it as
// its default-ON set (pinned against the model by
// TestNewContentBroadcastRoleDefaultsMirrorModel) and the draft-activity
// notice as its audience floor (pinned by the emission matrix) — one
// home for the role set, so a floor change cannot drift between them.
const staffRoleClause = `(u.role IN ('moderator', 'admin', 'super-admin'))`

// notifyNewContentTx inserts one user_notifications row per opted-in
// recipient of the 'new_content' broadcast and returns the per-recipient
// fan-out events, all inside the CALLER'S transaction.
// The recipient set is every user whose EFFECTIVE new_content preference is
// ON — an explicit push_enabled = 1 row, or the role default when no row
// exists (moderator+ on) — minus the actor
// (the creator/updater never gets their own publish).
//
// The preference gates BOTH the in-app row and push for this broadcast
// kind: there is no follow relationship to lean on, so the recipient set is
// the opted-in audience itself. A suspended recipient is skipped (the
// active-recipient rule) — the in-app row and its push both stay away from
// an account whose sessions were already revoked.
//
// Like notifyFollowersTx, the per-row notification ids are minted INSIDE
// the store (the multi-recipient seam split, see the store AGENTS).
func notifyNewContentTx(ctx context.Context, tx *sql.Tx, actorID string, k ContentKind, contentID, contentTitle string, createdAtMS int64) ([]NotificationEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT u.id FROM users u
		WHERE u.id <> ?
		  AND u.status = 'active'
		  AND (
			EXISTS (
				SELECT 1 FROM notification_preferences np
				WHERE np.user_id = u.id AND np.kind = 'new_content' AND np.push_enabled = 1
			)
			OR (
				NOT EXISTS (
					SELECT 1 FROM notification_preferences np
					WHERE np.user_id = u.id AND np.kind = 'new_content'
				)
				AND `+staffRoleClause+`
			)
		  )
		ORDER BY u.id`, actorID)
	if err != nil {
		return nil, fmt.Errorf("store: list new_content recipients: %w", err)
	}
	defer rows.Close()

	var events []NotificationEvent
	for rows.Next() {
		var recipientID string
		if err := rows.Scan(&recipientID); err != nil {
			return nil, fmt.Errorf("store: scan new_content recipient: %w", err)
		}
		notificationID, err := id.New()
		if err != nil {
			return nil, fmt.Errorf("store: notification id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_notifications (id, recipient_id, kind, actor_id, content_kind, content_id, comment_id, created_at_ms)
			VALUES (?, ?, 'new_content', ?, ?, ?, NULL, ?)`,
			notificationID, recipientID, actorID, k.kind, contentID, createdAtMS); err != nil {
			return nil, fmt.Errorf("store: insert new_content notification: %w", err)
		}
		events = append(events, NotificationEvent{
			RecipientID:  recipientID,
			Kind:         "new_content",
			ContentKind:  k.kind,
			ContentID:    contentID,
			ContentTitle: contentTitle,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate new_content recipients: %w", err)
	}
	return events, nil
}
