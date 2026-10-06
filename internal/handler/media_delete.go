package handler

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store"
)

// MediaDelete is the handler for DELETE /api/v1/media/{file}: the
// explicit-deletion terminal state of the upload lifecycle.
// An admin deletes one uploaded image — the served segment `<32hex>.<ext>`
// from the upload response's `url`, so the client never hand-builds a
// storage path. The route chain is RequestID → TrustedOrigin → Session →
// ForcePasswordChange → CSRF (the upload's chain; the X-CSRF-Token header
// keeps the body untouched — a DELETE carries no body).
//
// Outcomes:
//
//	malformed segment → 400 bad-request (the asset path answers plain text;
//	                     this is the API surface — a JSON problem)
//	well-formed, file missing → 404 not-found
//	referenced by any content row or avatar → 409 conflict — never removed
//	unreferenced → 204, file removed, Info log with the actor
//
// The check-then-remove window between the reference check and os.Remove is
// benign and documented (internal/handler/AGENTS.md): a concurrent create
// could reference the file in that window — the same human flow that would
// then fail its existence check. Accepted, not worth a lock. No audit event
// — the audited-event list has no media event; deletion is logged, not
// audited.
func MediaDelete(dataDir string, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "media delete requires authentication")
		if !ok {
			return
		}
		if !identity.CanDeleteMedia(su.Role) {
			writeForbidden(w, r, "media delete requires admin role")
			return
		}

		id, ext, ok := parseMediaFile(r.PathValue("file"))
		if !ok {
			writeBadRequest(w, r, "malformed media file segment")
			return
		}
		rel := media.RelativePath(id, ext)

		// Existence before the reference check: a missing file is a 404
		// regardless of what rows point at it.
		full := filepath.Join(dataDir, rel)
		if _, err := os.Stat(full); err != nil {
			if os.IsNotExist(err) {
				writeProblem(w, r, http.StatusNotFound,
					"/problems/not-found", "Not found")
				return
			}
			writeInternalError(w, r, logger, "media stat failed", "error", unwrappedCause(err))
			return
		}

		referenced, err := store.MediaPathReferenced(r.Context(), db, rel)
		if err != nil {
			writeInternalError(w, r, logger, "media reference check failed", "error", err)
			return
		}
		if referenced {
			writeConflict(w, r, "media file is referenced by content or a user avatar")
			return
		}

		if err := os.Remove(full); err != nil {
			if os.IsNotExist(err) {
				// Swept between the stat and the remove — the same outcome
				// the request wanted.
				writeProblem(w, r, http.StatusNotFound,
					"/problems/not-found", "Not found")
				return
			}
			writeInternalError(w, r, logger, "media remove failed", "error", unwrappedCause(err))
			return
		}
		logger.InfoContext(r.Context(), "media deleted",
			"file", id, "userId", su.UserID)
		writeNoContent(w, r)
	}
}
