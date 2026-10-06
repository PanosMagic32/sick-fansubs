package handler

import (
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Follows handlers: the
// per-content follow toggle under /api/v1/users/me/follows/{segment}/{id}
// — the favorites mirror: a follow drives the 'comment' and
// 'content_updated' notification kinds.
//
// The users subtree chain applies (RequestID → TrustedOrigin → Session →
// ForcePasswordChange → CSRF, no rate limits — authenticated per-user
// operations protected by origin + CSRF, the favorites precedent).

// followKind binds the two content types behind one handler set (the
// favoriteKind precedent).
type followKind struct {
	label string // log-only discriminator: "blog" | "project"

	content store.ContentKind
}

// BlogFollowsKind and ProjectFollowsKind bind the blog-post and project
// follow storage behind the shared follow handlers.
var (
	BlogFollowsKind = followKind{
		label:   "blog",
		content: store.BlogContent,
	}
	ProjectFollowsKind = followKind{
		label:   "project",
		content: store.ProjectContent,
	}
)

// followStatusDTO is the status read body — the favorites mirror.
type followStatusDTO struct {
	Following bool `json:"following"`
}

// FollowsStatus returns the handler for
// GET /api/v1/users/me/follows/{segment}/{id}.
//
// Outcomes: 200 {"following": bool}; 400 invalid id shape; 401
// unauthenticated; 404 masked for unknown/non-published content (the
// favorites status contract).
func FollowsStatus(kind followKind, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" follow status requires authentication")
		if !ok {
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}

		published, following, err := store.FollowStatus(r.Context(), db, kind.content, su.UserID, id)
		if err != nil {
			writeInternalError(w, r, logger, kind.label+" follow status failed", "error", err)
			return
		}
		if !published {
			// Masked absence — indistinguishable from the content detail.
			NotFound(w, r)
			return
		}

		writeJSON(w, r, http.StatusOK, followStatusDTO{Following: following})
	}
}

// FollowsAdd returns the handler for
// PUT /api/v1/users/me/follows/{segment}/{id}.
//
// Outcomes: 204 idempotent success; 400 invalid id shape; 404 masked for
// unknown/non-published content; 401 unauthenticated; 403 origin/CSRF
// failure (middleware).
func FollowsAdd(kind followKind, db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, kind.label+" follow add requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}

		if err := store.AddFollow(r.Context(), db, kind.content, su.UserID, id, now().UnixMilli()); err != nil {
			writeStoreError(w, r, err, kind.label+" follow add", "error", err)
			return
		}

		writeNoContent(w, r)
	}
}

// FollowsRemove returns the handler for
// DELETE /api/v1/users/me/follows/{segment}/{id}.
//
// Outcomes: 204 idempotent success (removing a non-follow also succeeds);
// 400 invalid id shape; 401 unauthenticated; 403 origin/CSRF failure
// (middleware).
func FollowsRemove(kind followKind, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, kind.label+" follow remove requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" id")
			return
		}

		if err := store.RemoveFollow(r.Context(), db, kind.content, su.UserID, id); err != nil {
			writeInternalError(w, r, logger, kind.label+" follow remove failed", "error", err)
			return
		}

		writeNoContent(w, r)
	}
}
