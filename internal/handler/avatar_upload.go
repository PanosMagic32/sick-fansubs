package handler

import (
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/id"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store"
)

// AvatarUpload is the handler for PUT /api/v1/users/me/avatar — the
// self-service avatar upload. ANY
// authenticated user may set their OWN avatar — the admin+ floor of
// POST /api/v1/media does not apply here.
//
// Unlike the content upload-first flow, the avatar is ONE step:
// the file is processed and the users.avatar_url reference is updated in
// the same request. The upload-first split exists to protect abandoned
// content FORMS from orphaned media; an avatar has no form to abandon, so
// the combined update cannot orphan anything (a failed update leaves only
// the processed file, which the orphan sweep reclaims — the accepted
// tradeoff, same as an upload that never gets referenced).
//
// The route rides the users subtree chain: RequestID → TrustedOrigin →
// Session → ForcePasswordChange → CSRF (the forced-change gate applies —
// the flagged account changes its password before touching the profile,
// consistent with every other protected route). No audit event — the event
// list has no media/avatar event (internal/handler/AGENTS.md).
//
// Processing is media.ProcessAvatar (200×200 square crop, PNG).
// The old avatar file is NOT deleted — the orphan sweep
// reclaims it after the grace window.
// auth_version is NOT bumped: an avatar change is not a security-state
// change, so sessions stay valid.
//
// Response: 200 with the refreshed UserProfile — the client updates its
// avatar rendering without a second round trip. Error mapping mirrors the
// media upload: missing file → 422 {file, required}; size or
// pixel bound → 413 {file, tooLarge}; undecodable → 422 {file,
// invalidFormat}.

// AvatarUpload returns the handler. publicBaseURL feeds the absolute
// avatarUrl in the response (the same value every profile-emitting
// handler receives); now is the injectable clock for the updated_at_ms
// stamp (nil → time.Now), so tests never depend on the wall clock.
func AvatarUpload(db *sql.DB, dataDir, publicBaseURL string, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "avatar upload requires authentication")
		if !ok {
			return
		}

		src, ok := readUploadFile(w, r)
		if !ok {
			return
		}
		defer src.Close()

		// Fresh random ID — uploads never reuse the migration's
		// content-derived IDs.
		uploadID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "avatar upload id generation failed", "error", err)
			return
		}

		rel, err := media.ProcessAvatar(dataDir, uploadID, src)
		if err != nil {
			writeProcessError(w, r, err)
			return
		}

		if err := store.UpdateUserAvatar(r.Context(), db, su.UserID, rel, now().UnixMilli()); err != nil {
			writeStoreAccountError(w, r, err, "avatar update", "error", err)
			return
		}

		// Re-read the row for the refreshed profile (two queries, short —
		// the profile projection needs email/role/createdAt the session
		// does not carry).
		u, err := store.UserByID(r.Context(), db, su.UserID)
		if err != nil {
			writeStoreAccountError(w, r, err, "avatar profile reload", "error", err)
			return
		}

		writeJSON(w, r, http.StatusOK, userProfileDTO(u, publicBaseURL))
	}
}
