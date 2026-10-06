package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/store"
)

// userStatusChangeKind parameterizes the suspend/reactivate pair: the two
// endpoints share the chain, the target load, the peer check, the same-status
// no-op, and the audit trail — only the target status, the store write, and
// the audit event differ (the favorites-bundle precedent).
//
// write returns store.ErrNotFound when the target vanished, and only the
// suspend write can return store.ErrLastActiveSuperAdmin — the shared error
// mapping below handles both for either kind (reactivation never trips the
// guard, but one mapping is clearer than a per-kind switch).
type userStatusChangeKind struct {
	targetStatus string
	event        string
	write        func(ctx context.Context, db *sql.DB, userID string, updatedAtMS int64) error
}

// UserSuspend returns the handler for POST /api/v1/users/{id}/suspend.
func UserSuspend(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return userStatusChangeHandler(db, publicBaseURL, userStatusChangeKind{
		targetStatus: identity.StatusSuspended,
		event:        audit.EventUserSuspended,
		write:        store.SuspendUser,
	})
}

// UserReactivate returns the handler for POST /api/v1/users/{id}/reactivate.
func UserReactivate(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return userStatusChangeHandler(db, publicBaseURL, userStatusChangeKind{
		targetStatus: identity.StatusActive,
		event:        audit.EventUserReactivated,
		write:        store.ReactivateUser,
	})
}

// userStatusChangeHandler implements the suspend/reactivate flow: a
// peer-protected status change that revokes the target's
// sessions (suspension) or forces a fresh sign-in (reactivation).
//
// Floor + peer protection: super-admins manage anyone including
// themselves; admins manage moderators and users; moderators manage users
// only. The last-active-super-admin guard applies to suspension —
// self-suspension included.
//
// Outcomes:
//   - 401 generic problem for an unauthenticated request.
//   - 400 for a structurally invalid id (publicIDPattern, 400 before lookup).
//   - 404 for an id that resolves to no user — NOT audited.
//   - 403 for a floor/peer-protection violation — AUDITED as the event's
//     failure with actor + target + the target's role snapshot.
//   - 409 for the last-active-super-admin guard (suspend only) — AUDITED
//     the same way, problem type /problems/auth/last-super-admin.
//   - 200 with the updated StaffUserItem projection on success, audited as
//     success (targetRole unchanged by a status change).
//   - A same-status POST is a 200 no-op: no version bump, no audit. The
//     no-op check sits AFTER authorization — an unauthorized same-status
//     attempt still answers the audited 403.
//   - 500 generic problem if the write fails (audited as failure).
func userStatusChangeHandler(db *sql.DB, publicBaseURL string, kind userStatusChangeKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "user status change requires authentication")
		if !ok {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid user id")
			return
		}

		// Load the target BEFORE the authorization check so every audited
		// 403/409 carries the complete record — actor, target, and the
		// target's current role snapshot.
		target, err := store.UserByID(r.Context(), db, id)
		if err != nil {
			writeStoreError(w, r, err, "status change target lookup", "error", err)
			return
		}

		if !identity.CanChangeUserStatus(su.Role, target.Role) {
			auditTargetFailure(r, kind.event, su, target)
			writeForbidden(w, r, "user status change not permitted for this target")
			return
		}

		// Same-status no-op: 200 with the current projection, no version
		// bump, no audit — a no-op is not a state change. The no-op check
		// sits AFTER authorization: an unauthorized same-status attempt
		// still answers the audited 403. For reactivation this also protects
		// live sessions: a bump on an already-active account would revoke
		// them for nothing.
		if target.Status == kind.targetStatus {
			writeJSON(w, r, http.StatusOK, staffUserProjection(publicBaseURL, staffUserFieldsFromUser(target), identity.CanViewStaffEmails(su.Role)))
			return
		}

		if err := kind.write(r.Context(), db, target.ID, time.Now().UnixMilli()); err != nil {
			switch writeStoreError(w, r, err, "user status change", "error", err) {
			case http.StatusConflict, http.StatusInternalServerError:
				auditTargetFailure(r, kind.event, su, target)
			}
			return
		}

		target.Status = kind.targetStatus
		logAuditTargetEvent(r, kind.event, audit.ResultSuccess, su.UserID, target.ID, target.Role)
		writeJSON(w, r, http.StatusOK, staffUserProjection(publicBaseURL, staffUserFieldsFromUser(target), identity.CanViewStaffEmails(su.Role)))
	}
}
