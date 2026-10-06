package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Notifications feed: SEVEN endpoints over TWO sources — the five account audit events
// (role-weighted, moderator+ only) and the viewer's own user_notifications
// rows (comment_reply, heart, comment, content_updated, new_content, and
// comment_removed; the staff-only draft_activity kind leaves the user half
// below the staff floor and is served at the floor only under the draft
// gate's current weight rule).
// The floor is AUTHENTICATED (any signed-in user gets the inbox); the
// staff half of the feed is simply empty for non-staff viewers.
//
// Every item carries `read`: the user space answers it from the
// row's own read_at_ms, the audit space from the viewer's notification_reads
// mark. Deletion covers both id spaces: an event row is deleted, while a
// visible audit event is DISMISSED for the viewer — the ledger row itself is
// never deleted, and a dismissed entry leaves the list, the count, and the
// badge.
//
// The precise contract lives in docs/api/openapi.yaml (operationIds
// listNotifications, getNotificationUnreadCount, markNotificationRead,
// markAllNotificationsRead, deleteNotification, clearReadNotifications,
// deleteAllNotifications).
// The route chain: reads on RequestID → TrustedOrigin → Session →
// ForcePasswordChange; the writes additionally CSRF (the forced-change
// gate applies to the whole surface).

const (
	notificationsDefaultLimit = 20
	notificationsMaxLimit     = 100

	// notificationKindCommentReply, notificationKindHeart,
	// notificationKindComment, notificationKindContentUpdated,
	// notificationKindNewContent, notificationKindCommentRemoved, and
	// notificationKindDraftActivity are the user_notifications feed kinds — the
	// OpenAPI discriminator values. The comment kinds share the comment-item
	// wire shape; content_updated and new_content carry no comment id.
	// comment_removed is the one ACTORLESS kind — the moderator is never
	// named, so its actorUsername is always null.
	// draft_activity is the one STAFF-ONLY, opt-in kind: it reports
	// unpublished content to the draft-visibility audience and carries draftAction.
	notificationKindCommentReply   = "comment_reply"
	notificationKindHeart          = "heart"
	notificationKindComment        = "comment"
	notificationKindContentUpdated = "content_updated"
	notificationKindNewContent     = "new_content"
	notificationKindCommentRemoved = "comment_removed"
	notificationKindDraftActivity  = "draft_activity"
)

// accountNotificationItemDTO is the wire projection of one account audit
// event (the shape STAYS for the account events): targetUsername/targetRole
// are always present, null when
// the referenced account is gone. No contentTitle key — the two kinds
// carry disjoint fields (the OpenAPI oneOf discriminator).
type accountNotificationItemDTO struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	Result         string  `json:"result"`
	ActorUsername  *string `json:"actorUsername"`
	TargetUsername *string `json:"targetUsername"`
	TargetRole     *string `json:"targetRole"`
	CreatedAt      string  `json:"createdAt"`
	// Read is the per-viewer read state — present on every
	// item shape so the page can style unread entries and swap each entry's
	// delete affordance.
	Read bool `json:"read"`
}

// commentNotificationItemDTO is the wire projection of one comment-related
// user_notifications entry — comment_reply, heart, comment, and
// comment_removed: contentTitle is always present (null for deleted
// content). commentId is
// the deep-link fragment target for the re-homing kinds and the removed
// comment's own id for comment_removed — which navigates to the CONTENT,
// without a fragment, because the comment is gone.
// actorUsername is always null for comment_removed: the notice is
// actorless by ruling.
type commentNotificationItemDTO struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	ActorUsername *string `json:"actorUsername"`
	ContentTitle  *string `json:"contentTitle"`
	ContentKind   string  `json:"contentKind"`
	ContentID     string  `json:"contentId"`
	CommentID     string  `json:"commentId"`
	CreatedAt     string  `json:"createdAt"`
	Read          bool    `json:"read"`
}

// contentNotificationItemDTO is the wire projection of one content-level
// user_notifications entry — content_updated and
// new_content: the comment-item shape MINUS
// commentId (the content itself is the navigation target).
type contentNotificationItemDTO struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	ActorUsername *string `json:"actorUsername"`
	ContentTitle  *string `json:"contentTitle"`
	ContentKind   string  `json:"contentKind"`
	ContentID     string  `json:"contentId"`
	CreatedAt     string  `json:"createdAt"`
	Read          bool    `json:"read"`
}

// draftActivityNotificationItemDTO is the wire projection of one
// draft_activity entry: the content-item shape PLUS the
// transition it reports. The action is always present for this kind — the
// row's draft_action column is bound to it by a per-kind CHECK (migration
// 0019) — and the UI picks the sentence from it.
type draftActivityNotificationItemDTO struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	ActorUsername *string `json:"actorUsername"`
	DraftAction   string  `json:"draftAction"`
	ContentTitle  *string `json:"contentTitle"`
	ContentKind   string  `json:"contentKind"`
	ContentID     string  `json:"contentId"`
	CreatedAt     string  `json:"createdAt"`
	Read          bool    `json:"read"`
}

// notificationItemDTO builds the per-kind wire item for one store row —
// the OpenAPI NotificationItem oneOf and the TS discriminated union mirror
// this split exactly.
func notificationItemDTO(it store.NotificationItem) any {
	switch it.Kind {
	case notificationKindCommentReply, notificationKindHeart, notificationKindComment, notificationKindCommentRemoved:
		return commentNotificationItemDTO{
			ID:            it.ID,
			Kind:          it.Kind,
			ActorUsername: it.ActorUsername,
			ContentTitle:  it.ContentTitle,
			ContentKind:   it.ContentKind,
			ContentID:     it.ContentID,
			CommentID:     it.CommentID,
			CreatedAt:     formatAPITime(it.CreatedAtMS),
			Read:          it.Read,
		}
	case notificationKindContentUpdated, notificationKindNewContent:
		return contentNotificationItemDTO{
			ID:            it.ID,
			Kind:          it.Kind,
			ActorUsername: it.ActorUsername,
			ContentTitle:  it.ContentTitle,
			ContentKind:   it.ContentKind,
			ContentID:     it.ContentID,
			CreatedAt:     formatAPITime(it.CreatedAtMS),
			Read:          it.Read,
		}
	case notificationKindDraftActivity:
		return draftActivityNotificationItemDTO{
			ID:            it.ID,
			Kind:          it.Kind,
			ActorUsername: it.ActorUsername,
			DraftAction:   it.DraftAction,
			ContentTitle:  it.ContentTitle,
			ContentKind:   it.ContentKind,
			ContentID:     it.ContentID,
			CreatedAt:     formatAPITime(it.CreatedAtMS),
			Read:          it.Read,
		}
	}
	return accountNotificationItemDTO{
		ID:             it.ID,
		Kind:           it.Kind,
		Result:         it.Result,
		ActorUsername:  it.ActorUsername,
		TargetUsername: it.TargetUsername,
		TargetRole:     it.TargetRole,
		CreatedAt:      formatAPITime(it.CreatedAtMS),
		Read:           it.Read,
	}
}

// unreadCountResponse is the badge payload.
type unreadCountResponse struct {
	Unread int64 `json:"unread"`
}

// notificationsFloor is the shared authorization gate:
// any signed-in user — a nil session is the generic 401. Non-staff viewers
// get the user_notifications half minus the staff-only draft kind.
func notificationsFloor(w http.ResponseWriter, r *http.Request) *identity.SessionUser {
	su, ok := requireSession(w, r, "notifications require authentication")
	if !ok {
		return nil
	}
	return su
}

// notificationsScope derives the per-viewer feed scope: includeStaff adds
// the role-weighted audit half (moderator+ — identity.CanViewStaffList).
func notificationsScope(su *identity.SessionUser) (viewerID string, weight int, includeStaff bool) {
	return su.UserID, identity.RoleWeight(su.Role), identity.CanViewStaffList(su.Role)
}

// NotificationsList returns GET /api/v1/notifications: one keyset page of
// the viewer's unified feed, newest first.
func NotificationsList(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su := notificationsFloor(w, r)
		if su == nil {
			return
		}
		_, limit, after, ok := parseKeysetParams(w, r, notificationsDefaultLimit, notificationsMaxLimit, notificationCursorPrefix)
		if !ok {
			return
		}

		viewerID, weight, includeStaff := notificationsScope(su)
		writeKeysetPage(w, r, "notifications list", keysetPage[store.NotificationItem, any]{
			count: func(ctx context.Context) (int, error) {
				return store.CountNotifications(ctx, db, viewerID, weight, includeStaff)
			},
			read: func(ctx context.Context) ([]store.NotificationItem, bool, error) {
				var key *store.NotificationPageKey
				if after != nil {
					key = &store.NotificationPageKey{CreatedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListNotifications(ctx, db, viewerID, weight, includeStaff, limit, key)
			},
			// The per-kind DTO dispatch is the OpenAPI oneOf discriminator on
			// `kind`; notificationItemDTO is total, so the projection never
			// fails.
			item: func(it store.NotificationItem) any {
				return notificationItemDTO(it)
			},
			next: func(last store.NotificationItem) string {
				return encodeCursor(notificationCursorPrefix, last.CreatedAtMS, last.ID)
			},
		})
	}
}

// NotificationsUnreadCount returns GET /api/v1/notifications/unread-count:
// the number of unread entries across BOTH sources, for the header badge.
// No query parameters are accepted.
func NotificationsUnreadCount(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su := notificationsFloor(w, r)
		if su == nil {
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}

		viewerID, weight, includeStaff := notificationsScope(su)
		count, err := store.UnreadNotificationCount(r.Context(), db, viewerID, weight, includeStaff)
		if err != nil {
			writeInternalError(w, r, logger, "notifications unread count failed", "error", err)
			return
		}
		writeJSON(w, r, http.StatusOK, unreadCountResponse{Unread: count})
	}
}

// NotificationsMarkRead returns POST /api/v1/notifications/{id}/read:
// mark one feed entry read (idempotent 204) across both id spaces, with
// per-space authorization. now stamps read_at_ms (nil → time.Now) — the
// application clock, never a schema default (docs/patterns/go/sql-mapping.md).
func NotificationsMarkRead(db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		su := notificationsFloor(w, r)
		if su == nil {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid notification id")
			return
		}

		// The store masks unknown ids and entries outside the viewer's
		// scope as the SAME ErrNotFound — the read operation never reveals
		// that an entry exists.
		viewerID, weight, includeStaff := notificationsScope(su)
		err := store.MarkNotificationRead(r.Context(), db, viewerID, id, weight, includeStaff, now().UnixMilli())
		if err != nil {
			writeStoreError(w, r, err, "mark notification read", "error", err)
			return
		}
		writeNoContent(w, r)
	}
}

// NotificationsMarkAllRead returns POST /api/v1/notifications/read-all:
// mark every visible feed entry read across both sources (idempotent 204).
// now stamps read_at_ms (nil → time.Now).
func NotificationsMarkAllRead(db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su := notificationsFloor(w, r)
		if su == nil {
			return
		}

		viewerID, weight, includeStaff := notificationsScope(su)
		n, err := store.MarkAllNotificationsRead(r.Context(), db, viewerID, weight, includeStaff, now().UnixMilli())
		if err != nil {
			writeInternalError(w, r, logger, "mark all notifications read failed", "error", err)
			return
		}
		logger.InfoContext(r.Context(), "notifications marked read",
			"count", n, "userId", viewerID)
		writeNoContent(w, r)
	}
}

// NotificationsDelete returns DELETE /api/v1/notifications/{id}: remove ONE
// entry from the viewer's feed. An event row is deleted; a visible audit
// event — the ledger half — is DISMISSED for the viewer only
// (notification_reads.dismissed_at_ms), never deleted: the audit record
// survives for the ledger and for other staff. Unknown, foreign, invisible,
// and already-dismissed ids are the masked 404, and idempotence is
// deliberately NOT offered — a second delete of the same id is a 404, because
// the entry is gone from the viewer's feed. now stamps the dismissal
// (nil → time.Now) — the application clock, never a schema default.
func NotificationsDelete(db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su := notificationsFloor(w, r)
		if su == nil {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid notification id")
			return
		}

		viewerID, weight, includeStaff := notificationsScope(su)
		deleted, err := store.DeleteNotification(r.Context(), db, viewerID, id, weight, includeStaff, now().UnixMilli())
		if err != nil {
			writeInternalError(w, r, logger, "delete notification failed", "error", err)
			return
		}
		if !deleted {
			NotFound(w, r)
			return
		}
		writeNoContent(w, r)
	}
}

// NotificationsClearRead returns POST /api/v1/notifications/clear-read:
// remove every READ event row of the viewer — the bulk sibling of the
// per-item delete, idempotent 204. Unread entries survive by construction,
// and the audit half is untouched (the per-viewer dismissal sweep belongs to
// delete-all); the handler logs the count.
func NotificationsClearRead(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su := notificationsFloor(w, r)
		if su == nil {
			return
		}

		_, _, includeStaff := notificationsScope(su)
		n, err := store.DeleteReadNotifications(r.Context(), db, su.UserID, includeStaff)
		if err != nil {
			writeInternalError(w, r, logger, "clear read notifications failed", "error", err)
			return
		}
		logger.InfoContext(r.Context(), "read notifications cleared",
			"count", n, "userId", su.UserID)
		writeNoContent(w, r)
	}
}

// NotificationsDeleteAll returns POST /api/v1/notifications/delete-all: empty
// the viewer's whole feed in one press — every event row of the viewer is
// deleted and every visible audit event is dismissed for the viewer, while
// the audit ledger rows themselves stay intact. Idempotent 204; the handler
// logs the count. now stamps the dismissal (nil → time.Now).
func NotificationsDeleteAll(db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su := notificationsFloor(w, r)
		if su == nil {
			return
		}

		viewerID, weight, includeStaff := notificationsScope(su)
		n, err := store.DeleteAllNotifications(r.Context(), db, viewerID, weight, includeStaff, now().UnixMilli())
		if err != nil {
			writeInternalError(w, r, logger, "delete all notifications failed", "error", err)
			return
		}
		logger.InfoContext(r.Context(), "notifications deleted",
			"count", n, "userId", viewerID)
		writeNoContent(w, r)
	}
}
