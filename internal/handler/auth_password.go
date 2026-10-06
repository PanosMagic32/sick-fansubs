package handler

import (
	"net/http"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

// HandlePasswordChange verifies the current password and replaces it with a new one.
//
// Requires a valid authenticated session (middleware enforces origin + CSRF).
// Revokes all sessions on success — the user must sign in again.
//
// Request:  { "currentPassword": "...", "newPassword": "..." }
// Success:  200 { "authenticated": false } + cleared cookie
// Failure:  400 (bad request), 401 (wrong current password, generic),
// 413 (body too large), 422 (current/new password validation)
func (h *Auth) HandlePasswordChange(w http.ResponseWriter, r *http.Request) {
	su, ok := requireSession(w, r, "password change requires authentication")
	if !ok {
		return
	}

	var req passwordChangeRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONBodyError(w, r, err)
		return
	}

	// Validate presence before calling the service.
	if req.CurrentPassword == "" || req.NewPassword == "" {
		var violations []Violation
		if req.CurrentPassword == "" {
			violations = append(violations, Violation{Field: "currentPassword", Code: "required"})
		}
		if req.NewPassword == "" {
			violations = append(violations, Violation{Field: "newPassword", Code: "required"})
		}
		writeValidationErrors(w, r, violations)
		return
	}

	// Bound the current password like sign-in's password field (the separately
	// bounded legacy-investigation field). Verification
	// compares at most 72 bytes, but the request contract stays explicit.
	if len(req.CurrentPassword) > 128 {
		writeValidationErrors(w, r, []Violation{
			{Field: "currentPassword", Code: "maxLength"},
		})
		return
	}

	// Rate-limit by user (5 per user, 15m window).
	allowed, retryAfter := h.pwChangeLimiter.Allow(su.UserID)
	if !allowed {
		writeTooManyRequests(w, r, retryAfter, "bucket", "password-change", "userId", su.UserID)
		return
	}

	err := h.auth.ChangePassword(r.Context(), su.UserID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		// Failure-result audit events fire only when the sensitive part of
		// the operation was actually attempted — wrong current password
		// (possible takeover attempt) or an internal failure. Request-level
		// validation rejections (422, new password too short etc.) do NOT
		// emit audit events, mirroring sign_in_failure's scope: the event
		// tracks attempted state changes, not form typos.
		switch status := writeAuthError(w, r, err, "password change", "error", err); status {
		case http.StatusUnauthorized, http.StatusInternalServerError:
			h.logAudit(r, audit.EventPasswordChanged, audit.ResultFailure, su.UserID)
		}
		return
	}

	h.logAudit(r, audit.EventPasswordChanged, audit.ResultSuccess, su.UserID)
	middleware.ClearSessionCookie(w, h.secure)
	writeJSON(w, r, http.StatusOK, signOutResponse{Authenticated: false})
}

// HandleForgotPassword mints a reset token and emails the reset link.
//
// Request:  { "identifier": "..." } (username OR email)
// Success:  200 {} — ALWAYS, whether the account exists or not (enumeration
// resistance; suspended accounts count as unknown).
//
// Failure:  400 (bad request), 403 (already authenticated —
// /problems/auth/already-authenticated, the sign-in/register precedent),
// 413 (body too large), 422 (validation), 429 (rate limits), 500 (send/
// store failure on a KNOWN account — an honest retryable failure; the
// recorded outage-residual channel).
func (h *Auth) HandleForgotPassword(w http.ResponseWriter, r *http.Request) {
	logger := logging.From(r.Context())
	if su := middleware.GetSession(r.Context()); su != nil {
		writeProblem(w, r, http.StatusForbidden,
			"/problems/auth/already-authenticated",
			"Already authenticated")
		logger.WarnContext(r.Context(), "forgot-password while authenticated")
		return
	}

	var req forgotPasswordRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONBodyError(w, r, err)
		return
	}

	if req.Identifier == "" {
		writeValidationErrors(w, r, []Violation{
			{Field: "identifier", Code: "required"},
		})
		return
	}
	if len(req.Identifier) > 256 {
		writeValidationErrors(w, r, []Violation{
			{Field: "identifier", Code: "maxLength"},
		})
		return
	}

	// Rate-limit by identifier (3 per identifier, 1h — the
	// sign-in dual-bucket precedent, so distributed IPs cannot inbox-bomb
	// one victim). The key uses the same whitespace/case fold the lenient
	// lookup resolves, so spelling variants share a bucket; it is never
	// logged (it may be a personal email address).
	rateLimitKey := store.NormalizeCanonWhitespace(req.Identifier)
	allowed, retryAfter := h.forgotIDLimiter.Allow(rateLimitKey)
	if !allowed {
		writeTooManyRequests(w, r, retryAfter, "bucket", "forgot-password-id")
		return
	}

	userID, err := h.auth.ForgotPassword(r.Context(), req.Identifier)
	if err != nil {
		// Audit only when the account WAS identified (userID set): the
		// event scopes to the send path. A sweep/lookup
		// failure before identification gets the 500 without an audit row
		// — the identifier stays out of the ledger.
		if userID != "" {
			h.logAudit(r, audit.EventPasswordResetRequested, audit.ResultFailure, userID)
		}
		writeInternalError(w, r, logger, "forgot-password failed", "error", err)
		return
	}

	// Known account: the email went out. Unknown account: nothing happened,
	// no audit row (the identifier is never logged). Same 200 {} body either
	// way.
	if userID != "" {
		h.logAudit(r, audit.EventPasswordResetRequested, audit.ResultSuccess, userID)
	}
	writeJSON(w, r, http.StatusOK, struct{}{})
}

// HandleResetPassword consumes a reset token and replaces the password.
//
// Request:  { "token": "...", "password": "..." }
// Success:  204 No Content; the session cookie is cleared when the current
// session belongs to the reset account (revocation — every session dies).
//
// Failure:  400 (bad request), 413 (body too large), 422 — a bad/expired/
// used token (or a suspended account) is the generic
// /problems/auth/reset-token-invalid with NO distinction; password-policy
// violations are field violations with registration's codes. Token
// validation runs BEFORE password validation (a bad token never reaches
// bcrypt). 429 (rate limits), 500 (internal).
func (h *Auth) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONBodyError(w, r, err)
		return
	}

	// Token shape first: a malformed token is the generic
	// token-invalid outcome — no field detail, nothing to distinguish.
	if len(req.Token) != 43 {
		writeResetTokenInvalid(w, r)
		return
	}

	// Password validation lives ENTIRELY in the service AFTER token
	// existence: a field violation here would leak a
	// password-policy answer before the token was checked — a bad token
	// plus any password shape must answer token-invalid only. The
	// service's shared policy covers empty (minLength) and over-long
	// (maxLength) submissions; the 4 KiB body bound caps the field.
	userID, role, err := h.auth.ResetPasswordWithToken(r.Context(), req.Token, req.Password)
	if err != nil {
		writeAuthError(w, r, err, "reset-password", "error", err)
		return
	}

	// Success audit: actor == target (the user reset their own account),
	// with the role snapshot — the event joins the
	// account-security reconciliation list.
	logAuditTargetEvent(r, audit.EventPasswordResetCompleted, audit.ResultSuccess, userID, userID, role)

	// Clear the cookie only when the current session belongs to the reset
	// account (it was revoked by the auth_version bump). A session of a
	// DIFFERENT user survives — their sessions were not touched. Clearing
	// happens only after the store transaction committed.
	if su := middleware.GetSession(r.Context()); su != nil && su.UserID == userID {
		middleware.ClearSessionCookie(w, h.secure)
	}
	writeNoContent(w, r)
}

// writeResetTokenInvalid writes the generic reset-token problem — the one
// public outcome for missing/expired/used tokens and suspended accounts.
func writeResetTokenInvalid(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusUnprocessableEntity,
		"/problems/auth/reset-token-invalid",
		"Invalid or expired reset token")
}
