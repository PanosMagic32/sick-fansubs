package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/id"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/slug"
	"sick-fansubs/internal/store"
)

// ProjectCreate returns the handler for POST /api/v1/projects.
//
// Outcomes: 201 + Location + staff-shaped body; 400 malformed body (an
// unknown `slug` key included); 401 unauthenticated; 403 non-admin role /
// origin / CSRF (middleware); 413/415/422 per the write contract; 500.
//
// The slug is generated from the title: the transliterated base,
// then -2, -3, … on a slug collision; an empty transliteration falls back
// to "p-<id>" so create never fails on slug generation alone.
func ProjectCreate(db *sql.DB, dataDir string, publicBaseURL string, notify push.Notifier, now func() time.Time) http.HandlerFunc {
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

		req, ok := parseProjectCreate(w, r)
		if !ok {
			return
		}
		if !validThumbnailReference(dataDir, req.ThumbnailPath) {
			writeValidationErrors(w, r, []Violation{{Field: "thumbnailPath", Code: "invalidFormat"}})
			return
		}

		nowMS := now().UnixMilli()
		var publishedMS int64
		if req.Status == "published" {
			publishedMS = nowMS
		}

		projectID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "content id generation failed", "error", err)
			return
		}

		base := slug.GenerateBase(req.Title)
		if base == "" {
			base = "p-" + projectID
		}

		var project *store.StaffProject
		var events []store.NotificationEvent
		for attempt := range maxSlugAttempts {
			candidate := base
			if attempt > 0 {
				candidate = fmt.Sprintf("%s-%d", base, attempt+1)
			}
			project, events, err = store.CreateProject(r.Context(), db, store.CreateProjectParams{
				ID:            projectID,
				Title:         req.Title,
				Description:   req.Description,
				Slug:          candidate,
				ThumbnailURL:  req.ThumbnailPath,
				Status:        req.Status,
				CreatorID:     su.UserID,
				UpdaterID:     su.UserID,
				PublishedAtMS: publishedMS,
				NowMS:         nowMS,
				Downloads:     req.DownloadsParsed,
			})
			if err == nil {
				break
			}
			if !errors.Is(err, store.ErrSlugTaken) {
				// An absence is internal here too: the committed read-back
				// raced a delete (the BlogCreate contract).
				if errors.Is(err, store.ErrNotFound) {
					writeInternalError(w, r, logger, "project create failed", "error", err)
					logAuditEvent(r, audit.EventContentCreated, audit.ResultFailure, su.UserID)
					return
				}
				if writeStoreError(w, r, err, "project create", "error", err) == http.StatusInternalServerError {
					logAuditEvent(r, audit.EventContentCreated, audit.ResultFailure, su.UserID)
				}
				return
			}
		}
		if project == nil {
			// All maxSlugAttempts candidates collided.
			writeInternalError(w, r, logger, "project create failed", "reason", "slug attempts exhausted", "base", base)
			logAuditEvent(r, audit.EventContentCreated, audit.ResultFailure, su.UserID)
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
		w.Header().Set("Location", "/api/v1/admin/projects/"+project.ID)
		writeJSON(w, r, http.StatusCreated, staffProjectDTOFrom(publicBaseURL, project))
	}
}

// ProjectUpdate returns the handler for PUT /api/v1/projects/{id}.
//
// Outcomes: 200 staff-shaped body + fresh ETag; 400 malformed body/If-Match
// or invalid id; 401; 403 role/origin/CSRF, or a moderator-submitted slug;
// 404 masked (unknown OR draft-invisible); 412 stale If-Match; 422 field
// violations (invalid slug grammar, duplicate slug); 428 missing If-Match;
// 500.
func ProjectUpdate(db *sql.DB, dataDir string, publicBaseURL string, notify push.Notifier, now func() time.Time) http.HandlerFunc {
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
			writeBadRequest(w, r, "invalid project id")
			return
		}

		// The draft-visibility gate — and the current row (the slug fallback when
		// the update body omits it).
		cur, ok := projectStaffReadGate(w, r, db, id, su, weight, "project update")
		if !ok {
			return
		}

		revision, ok := parseIfMatch(w, r)
		if !ok {
			return
		}

		req, slugField, ok := parseProjectUpdate(w, r)
		if !ok {
			return
		}
		if !validThumbnailReference(dataDir, req.ThumbnailPath) {
			writeValidationErrors(w, r, []Violation{{Field: "thumbnailPath", Code: "invalidFormat"}})
			return
		}

		newSlug := cur.Slug // absent keeps the existing slug
		if slugField != nil {
			// A present slug field is admin+-only: a moderator must not
			// request a capability they lack (see the handler AGENTS note on
			// the shared admin floor).
			if !identity.CanCreateContent(su.Role) {
				writeForbidden(w, r, "slug editing requires admin role")
				return
			}
			if !slug.Valid(*slugField) {
				writeValidationErrors(w, r, []Violation{{Field: "slug", Code: "invalidFormat"}})
				return
			}
			newSlug = *slugField
		}

		project, events, err := store.UpdateProject(r.Context(), db, id, revision, store.UpdateProjectParams{
			Title:        req.Title,
			Description:  req.Description,
			Slug:         newSlug,
			ThumbnailURL: req.ThumbnailPath,
			Status:       req.Status,
			UpdaterID:    su.UserID,
			NowMS:        now().UnixMilli(),
			Downloads:    req.DownloadsParsed,
		})
		if err != nil {
			if writeStoreError(w, r, err, "project update", "error", err) == http.StatusInternalServerError {
				logAuditEvent(r, audit.EventContentUpdated, audit.ResultFailure, su.UserID)
			}
			return
		}

		// Fan-out — the same after-commit seam as BlogUpdate:
		// the notification rows committed in the update transaction; each
		// follower's content_updated event, each opted-in recipient's
		// new_content event, or each visible staff member's draft_activity
		// notice goes to the push channel; silent transitions emitted no
		// rows.
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
		w.Header().Set("ETag", etagForRevision(project.Revision))
		writeJSON(w, r, http.StatusOK, staffProjectDTOFrom(publicBaseURL, project))
	}
}

// ProjectDelete returns the handler for DELETE /api/v1/projects/{id}.
//
// Outcomes: 204; 400 malformed If-Match or invalid id; 401; 403
// role/origin/CSRF (the delete floor is admin+); 404 masked; 412 stale
// If-Match; 428 missing If-Match;
// 500. The cascade (downloads, favorites, FTS) runs in the store
// transaction.
func ProjectDelete(db *sql.DB) http.HandlerFunc {
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
			writeBadRequest(w, r, "invalid project id")
			return
		}

		if _, ok := projectStaffReadGate(w, r, db, id, su, weight, "project delete"); !ok {
			return
		}

		revision, ok := parseIfMatch(w, r)
		if !ok {
			return
		}

		err := store.DeleteProject(r.Context(), db, id, revision)
		if err != nil {
			if writeStoreError(w, r, err, "project delete", "error", err) == http.StatusInternalServerError {
				logAuditEvent(r, audit.EventContentDeleted, audit.ResultFailure, su.UserID)
			}
			return
		}

		logAuditEvent(r, audit.EventContentDeleted, audit.ResultSuccess, su.UserID)
		writeNoContent(w, r)
	}
}
