package handler

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
)

// Email change: PUT /api/v1/users/me/email.
//
// The account's email is the recovery identifier, so the change requires
// the CURRENT password (a stolen session must not reroute recovery). The
// route chain (RequestID → TrustedOrigin → Session → ForcePasswordChange →
// CSRF) enforces authentication + origin + CSRF like every protected users
// route.
//
// The shared verification-send bucket (1 per user per 30 minutes) gates the
// WHOLE operation: over limit = 429, nothing changes — no state change
// without its verification email, no email without its state change.
//
// After the commit the handler sends the verification email to the NEW
// address and a security notice to the PREVIOUS address; a failure of either
// is logged, never surfaced (the resend surface recovers, and the notice is a
// best-effort signal).

// emailChangeRequest is the PUT /api/v1/users/me/email body.
type emailChangeRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"currentPassword"`
}

// EmailChange returns the handler for PUT /api/v1/users/me/email.
//
// Outcomes:
//   - 401 generic problem for an unauthenticated request.
//   - 422 field violations: email required/invalidFormat/alreadyTaken,
//     currentPassword required.
//   - 401 generic problem for a wrong current password (the sign-in
//     outcome) — AUDITED as an email_changed failure (a recovery-reroute
//     attempt is exactly what the trail should show).
//   - 429 + Retry-After when the shared verification-send bucket is hot.
//   - 200 with the refreshed UserProfile on success (or the same-email
//     no-op: no sends, no audit). The verification email and the
//     previous-address notice run after the commit; a failure of either is
//     logged, never surfaced.
//   - 500 generic problem if the write fails.
func EmailChange(db *sql.DB, svc *auth.Service, publicBaseURL string, limiter *middleware.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "email change requires authentication")
		if !ok {
			return
		}

		var req emailChangeRequest
		if err := readJSON(w, r, &req); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}

		var violations []Violation
		if req.Email == "" {
			violations = append(violations, Violation{Field: "email", Code: "required"})
		}
		if req.CurrentPassword == "" {
			violations = append(violations, Violation{Field: "currentPassword", Code: "required"})
		}
		if len(violations) > 0 {
			writeValidationErrors(w, r, violations)
			return
		}

		// The shared verification-send bucket: checked
		// BEFORE the transaction — throttled requests change nothing.
		allowed, retryAfter := limiter.Allow(su.UserID)
		if !allowed {
			writeTooManyRequests(w, r, retryAfter, "bucket", "verification-send", "userId", su.UserID)
			return
		}

		u, previousEmail, changed, err := svc.ChangeEmail(r.Context(), su.UserID, req.Email, req.CurrentPassword)
		if err != nil {
			if writeAuthError(w, r, err, "email change", "error", err) == http.StatusUnauthorized {
				// A recovery-reroute attempt is exactly what the trail should
				// show; field and race rejections stay unaudited.
				logAuditTargetEvent(r, audit.EventEmailChanged, audit.ResultFailure, su.UserID, su.UserID, su.Role)
			}
			return
		}

		if changed {
			// Audit the committed swap (actor == target, role snapshot —
			// the refreshed row's role). The email VALUE is never logged.
			logAuditTargetEvent(r, audit.EventEmailChanged, audit.ResultSuccess, u.ID, u.ID, u.Role)

			// Verification email for the NEW address. A failure never
			// fails the change — logged, resend recovers.
			if err := svc.SendVerificationEmail(r.Context(), u.ID); err != nil {
				logger.ErrorContext(r.Context(), "email change verification send failed", "error", err)
			}

			// Security notice to the PREVIOUS address — best effort, a
			// failure never fails the committed change.
			if err := svc.SendEmailChangeNotice(r.Context(), previousEmail, u.Email); err != nil {
				logger.ErrorContext(r.Context(), "email change notice send failed", "error", err)
			}
		}

		writeJSON(w, r, http.StatusOK, userProfileDTO(u, publicBaseURL))
	}
}
