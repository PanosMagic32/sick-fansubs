package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/store"
)

// StaffUserItem is the wire projection of one account in the staff list.
//
// Email is a pointer with omitempty: it is exposed only to
// admin+ viewers, so a moderator's response carries no email key at all
// (a null key would still disclose "an email exists here" per account,
// and the contract says the field is not part of the moderator projection).
//
// AvatarURL follows the UserProfile shape: a pointer with NO omitempty, so the key is
// always present and null when the account has no avatar — this is account
// state the admin members UI renders, not an admin-only field, so it has no
// role gate.
type StaffUserItem struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	Email         *string `json:"email,omitempty"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	EmailVerified bool    `json:"emailVerified"`
	CreatedAt     string  `json:"createdAt"`
	AvatarURL     *string `json:"avatarUrl"`
}

// resetPasswordResponse is the one-time plaintext temp password. The
// default Cache-Control: no-store applies — the plaintext
// exists only in this response and the operator's out-of-band handover.
type resetPasswordResponse struct {
	Password string `json:"password"`
}

// auditTargetFailure emits the audited FAILURE row shared by the staff
// account operations: actor + target + the target's CURRENT role
// snapshot — the visibility rule's input. Blocked 403s, guard 409s,
// and failed writes all call it.
func auditTargetFailure(r *http.Request, event string, su *identity.SessionUser, target *identity.User) {
	logAuditTargetEvent(r, event, audit.ResultFailure, su.UserID, target.ID, target.Role)
}

// UserPasswordReset returns the handler for POST /api/v1/users/{id}/reset-password:
// the admin manual reset. The server generates
// a 16-character temp password, stores a fresh verifier, sets the
// forced-change flag, bumps auth_version, and deletes the target's sessions
// in one transaction; the plaintext returns ONCE.
//
// Outcomes:
//   - 401 generic problem for an unauthenticated request.
//   - 400 for a structurally invalid id (publicIDPattern, 400 before lookup).
//   - 404 for an id that resolves to no user — NOT audited (the masked
//     outcome carries no security-relevant attempt).
//   - 403 for a floor/peer-protection violation (below admin+, or an admin
//     targeting an admin/super-admin) — AUDITED as a password_reset failure
//     with actor + target + target_role, so higher roles see the blocked
//     attempt in the feed.
//   - 200 { "password": "<temp>" } on success, audited as success.
//   - 500 generic problem if the write fails (audited as failure — the
//     sensitive part of the operation was attempted).
func UserPasswordReset(db *sql.DB, auth *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "password reset requires authentication")
		if !ok {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid user id")
			return
		}

		// Load the target BEFORE the authorization check so every audited
		// 403 carries the complete record — actor, target, and the target's
		// role snapshot that the feed's visibility rule needs. A nonexistent
		// target is the plain masked 404, never audited.
		target, err := store.UserByID(r.Context(), db, id)
		if err != nil {
			writeStoreError(w, r, err, "password reset target lookup", "error", err)
			return
		}

		// Floor + peer protection. The blocked attempt is
		// exactly what higher roles should notice, so it is audited — with
		// the full target snapshot.
		if !identity.CanResetPassword(su.Role, target.Role) {
			auditTargetFailure(r, audit.EventPasswordReset, su, target)
			writeForbidden(w, r, "password reset not permitted for this target")
			return
		}

		temp, err := auth.ResetPassword(r.Context(), target.ID)
		if err != nil {
			// A target that vanished between the authorization load and the
			// write shares the masked 404; anything else is an audited failure.
			if writeStoreError(w, r, err, "password reset", "error", err) == http.StatusInternalServerError {
				auditTargetFailure(r, audit.EventPasswordReset, su, target)
			}
			return
		}

		logAuditTargetEvent(r, audit.EventPasswordReset, audit.ResultSuccess, su.UserID, target.ID, target.Role)
		writeJSON(w, r, http.StatusOK, resetPasswordResponse{Password: temp})
	}
}

// roleChangeRequest is the PATCH /api/v1/users/{id}/role body. The value is
// validated against the accepted role set before use.
type roleChangeRequest struct {
	Role string `json:"role"`
}

// staffUserFields is the input of staffUserProjection: the stored account
// fields the wire shape carries, whatever row the caller holds (the list's
// store.StaffUser, the write handlers' identity.User). It exists so the shared
// projection takes ONE value instead of ten positional arguments — username
// and email, role and status, and the two booleans would otherwise swap
// silently at a call site.
type staffUserFields struct {
	ID            string
	Username      string
	Email         string
	AvatarURL     *string
	Role          string
	Status        string
	EmailVerified bool
	CreatedAtMS   int64
}

// staffUserFieldsFromRow adapts a staff list row to the one projection input.
func staffUserFieldsFromRow(u store.StaffUser) staffUserFields {
	return staffUserFields{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		AvatarURL:     u.AvatarURL,
		Role:          u.Role,
		Status:        u.Status,
		EmailVerified: u.EmailVerified,
		CreatedAtMS:   u.CreatedAtMS,
	}
}

// staffUserFieldsFromUser adapts a full user record to the one projection input.
func staffUserFieldsFromUser(u *identity.User) staffUserFields {
	return staffUserFields{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		AvatarURL:     u.AvatarURL,
		Role:          u.Role,
		Status:        u.Status,
		EmailVerified: u.EmailVerifiedAtMS != nil,
		CreatedAtMS:   u.CreatedAtMS,
	}
}

// staffUserProjection builds the wire projection of one account:
// the StaffUserItem shape the staff list and the staff write responses share,
// with email included only when the viewer may see it — a moderator never
// receives the field at all. Every caller hands the stored fields through
// staffUserFields, so this stays the single mapping to the wire and the
// shapes cannot drift.
//
// AvatarURL is the stored reference; it is projected through avatarURLOrNull
// so the wire carries the ABSOLUTE served URL (the 2-hex images
// subdirectory never appears) or null when the account has no avatar, or
// when the stored value fails the scheme guard. Unlike email it is always
// present — no second role gate.
func staffUserProjection(publicBaseURL string, u staffUserFields, includeEmail bool) StaffUserItem {
	item := StaffUserItem{
		ID:            u.ID,
		Username:      u.Username,
		Role:          u.Role,
		Status:        u.Status,
		EmailVerified: u.EmailVerified,
		CreatedAt:     formatAPITime(u.CreatedAtMS),
		AvatarURL:     avatarURLOrNull(publicBaseURL, u.AvatarURL),
	}
	if includeEmail {
		item.Email = &u.Email
	}
	return item
}

// UserRoleChange returns the handler for PATCH /api/v1/users/{id}/role:
// a peer-protected role change that revokes
// the target's sessions. The complete transition policy:
//
//	super-admin: any target (incl. self) → any of the four roles
//	admin: moderator/user targets → moderator ↔ user only
//
// Outcomes:
//   - 401 generic problem for an unauthenticated request.
//   - 400 for a structurally invalid id or a malformed body (readJSON).
//   - 422 {role, invalidValue} for a role outside the accepted set.
//   - 404 for an id that resolves to no user — NOT audited.
//   - 403 for a floor/peer-protection violation — AUDITED as a role_changed
//     failure with actor + target + the target's CURRENT role snapshot.
//   - 409 for the last-active-super-admin guard — AUDITED
//     the same way (a blocked demotion is security-relevant).
//   - 200 with the updated StaffUserItem projection on success, audited as
//     role_changed success with targetRole = the NEW role.
//   - A same-role PATCH is a 200 no-op: no version bump, no audit. The
//     no-op check sits AFTER authorization, so
//     an unauthorized same-role attempt still answers the audited 403 —
//     authorization precedes even no-ops.
//   - 500 generic problem if the write fails (audited as failure).
func UserRoleChange(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "role change requires authentication")
		if !ok {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid user id")
			return
		}

		var req roleChangeRequest
		if err := readJSON(w, r, &req); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}
		if !identity.IsValidRole(req.Role) {
			writeValidationErrors(w, r, []Violation{{Field: "role", Code: "invalidValue"}})
			return
		}

		// Load the target BEFORE the authorization check so every audited
		// 403/409 carries the complete record — actor, target, and the
		// target's current role snapshot.
		target, err := store.UserByID(r.Context(), db, id)
		if err != nil {
			writeStoreError(w, r, err, "role change target lookup", "error", err)
			return
		}

		if !identity.CanChangeRole(su.Role, target.Role, req.Role) {
			auditTargetFailure(r, audit.EventRoleChanged, su, target)
			writeForbidden(w, r, "role change not permitted for this target")
			return
		}

		// Same-role no-op: 200 with the current projection, no version bump,
		// no audit — a no-op is not a state change. Authorization was checked
		// first: an actor who cannot manage the target never gets here.
		if req.Role == target.Role {
			writeJSON(w, r, http.StatusOK, staffUserProjection(publicBaseURL, staffUserFieldsFromUser(target), identity.CanViewStaffEmails(su.Role)))
			return
		}

		if err := store.ChangeUserRole(r.Context(), db, target.ID, req.Role, time.Now().UnixMilli()); err != nil {
			// The guard 409 and the unmapped 500 are security-relevant; the
			// masked 404 is not audited (the lookup precedent).
			switch writeStoreError(w, r, err, "role change", "error", err) {
			case http.StatusConflict, http.StatusInternalServerError:
				auditTargetFailure(r, audit.EventRoleChanged, su, target)
			}
			return
		}

		target.Role = req.Role
		logAuditTargetEvent(r, audit.EventRoleChanged, audit.ResultSuccess, su.UserID, target.ID, req.Role)
		writeJSON(w, r, http.StatusOK, staffUserProjection(publicBaseURL, staffUserFieldsFromUser(target), identity.CanViewStaffEmails(su.Role)))
	}
}

// UserDelete returns the handler for DELETE /api/v1/users/{id}: a
// peer-protected hard delete. The schema's foreign keys cascade every row the
// account owns — favorites, sessions, its comments and their replies, follows,
// notification rows and read state, reset/verification tokens, push
// subscriptions and preferences — and SET NULL the content creators/updaters
// plus the account's actor reference on other recipients' notification rows,
// so authored blog/project rows survive with the relationship broken; the
// account's comments do not. The confirmation step is frontend-only.
//
// Outcomes:
//   - 401 generic problem for an unauthenticated request.
//   - 400 for a structurally invalid id.
//   - 404 for an id that resolves to no user — NOT audited.
//   - 403 for a floor/peer-protection violation — AUDITED as a user_deleted
//     failure with actor + target + the target's role snapshot.
//   - 409 for the last-active-super-admin guard (incl. self-deletion) —
//     AUDITED the same way.
//   - 204 No Content on success, audited as user_deleted success with
//     targetRole = the role AT deletion time (the pre-delete snapshot — the
//     row is gone afterward).
//   - 500 generic problem if the write fails (audited as failure).
func UserDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "user deletion requires authentication")
		if !ok {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid user id")
			return
		}

		target, err := store.UserByID(r.Context(), db, id)
		if err != nil {
			writeStoreError(w, r, err, "user delete target lookup", "error", err)
			return
		}

		if !identity.CanDeleteUser(su.Role, target.Role) {
			auditTargetFailure(r, audit.EventUserDeleted, su, target)
			writeForbidden(w, r, "user deletion not permitted for this target")
			return
		}

		if err := store.DeleteUser(r.Context(), db, target.ID); err != nil {
			switch writeStoreError(w, r, err, "user deletion", "error", err) {
			case http.StatusConflict, http.StatusInternalServerError:
				auditTargetFailure(r, audit.EventUserDeleted, su, target)
			}
			return
		}

		logAuditTargetEvent(r, audit.EventUserDeleted, audit.ResultSuccess, su.UserID, target.ID, target.Role)
		writeNoContent(w, r)
	}
}

// Staff user list pagination bounds: default
// 20, maximum 100 — the shared limit plumbing's default, like every other
// collection (the Lit client sends its own explicit limit=10).
const (
	staffUserListDefaultLimit = 20
	staffUserListMaxLimit     = 100
)

// StaffUserList returns the handler for GET /api/v1/users.
//
// Outcomes:
//   - 200 with the staff list envelope for an authenticated moderator+.
//     email appears only for admin+ viewers; avatarUrl is always present
//     (null when the account has no avatar).
//   - 401 generic problem for an unauthenticated request.
//   - 403 for an authenticated user (role=user) — the floor is moderator+.
//   - 400 for malformed query strings, unknown/duplicate parameters, or an
//     invalid cursor.
//   - 422 {role, invalidValue} / {status, invalidValue} for filter values
//     outside the accepted role/status sets (silently
//     ignoring a filter would change the result set's semantics).
//   - 500 generic problem if the read itself fails.
func StaffUserList(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "staff user list requires authentication")
		if !ok {
			return
		}
		if !identity.CanViewStaffList(su.Role) {
			writeForbidden(w, r, "staff user list requires moderator or above")
			return
		}

		query, limit, after, ok := parseKeysetParams(w, r, staffUserListDefaultLimit, staffUserListMaxLimit, staffUserCursorPrefix, "role", "status")
		if !ok {
			return
		}
		roleFilter := query.Get("role")
		if roleFilter != "" && !identity.IsValidRole(roleFilter) {
			writeValidationErrors(w, r, []Violation{{Field: "role", Code: "invalidValue"}})
			return
		}
		statusFilter := query.Get("status")
		if statusFilter != "" && !identity.IsValidStatus(statusFilter) {
			writeValidationErrors(w, r, []Violation{{Field: "status", Code: "invalidValue"}})
			return
		}

		// The email gate is applied at the boundary: the store always
		// returns the column, and only the viewer's role decides whether
		// it reaches the wire.
		includeEmail := identity.CanViewStaffEmails(su.Role)
		writeKeysetPage(w, r, "staff user list query", keysetPage[store.StaffUser, StaffUserItem]{
			count: func(ctx context.Context) (int, error) {
				return store.CountStaffUsers(ctx, db, roleFilter, statusFilter)
			},
			read: func(ctx context.Context) ([]store.StaffUser, bool, error) {
				var key *store.UserPageKey
				if after != nil {
					key = &store.UserPageKey{CreatedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListStaffUsers(ctx, db, limit, key, roleFilter, statusFilter)
			},
			item: func(u store.StaffUser) StaffUserItem {
				return staffUserProjection(publicBaseURL, staffUserFieldsFromRow(u), includeEmail)
			},
			next: func(last store.StaffUser) string {
				return encodeCursor(staffUserCursorPrefix, last.CreatedAtMS, last.ID)
			},
		})
	}
}
