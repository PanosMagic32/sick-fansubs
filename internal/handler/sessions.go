package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

// Self-service session list: GET
// /api/v1/users/me/sessions lists the caller's own unexpired sessions and
// DELETE /api/v1/users/me/sessions/{id} revokes one of them.
//
// The surface backs the account page's "Συσκευές" block. It reports only
// what the surface may expose: the opaque session id, the bounded
// client label (NULL when the classifier recognized nothing), the creation
// and expiry instants, and whether the row IS the requesting session. No IP
// address, no location, no last-seen instant — those columns do not exist.
//
// The route chain is RequestID → TrustedOrigin → Session → ForcePasswordChange
// → CSRF, the users subtree chain; the DELETE is CSRF-protected by the
// middleware and, like the favorites/follows deletions, carries no rate
// limiter.

// sessionItemDTO is one session row on the wire.
//
// ClientLabel is a POINTER so an unknown device is an explicit JSON null
// rather than an empty string — the client renders "Άγνωστη συσκευή" for
// null and shows the label verbatim otherwise.
type sessionItemDTO struct {
	ID          string  `json:"id"`
	ClientLabel *string `json:"clientLabel"`
	CreatedAt   string  `json:"createdAt"`
	ExpiresAt   string  `json:"expiresAt"`
	Current     bool    `json:"current"`
}

// SessionsList returns the handler for GET /api/v1/users/me/sessions.
//
// Ordering is created_at_ms DESC, id ASC — newest sign-in first. The window
// is expires_at_ms > now: an expired row is already absent here, whether or
// not the bounded cleanup task has run. now is injectable for tests (nil
// falls back to time.Now).
//
// Outcomes: 200 with the caller's own active sessions; 400 malformed/unknown/
// duplicate query or invalid cursor; 401 unauthenticated; 422 limit out of
// range. Another user's sessions are unreachable by construction — the store
// query filters on the authenticated user id, not on a client-supplied one.
func SessionsList(db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "session list requires authentication")
		if !ok {
			return
		}

		_, limit, after, ok := parseKeysetParams(w, r, 20, 100, sessionCursorPrefix)
		if !ok {
			return
		}

		writeKeysetPage(w, r, "session list", keysetPage[store.SessionListItem, sessionItemDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountSessions(ctx, db, su.UserID, now().UnixMilli())
			},
			read: func(ctx context.Context) ([]store.SessionListItem, bool, error) {
				var key *store.SessionPageKey
				if after != nil {
					// The shared codec's timestamp slot carries the session's
					// created_at_ms in this namespace.
					key = &store.SessionPageKey{CreatedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListSessions(ctx, db, su.UserID, now().UnixMilli(), limit, key)
			},
			item: func(it store.SessionListItem) sessionItemDTO {
				var label *string
				if it.ClientLabel != "" {
					label = &it.ClientLabel
				}
				return sessionItemDTO{
					ID:          it.ID,
					ClientLabel: label,
					CreatedAt:   formatAPITime(it.CreatedAtMS),
					ExpiresAt:   formatAPITime(it.ExpiresAtMS),
					// The request's own session id is the only source of truth
					// for "this device" — never a client-supplied flag.
					Current: it.ID == su.SessionID,
				}
			},
			next: func(last store.SessionListItem) string {
				return encodeCursor(sessionCursorPrefix, last.CreatedAtMS, last.ID)
			},
		})
	}
}

// SessionRevoke returns the handler for
// DELETE /api/v1/users/me/sessions/{id}.
//
// Outcomes: 204 idempotent success; 400 invalid id shape; 401
// unauthenticated; 403 origin/CSRF failure (middleware).
//
// Idempotence is the contract: the outcome is identical
// whether the id names the caller's own session, another user's session, or
// nothing at all. The store scopes the delete by owner, so a foreign id
// simply removes no row.
//
// Revoking the CURRENT session behaves like sign-out — the row is deleted and
// the cookie is cleared, after the delete commits (never clear a
// credential whose server-side row still exists). The UI does not offer the
// action on the current row; this path exists so the endpoint has no special
// cases.
//
// Auditing follows the sign-out precedent: a success row only when a row was
// actually deleted, so a replayed request cannot manufacture audit noise, and
// a failure row when the delete was attempted and failed.
func SessionRevoke(db *sql.DB, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "session revoke requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid session id")
			return
		}

		deleted, err := store.RevokeSession(r.Context(), db, su.UserID, id)
		if err != nil {
			logAuditEvent(r, audit.EventSessionRevoked, audit.ResultFailure, su.UserID)
			writeInternalError(w, r, logger, "session revoke failed", "error", err)
			return
		}

		// A no-op delete (unknown, foreign, or already-revoked id) changed
		// nothing server-side, so it writes no audit row — the sign-out
		// rationale (auth.go: a replayed stale cookie must not manufacture
		// audit noise).
		if deleted > 0 {
			logAuditEvent(r, audit.EventSessionRevoked, audit.ResultSuccess, su.UserID)
		}
		if id == su.SessionID {
			middleware.ClearSessionCookie(w, secure)
		}

		writeNoContent(w, r)
	}
}
