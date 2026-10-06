package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"sick-fansubs/internal/audit"
)

// NotificationItem is the row projection for one unified feed entry.
//
// The two sources share one scan shape; each kind populates only its own
// fields (the handler omits the absent ones on the wire):
//   - account events: Kind = the event name, Result, ActorUsername,
//     TargetUsername, TargetRole;
//   - comment_reply / heart / comment: Kind = the kind, ActorUsername,
//     ContentTitle (joined at read time regardless of status — NULL for
//     deleted content), ContentKind, ContentID, CommentID. The heart
//     and comment items reuse the comment_reply shape;
//   - content_updated / new_content: the comment shape MINUS CommentID
//     (the content is the navigation target);
//   - draft_activity: the comment shape MINUS CommentID PLUS DraftAction —
//     the staff-only notice about unpublished content, which navigates to
//     the staff editor rather than the public page.
//
// ActorUsername is nil when the joined users row is gone (a deleted actor)
// or the actor is the "maintenance" pseudo-actor; the frontend renders the
// system label.
type NotificationItem struct {
	ID             string
	Kind           string
	Result         string
	ActorUsername  *string
	TargetUsername *string
	TargetRole     *string
	ContentTitle   *string
	ContentKind    string
	ContentID      string
	CommentID      string
	// DraftAction is the unpublished transition a draft_activity row reports
	// (created | updated | unpublished); empty for every other kind.
	DraftAction string
	CreatedAtMS int64
	// Read reports whether this entry is already read BY THE VIEWER. The two
	// spaces answer it differently: a user_notifications row carries its own
	// read_at_ms, the audit space carries per-viewer marks in
	// notification_reads.
	Read bool
}

// NotificationPageKey is the feed's continuation tuple:
// created_at_ms DESC, id ASC — one tuple across BOTH sources (the union's
// outer ordering). The handler's shared cursor codec payload stays
// structurally identical.
type NotificationPageKey struct {
	CreatedAtMS int64
	ID          string
}

// feedEventNames lists the five account events the staff half of the feed
// displays. Built from the audit package's event-name
// constants so the feed's scope can never drift from the emitter contract,
// and pinned equal to the frontend's `isAccountEvent` guard (that guard
// splits the feed halves on the client — the audit-row rendering branch and
// the clear-read local filter — so a drift would desync the client's kept
// rows from the server's sweep).
var feedEventNames = []string{
	audit.EventPasswordReset,
	audit.EventRoleChanged,
	audit.EventUserSuspended,
	audit.EventUserReactivated,
	audit.EventUserDeleted,
}

// feedEventsClause is the SQL form of feedEventNames. The values are package
// constants, never request input — no interpolation risk.
var feedEventsClause = func() string {
	quoted := make([]string, 0, len(feedEventNames))
	for _, name := range feedEventNames {
		quoted = append(quoted, "'"+name+"'")
	}
	return "e.event IN (" + strings.Join(quoted, ", ") + ")"
}()

// targetRoleWeightCase mirrors identity.RoleWeight for the feed's visibility
// gate (SQL cannot call Go). It matches the model for the four accepted
// roles, pinned by TestNotificationVisibilityMatrix (the draft-gate canary
// precedent); the unknown arm deliberately diverges: NULL is excluded by
// the IS NOT NULL half of the predicate, and an unknown non-NULL value
// weighs 4 — above every staff role, where the model answers -1 — so an
// unrecognized snapshot can never leak to any viewer.
const targetRoleWeightCase = `CASE e.target_role
	WHEN 'super-admin' THEN 3
	WHEN 'admin' THEN 2
	WHEN 'moderator' THEN 1
	WHEN 'user' THEN 0
	ELSE 4 END`

// visibilityPredicate is the shared staff-feed filter: the five account
// events, a role snapshot present, and the snapshot's weight at or below
// the viewer's (bound as one parameter). The snapshot — not a users JOIN —
// is the authorization input, so deleted targets keep their visibility and
// role changes never rewrite history.
var visibilityPredicate = feedEventsClause + ` AND e.target_role IS NOT NULL
		AND (` + targetRoleWeightCase + `) <= ?`

// userNotificationsFrom builds the feed's user half — the row source, its
// content-title and creator joins — and its bind arguments.
//
// The staff-only draft notice is re-gated on READ, not merely on the floor:
// below the staff floor the kind leaves the branch entirely; at the floor it
// is served only under the SAME rule the draft-visibility read applies — the
// row's creator, or a viewer weight strictly above the creator's — evaluated
// against the CURRENT content row. The write-time audience is not a standing
// grant, so a later role change that puts the creator out of view stops the
// title from being served. The creator weight takes the max over the
// per-kind joins: a non-matching join weighs -1 (the unknown-role arm), as
// does content deleted after delivery (the unattributed arm) — both remain
// visible to staff, matching the read gate.
func userNotificationsFrom(viewerID string, viewerRoleWeight int, includeStaff bool) (string, []any) {
	// The content title and creator are read from whichever content table the
	// row's kind names: one LEFT JOIN per content kind, keyed on the kind
	// literal and the row's content id (the feedEventsClause precedent —
	// package constants are spliced into the SQL, values stay bound
	// parameters).
	var titleJoins, titleCoalesce, creatorJoins strings.Builder
	titleCoalesce.WriteString("COALESCE(")
	var creatorIDs, creatorWeights []string
	for i, k := range allContentKinds {
		alias := fmt.Sprintf("t%d", i)
		creator := fmt.Sprintf("c%d", i)
		if i > 0 {
			titleCoalesce.WriteString(", ")
		}
		fmt.Fprintf(&titleJoins, "\n\t\tLEFT JOIN %s %s ON un.content_kind = '%s' AND %s.id = un.content_id", k.table, alias, k.kind, alias)
		fmt.Fprintf(&creatorJoins, "\n\t\tLEFT JOIN users %s ON %s.id = %s.creator_id", creator, creator, alias)
		fmt.Fprintf(&titleCoalesce, "%s.title", alias)
		creatorIDs = append(creatorIDs, alias+".creator_id")
		creatorWeights = append(creatorWeights, roleWeightCase(creator))
	}
	titleCoalesce.WriteString(") AS content_title")

	userWhere := `un.recipient_id = ?`
	args := []any{viewerID}
	if !includeStaff {
		userWhere += ` AND un.kind <> 'draft_activity'`
	} else {
		// Bind order: the recipient, then the draft gate's creator and weight.
		userWhere += ` AND (un.kind <> 'draft_activity' OR (COALESCE(` + strings.Join(creatorIDs, ", ") + `) = ? OR MAX(` + strings.Join(creatorWeights, ", ") + `) < ?))`
		args = append(args, viewerID, viewerRoleWeight)
	}

	branch := `SELECT un.id AS id, un.kind AS kind, NULL AS result, ua.username AS actor_username, NULL AS target_username, NULL AS target_role,
			` + titleCoalesce.String() + `, un.content_kind AS content_kind, un.content_id AS content_id, un.comment_id AS comment_id, un.draft_action AS draft_action, un.created_at_ms AS created_at_ms,
			un.read_at_ms IS NOT NULL AS is_read
		FROM user_notifications un
		LEFT JOIN users ua ON ua.id = un.actor_id` + titleJoins.String() + creatorJoins.String() + `
		WHERE ` + userWhere
	return branch, args
}

// notificationsFrom builds the feed's UNION source and its bind arguments:
// the audit read-state EXISTS binds the viewer id before the role weight, the
// dismissal anti-join binds it again, and the user branch binds it again —
// one source for the page read and the count.
func notificationsFrom(viewerID string, viewerRoleWeight int, includeStaff bool) (string, []any) {
	userBranch, userArgs := userNotificationsFrom(viewerID, viewerRoleWeight, includeStaff)

	auditBranch := `SELECT e.id AS id, e.event AS kind, e.result AS result, a.username AS actor_username, t.username AS target_username, e.target_role AS target_role,
			NULL AS content_title, NULL AS content_kind, NULL AS content_id, NULL AS comment_id, NULL AS draft_action, e.created_at_ms AS created_at_ms,
			EXISTS (SELECT 1 FROM notification_reads r WHERE r.user_id = ? AND r.event_id = e.id) AS is_read
		FROM audit_events e
		LEFT JOIN users a ON a.id = e.actor_id
		LEFT JOIN users t ON t.id = e.target_id
		WHERE ` + visibilityPredicate + `
			AND NOT EXISTS (
				SELECT 1 FROM notification_reads d
				WHERE d.user_id = ? AND d.event_id = e.id AND d.dismissed_at_ms IS NOT NULL)`

	if !includeStaff {
		return userBranch, userArgs
	}
	args := append([]any{viewerID, viewerRoleWeight, viewerID}, userArgs...)
	return auditBranch + "\n\tUNION ALL\n\t" + userBranch, args
}

// ListNotifications returns one keyset page of the viewer's unified feed,
// newest first, plus whether another page exists.
//
// includeStaff adds the role-weighted audit half (moderator+ — the handler
// derives it from identity.CanViewStaffList); a non-staff viewer gets only
// their own user_notifications rows. limit must already satisfy the wire
// contract (1..100; the handler enforces it); the store fetches limit+1
// rows so hasNextPage needs no count query. after is
// nil for the first page.
//
// Two explicit query shapes keep the keyset predicate visible (the
// blog-list pattern): the first page has no continuation predicate, later
// pages apply the strict "after" tuple on the UNION's outer columns.
func ListNotifications(ctx context.Context, db *sql.DB, viewerID string, viewerRoleWeight int, includeStaff bool, limit int, after *NotificationPageKey) ([]NotificationItem, bool, error) {
	from, args := notificationsFrom(viewerID, viewerRoleWeight, includeStaff)

	continuation := ""
	if after != nil {
		continuation = ` WHERE f.created_at_ms < ? OR (f.created_at_ms = ? AND f.id > ?)`
		args = append(args, after.CreatedAtMS, after.CreatedAtMS, after.ID)
	}
	query := `SELECT f.id, f.kind, f.result, f.actor_username, f.target_username, f.target_role,
			f.content_title, f.content_kind, f.content_id, f.comment_id, f.draft_action, f.created_at_ms, f.is_read
		FROM (` + from + `) f` + continuation + `
		ORDER BY f.created_at_ms DESC, f.id ASC
		LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list notifications: %w", err)
	}
	defer rows.Close()

	var items []NotificationItem
	for rows.Next() {
		var (
			it        NotificationItem
			actor     sql.NullString
			target    sql.NullString
			role      sql.NullString
			title     sql.NullString
			contentK  sql.NullString
			contentID sql.NullString
			commentID sql.NullString
			draft     sql.NullString
			result    sql.NullString
			isRead    int64
		)
		if err := rows.Scan(
			&it.ID, &it.Kind, &result, &actor, &target, &role,
			&title, &contentK, &contentID, &commentID, &draft, &it.CreatedAtMS, &isRead,
		); err != nil {
			return nil, false, fmt.Errorf("store: scan notification: %w", err)
		}
		it.Read = isRead != 0
		it.Result = nullStringValue(result)
		it.ActorUsername = nullStringPtr(actor)
		it.TargetUsername = nullStringPtr(target)
		it.TargetRole = nullStringPtr(role)
		it.ContentTitle = nullStringPtr(title)
		it.ContentKind = nullStringValue(contentK)
		it.ContentID = nullStringValue(contentID)
		it.CommentID = nullStringValue(commentID)
		it.DraftAction = nullStringValue(draft)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate notifications: %w", err)
	}

	// The limit+1th row only proves a next page exists; it is not part of
	// this page's items.
	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return items, hasNext, nil
}

// CountNotifications returns the feed's filtered row count — the pager's
// "of N" number — under the same scope, visibility predicate, and joins.
func CountNotifications(ctx context.Context, db *sql.DB, viewerID string, viewerRoleWeight int, includeStaff bool) (int, error) {
	from, args := notificationsFrom(viewerID, viewerRoleWeight, includeStaff)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+from+`) f`, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count notifications: %w", err)
	}
	return n, nil
}

// UnreadNotificationCount returns the number of unread feed entries — the
// header badge, which sums both sources. The staff half is the anti-join on
// notification_reads; the user half counts the user branch's unread rows, so
// the draft re-gate applies to the badge exactly as it applies to the feed.
func UnreadNotificationCount(ctx context.Context, db *sql.DB, viewerID string, viewerRoleWeight int, includeStaff bool) (int64, error) {
	var count int64
	if includeStaff {
		userFrom, userArgs := userNotificationsFrom(viewerID, viewerRoleWeight, true)
		query := `SELECT
			(SELECT COUNT(*) FROM audit_events e
			 WHERE ` + visibilityPredicate + `
			   AND NOT EXISTS (
				SELECT 1 FROM notification_reads r
				WHERE r.user_id = ? AND r.event_id = e.id))
			+
			(SELECT COUNT(*) FROM (` + userFrom + `) f WHERE f.is_read = 0)`
		args := append([]any{viewerRoleWeight, viewerID}, userArgs...)
		if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return 0, fmt.Errorf("store: count unread notifications: %w", err)
		}
		return count, nil
	}

	userFrom, userArgs := userNotificationsFrom(viewerID, viewerRoleWeight, false)
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (`+userFrom+`) f WHERE f.is_read = 0`,
		userArgs...).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: count unread notifications: %w", err)
	}
	return count, nil
}

// MarkNotificationRead marks one feed entry read across BOTH id spaces,
// with per-space authorization:
//
//   - a user_notifications id must belong to the viewer and stay inside the
//     viewer's branch — below the staff floor the re-gated draft kind is not
//     addressable, so a hidden row answers the same ErrNotFound as an
//     unknown one;
//   - an audit_events id must satisfy the feed visibility predicate and not
//     already be dismissed for the viewer — unknown, invisible, and dismissed
//     ids are the same masked ErrNotFound (the read operation never reveals
//     that an entry exists).
//
// The audit path's check-then-insert gap is the documented benign TOCTOU
// (the disambiguation-SELECT precedent): the only racer is the deferred
// retention cleanup deleting the event between the two statements, which
// surfaces as the insert's FK error, never as data corruption.
func MarkNotificationRead(ctx context.Context, db *sql.DB, viewerID, notificationID string, viewerRoleWeight int, includeStaff bool, readAtMS int64) error {
	userWhere := `WHERE id = ? AND recipient_id = ?`
	args := []any{readAtMS, notificationID, viewerID}
	if !includeStaff {
		userWhere += ` AND kind <> 'draft_activity'`
	}
	res, err := db.ExecContext(ctx, `
		UPDATE user_notifications SET read_at_ms = ? `+userWhere, args...)
	if err != nil {
		return fmt.Errorf("store: mark notification read (user space): %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: mark notification read rows affected: %w", err)
	}
	if n == 1 {
		return nil
	}

	if !includeStaff {
		return ErrNotFound
	}

	existsQuery := `SELECT 1
		FROM audit_events e
		WHERE e.id = ? AND ` + visibilityPredicate + `
			AND NOT EXISTS (
				SELECT 1 FROM notification_reads d
				WHERE d.user_id = ? AND d.event_id = e.id AND d.dismissed_at_ms IS NOT NULL)`

	var one int
	err = db.QueryRowContext(ctx, existsQuery, notificationID, viewerRoleWeight, viewerID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: check notification visibility: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO notification_reads (user_id, event_id, read_at_ms)
		VALUES (?, ?, ?)`,
		viewerID, notificationID, readAtMS); err != nil {
		return fmt.Errorf("store: mark notification read: %w", err)
	}
	return nil
}

// MarkAllNotificationsRead marks every visible feed entry read for the
// viewer across both sources in one pass. Both
// writes absorb repeats, so the operation is idempotent; the returned
// count is the number of rows newly marked (the handler logs it). Below the
// staff floor the re-gated draft kind is excluded from the user-space sweep.
func MarkAllNotificationsRead(ctx context.Context, db *sql.DB, viewerID string, viewerRoleWeight int, includeStaff bool, readAtMS int64) (int64, error) {
	userWhere := `WHERE recipient_id = ? AND read_at_ms IS NULL`
	args := []any{readAtMS, viewerID}
	if !includeStaff {
		userWhere += ` AND kind <> 'draft_activity'`
	}
	res, err := db.ExecContext(ctx, `
		UPDATE user_notifications SET read_at_ms = ? `+userWhere, args...)
	if err != nil {
		return 0, fmt.Errorf("store: mark all read (user space): %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: mark all read rows affected: %w", err)
	}

	if !includeStaff {
		return n, nil
	}

	query := `INSERT OR IGNORE INTO notification_reads (user_id, event_id, read_at_ms)
		SELECT ?, e.id, ?
		FROM audit_events e
		WHERE ` + visibilityPredicate + `
			AND NOT EXISTS (
				SELECT 1 FROM notification_reads d
				WHERE d.user_id = ? AND d.event_id = e.id AND d.dismissed_at_ms IS NOT NULL)`
	res, err = db.ExecContext(ctx, query, viewerID, readAtMS, viewerRoleWeight, viewerID)
	if err != nil {
		return 0, fmt.Errorf("store: mark all notifications read: %w", err)
	}
	staff, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: mark all notifications read rows affected: %w", err)
	}
	return n + staff, nil
}
