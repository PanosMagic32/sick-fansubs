package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/id"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store"
)

// BlogCreate returns the handler for POST /api/v1/blog-posts.
//
// Outcomes: 201 + Location + staff-shaped body; 400 malformed body;
// 401 unauthenticated; 403 non-admin role / origin / CSRF (middleware);
// 413/415/422 per the write contract; 500.
func BlogCreate(db *sql.DB, dataDir string, publicBaseURL string, notify push.Notifier, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, _, ok := staffRequester(w, r)
		if !ok {
			return
		}
		if !identity.CanCreateContent(su.Role) {
			writeForbidden(w, r, "content create requires admin role")
			return
		}

		req, ok := parseBlogPostWrite(w, r, "draft", "published")
		if !ok {
			return
		}
		if !validThumbnailReference(dataDir, req.ThumbnailPath) {
			writeValidationErrors(w, r, []Violation{{Field: "thumbnailPath", Code: "invalidFormat"}})
			return
		}

		nowMS := now().UnixMilli()
		var publishedMS int64
		if *req.Status == "published" {
			publishedMS = nowMS
		}

		postID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "content id generation failed", "error", err)
			return
		}

		post, events, err := store.CreateBlogPost(r.Context(), db, store.CreateBlogPostParams{
			ID:            postID,
			Title:         req.Title,
			Subtitle:      *req.Subtitle,
			Description:   *req.Description,
			ThumbnailURL:  req.ThumbnailPath,
			Status:        *req.Status,
			CreatorID:     su.UserID,
			UpdaterID:     su.UserID,
			PublishedAtMS: publishedMS,
			NowMS:         nowMS,
			Downloads:     req.DownloadsParsed,
		})
		if err != nil {
			// A create owns no absence: ErrNotFound here is the post-commit
			// read-back losing a race with a concurrent delete — an internal
			// failure, never the masked 404 the sentinel maps to elsewhere.
			if errors.Is(err, store.ErrNotFound) {
				writeInternalError(w, r, logger, "blog create failed", "error", err)
				logAuditEvent(r, audit.EventContentCreated, audit.ResultFailure, su.UserID)
				return
			}
			if writeStoreError(w, r, err, "blog create", "error", err) == http.StatusInternalServerError {
				logAuditEvent(r, audit.EventContentCreated, audit.ResultFailure, su.UserID)
			}
			return
		}

		// new_content fan-out: the
		// broadcast rows committed in the create transaction — hand each
		// recipient's event to the push channel AFTER the commit (the
		// heart/reply precedent). A DRAFT create carries the draft_activity
		// notice instead, through the same loop.
		if notify != nil {
			for _, ev := range events {
				notify.NotifyPush(r.Context(), push.Event{
					RecipientID:   ev.RecipientID,
					Kind:          ev.Kind,
					ActorUsername: su.Username,
					ContentTitle:  ev.ContentTitle,
					ContentKind:   ev.ContentKind,
					ContentID:     ev.ContentID,
					DraftAction:   ev.DraftAction,
				})
			}
		}

		logAuditEvent(r, audit.EventContentCreated, audit.ResultSuccess, su.UserID)
		// The created resource's canonical address is the STAFF detail: the
		// public detail masks drafts (the published filter), so only the
		// staff path is live for every status.
		w.Header().Set("Location", "/api/v1/admin/blog-posts/"+post.ID)
		writeJSON(w, r, http.StatusCreated, staffDTO(publicBaseURL, post))
	}
}

// BlogUpdate returns the handler for PUT /api/v1/blog-posts/{id}.
//
// Outcomes: 200 staff-shaped body + fresh ETag; 400 malformed body/If-Match
// or invalid id; 401; 403 role/origin/CSRF; 404 masked (unknown OR
// draft-invisible); 412 stale If-Match; 422 field violations; 428 missing
// If-Match; 500.
func BlogUpdate(db *sql.DB, dataDir string, publicBaseURL string, notify push.Notifier, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		su, weight, ok := staffRequester(w, r)
		if !ok {
			return
		}
		if !identity.CanModerateContent(su.Role) {
			writeForbidden(w, r, "content update requires moderator role")
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid blog post id")
			return
		}

		// The draft-visibility gate: a pre-read through the SAME visibility
		// filter as the staff detail — see staffReadGate.
		if !staffReadGate(w, r, db, id, su, weight, "blog update") {
			return
		}

		revision, ok := parseIfMatch(w, r)
		if !ok {
			return
		}

		req, ok := parseBlogPostWrite(w, r, "draft", "published", "archived")
		if !ok {
			return
		}
		if !validThumbnailReference(dataDir, req.ThumbnailPath) {
			writeValidationErrors(w, r, []Violation{{Field: "thumbnailPath", Code: "invalidFormat"}})
			return
		}

		post, events, err := store.UpdateBlogPost(r.Context(), db, id, revision, store.UpdateBlogPostParams{
			Title:        req.Title,
			Subtitle:     *req.Subtitle,
			Description:  *req.Description,
			ThumbnailURL: req.ThumbnailPath,
			Status:       *req.Status,
			UpdaterID:    su.UserID,
			NowMS:        now().UnixMilli(),
			Downloads:    req.DownloadsParsed,
		})
		if err != nil {
			if writeStoreError(w, r, err, "blog update", "error", err) == http.StatusInternalServerError {
				logAuditEvent(r, audit.EventContentUpdated, audit.ResultFailure, su.UserID)
			}
			return
		}

		// Fan-out: the notification rows committed in the
		// update transaction — hand each follower's content_updated event,
		// each opted-in recipient's new_content event, or each visible staff
		// member's draft_activity notice to the push channel AFTER the commit
		// (the heart/reply precedent; a silent transition emitted no rows, so
		// no duplicate push can follow).
		if notify != nil {
			for _, ev := range events {
				notify.NotifyPush(r.Context(), push.Event{
					RecipientID:   ev.RecipientID,
					Kind:          ev.Kind,
					ActorUsername: su.Username,
					ContentTitle:  ev.ContentTitle,
					ContentKind:   ev.ContentKind,
					ContentID:     ev.ContentID,
					DraftAction:   ev.DraftAction,
				})
			}
		}

		logAuditEvent(r, audit.EventContentUpdated, audit.ResultSuccess, su.UserID)
		w.Header().Set("ETag", etagForRevision(post.Revision))
		writeJSON(w, r, http.StatusOK, staffDTO(publicBaseURL, post))
	}
}

// BlogDelete returns the handler for DELETE /api/v1/blog-posts/{id}.
//
// Outcomes: 204; 400 malformed If-Match or invalid id; 401; 403
// role/origin/CSRF (the delete floor is admin+); 404 masked; 412 stale
// If-Match; 428 missing If-Match;
// 500. The cascade (downloads, favorites, FTS) runs in the store
// transaction.
func BlogDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, weight, ok := staffRequester(w, r)
		if !ok {
			return
		}
		if !identity.CanDeleteContent(su.Role) {
			writeForbidden(w, r, "content delete requires admin role")
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid blog post id")
			return
		}

		if !staffReadGate(w, r, db, id, su, weight, "blog delete") {
			return
		}

		revision, ok := parseIfMatch(w, r)
		if !ok {
			return
		}

		err := store.DeleteBlogPost(r.Context(), db, id, revision)
		if err != nil {
			if writeStoreError(w, r, err, "blog delete", "error", err) == http.StatusInternalServerError {
				logAuditEvent(r, audit.EventContentDeleted, audit.ResultFailure, su.UserID)
			}
			return
		}

		logAuditEvent(r, audit.EventContentDeleted, audit.ResultSuccess, su.UserID)
		writeNoContent(w, r)
	}
}
