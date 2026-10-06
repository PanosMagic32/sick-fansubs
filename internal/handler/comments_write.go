package handler

import (
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/id"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store"
)

// maxCommentBodySize is the comment create/edit body bound: 5,000 runes
// ≤ 20 KB plus JSON escaping/framing — ~32 KiB. Bounded
// BEFORE decoding.
const maxCommentBodySize = 32 * 1024

// CommentsCreate returns POST /api/v1/{segment}/{id}/comments — create a
// top-level comment (auth + CSRF via the chain). 201 + Location pointing
// at the thread list with ?focus=<new id> (the deep-link resource — there
// is no single-comment GET).
func CommentsCreate(kind commentKind, db *sql.DB, notify push.Notifier, now func() time.Time, limiter *middleware.RateLimiter) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" comment create requires authentication")
		if !ok {
			return
		}
		contentID := r.PathValue("id")
		if !publicIDPattern.MatchString(contentID) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}

		var req commentBodyRequest
		if err := readJSONLimit(w, r, &req, maxCommentBodySize); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}
		body, ok := validateCommentBody(w, r, req.Body)
		if !ok {
			return
		}

		if !allowUserBucket(w, r, limiter, "comment-create", su.UserID) {
			return
		}

		commentID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "comment id generation failed", "error", err)
			return
		}

		events, err := store.CreateComment(r.Context(), db, kind.content, contentID, su.UserID, body, commentID, now().UnixMilli())
		if err != nil {
			writeStoreError(w, r, err, kind.label+" comment create", "error", err)
			return
		}

		// 'comment' fan-out: the per-follower
		// notification rows committed in the create transaction — hand each
		// event to the push channel AFTER the commit (the reply precedent).
		if notify != nil {
			for _, ev := range events {
				notify.NotifyPush(r.Context(), push.Event{
					RecipientID:   ev.RecipientID,
					Kind:          ev.Kind,
					ActorUsername: su.Username,
					ContentTitle:  ev.ContentTitle,
					ContentKind:   ev.ContentKind,
					ContentID:     ev.ContentID,
					CommentID:     ev.CommentID,
				})
			}
		}
		writeCreatedWithLocation(w, r, kind.threadURL(contentID, commentID))
	}
}

// CommentsReply returns POST /api/v1/{segment}/{id}/comments/{commentId}/replies
// — reply to a TOP-LEVEL comment only. 201 + Location (?focus=<reply id>);
// the comment_reply notification is written in the same transaction
// (self-replies suppressed).
func CommentsReply(kind commentKind, db *sql.DB, notify push.Notifier, now func() time.Time, limiter *middleware.RateLimiter) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" comment reply requires authentication")
		if !ok {
			return
		}
		contentID := r.PathValue("id")
		if !publicIDPattern.MatchString(contentID) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}
		parentID := r.PathValue("commentId")
		if !publicIDPattern.MatchString(parentID) {
			writeBadRequest(w, r, "invalid comment id")
			return
		}

		var req commentBodyRequest
		if err := readJSONLimit(w, r, &req, maxCommentBodySize); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}
		body, ok := validateCommentBody(w, r, req.Body)
		if !ok {
			return
		}

		if !allowUserBucket(w, r, limiter, "comment-reply", su.UserID) {
			return
		}

		replyID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "reply id generation failed", "error", err)
			return
		}
		notificationID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "notification id generation failed", "error", err)
			return
		}

		event, emitted, err := store.CreateCommentReply(r.Context(), db, kind.content, contentID, parentID, su.UserID, body, replyID, notificationID, now().UnixMilli())
		if err != nil {
			writeStoreError(w, r, err, kind.label+" comment reply", "error", err)
			return
		}
		// The notification row committed in the same transaction — hand the
		// event to the push channel AFTER the commit.
		if emitted && notify != nil {
			notify.NotifyPush(r.Context(), push.Event{
				RecipientID:   event.RecipientID,
				Kind:          event.Kind,
				ActorUsername: su.Username,
				ContentTitle:  event.ContentTitle,
				ContentKind:   event.ContentKind,
				ContentID:     event.ContentID,
				CommentID:     event.CommentID,
			})
		}
		writeCreatedWithLocation(w, r, kind.threadURL(contentID, replyID))
	}
}

// CommentsUpdate returns PATCH /api/v1/{segment}/{id}/comments/{commentId}
// — author-only body edit. 200 + the updated item; the edited
// marker derives from updatedAt > createdAt on the client.
func CommentsUpdate(kind commentKind, db *sql.DB, publicBaseURL string, now func() time.Time, limiter *middleware.RateLimiter) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, kind.label+" comment update requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}
		commentID := r.PathValue("commentId")
		if !publicIDPattern.MatchString(commentID) {
			writeBadRequest(w, r, "invalid comment id")
			return
		}

		var req commentBodyRequest
		if err := readJSONLimit(w, r, &req, maxCommentBodySize); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}
		body, ok := validateCommentBody(w, r, req.Body)
		if !ok {
			return
		}

		if !allowUserBucket(w, r, limiter, "comment-edit", su.UserID) {
			return
		}

		c, err := store.UpdateComment(r.Context(), db, kind.content, id, commentID, su.UserID, body, now().UnixMilli())
		if err != nil {
			writeStoreError(w, r, err, kind.label+" comment update", "error", err)
			return
		}
		writeJSON(w, r, http.StatusOK, kind.itemDTO(publicBaseURL, *c, c.Replies != nil))
	}
}

// CommentsDelete returns DELETE /api/v1/{segment}/{id}/comments/{commentId}
// — author (self) or moderator+ delete, hard cascade, 204. A
// staff deletion of ANOTHER user's comment emits the ledger-only
// comment_deleted audit event (actor + the comment's author as target) AND
// the comment_removed notification to that author (in-tx with the delete,
// push fanned out after the commit);
// self-deletes — including a staff member deleting their own comment —
// emit neither. The notice is ACTORLESS (the actorless ruling): the push
// event carries no actor username and the row stores a NULL actor_id, so
// the moderator is never named to the moderated author. DELETE is exempt
// from the published-only gate for the author and the staff floor
// (moderation must work on archived/draft content); a NON-author below the
// staff floor on unpublished content answers the content detail's masked
// 404, so the gate stays no existence oracle.
func CommentsDelete(kind commentKind, db *sql.DB, notify push.Notifier, now func() time.Time, limiter *middleware.RateLimiter) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" comment delete requires authentication")
		if !ok {
			return
		}
		contentID := r.PathValue("id")
		if !publicIDPattern.MatchString(contentID) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}
		commentID := r.PathValue("commentId")
		if !publicIDPattern.MatchString(commentID) {
			writeBadRequest(w, r, "invalid comment id")
			return
		}

		// The bucket is applied BEFORE the store reads (create/reply/edit/heart
		// do the same): a hot bucket answers 429 without touching the database.
		if !allowUserBucket(w, r, limiter, "comment-delete", su.UserID) {
			return
		}

		authorID, published, err := store.CommentAuthorID(r.Context(), db, kind.content, contentID, commentID)
		if err != nil {
			writeStoreError(w, r, err, kind.label+" comment author lookup", "error", err)
			return
		}

		isSelf := su.UserID == authorID
		if !isSelf && !identity.CanModerateContent(su.Role) {
			// On unpublished content the refusal is the content detail's own
			// masked 404: a plain user must not learn that a comment exists on
			// a post they cannot read. The author and the staff floor keep
			// their paths (moderation works on archived/draft content).
			if !published {
				NotFound(w, r)
				return
			}
			writeForbidden(w, r, "comment delete requires the author or a staff role")
			return
		}

		// Only a non-author delete can notify anyone — mint the id on that
		// path (the reply/heart precedent).
		var notificationID string
		if !isSelf {
			notificationID, err = id.New()
			if err != nil {
				writeInternalError(w, r, logger, "comment_removed notification id generation failed", "error", err)
				return
			}
		}

		event, emitted, err := store.DeleteComment(r.Context(), db, kind.content, commentID, su.UserID, notificationID, now().UnixMilli())
		if err != nil {
			// A row deleted between the author lookup and the delete is the
			// documented benign TOCTOU — the same masked outcome.
			writeStoreError(w, r, err, kind.label+" comment delete", "error", err)
			return
		}

		if !isSelf {
			logAuditTargetEvent(r, audit.EventCommentDeleted, audit.ResultSuccess, su.UserID, authorID, "")
		}
		// The notification row committed in the same transaction — hand the
		// event to the push channel AFTER the commit, WITHOUT an actor: the
		// notice never names the moderator (the actorless ruling), so the
		// payload must not carry the username either.
		if emitted && notify != nil {
			notify.NotifyPush(r.Context(), push.Event{
				RecipientID:  event.RecipientID,
				Kind:         event.Kind,
				ContentTitle: event.ContentTitle,
				ContentKind:  event.ContentKind,
				ContentID:    event.ContentID,
				CommentID:    event.CommentID,
			})
		}
		writeNoContent(w, r)
	}
}

// CommentsHeart returns PUT/DELETE
// /api/v1/{segment}/{id}/comments/{commentId}/heart — the idempotent 204
// heart toggle (favorites pattern). The counter delta, the heart
// row, and the heart notification emission/removal commit in one
// transaction. PUT on the viewer's own
// comment is a 422 {commentId, invalidValue} — self-hearts are forbidden.
func CommentsHeart(kind commentKind, db *sql.DB, notify push.Notifier, now func() time.Time, on bool, limiter *middleware.RateLimiter) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" comment heart requires authentication")
		if !ok {
			return
		}
		contentID := r.PathValue("id")
		if !publicIDPattern.MatchString(contentID) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}
		commentID := r.PathValue("commentId")
		if !publicIDPattern.MatchString(commentID) {
			writeBadRequest(w, r, "invalid comment id")
			return
		}

		if !allowUserBucket(w, r, limiter, "comment-heart", su.UserID) {
			return
		}

		// The notification id is only ever consumed by the fresh-heart
		// insert — mint it on the PUT path (the reply precedent).
		var heartNotificationID string
		if on {
			var err error
			heartNotificationID, err = id.New()
			if err != nil {
				writeInternalError(w, r, logger, "heart notification id generation failed", "error", err)
				return
			}
		}

		if event, emitted, err := store.SetCommentHeart(r.Context(), db, kind.content, su.UserID, contentID, commentID, on, heartNotificationID, now().UnixMilli()); err != nil {
			writeStoreError(w, r, err, kind.label+" comment heart", "error", err)
			return
		} else if emitted && notify != nil {
			// The notification row committed in the same transaction — hand
			// the event to the push channel AFTER the commit. A repeat heart
			// emits nothing (the row existed), so
			// no duplicate push can follow an idempotent PUT.
			notify.NotifyPush(r.Context(), push.Event{
				RecipientID:   event.RecipientID,
				Kind:          event.Kind,
				ActorUsername: su.Username,
				ContentTitle:  event.ContentTitle,
				ContentKind:   event.ContentKind,
				ContentID:     event.ContentID,
				CommentID:     event.CommentID,
			})
		}
		writeNoContent(w, r)
	}
}

// allowUserBucket applies one user-keyed handler-level rate-limit bucket
// (the pwChangeLimiter precedent):
// the callers are all authenticated, so the key is the user id and there is
// no IP bucket. It writes the 429 + Retry-After and logs the exceedance
// through the writer, returning false. Limiter is required: a nil would
// silently disable an abuse control. Callers run it AFTER the cheap
// path/body validation so a malformed request never spends the budget.
func allowUserBucket(w http.ResponseWriter, r *http.Request, limiter *middleware.RateLimiter, bucket, userID string) bool {
	if limiter == nil {
		panic("handler: nil rate limiter for bucket " + bucket)
	}
	allowed, retryAfter := limiter.Allow(userID)
	if !allowed {
		writeTooManyRequests(w, r, retryAfter, "bucket", bucket, "userId", userID)
		return false
	}
	return true
}

// writeCreatedWithLocation writes the 201 baseline headers plus Location —
// the comment create/reply contract returns no body (Location is
// the deep-link resource).
func writeCreatedWithLocation(w http.ResponseWriter, r *http.Request, location string) {
	setAPIHeaders(w, r)
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusCreated)
}
