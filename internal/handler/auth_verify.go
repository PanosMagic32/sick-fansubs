package handler

import (
	"net/http"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/logging"
)

// HandleVerifyEmail consumes a verification token and stamps the account
// verified.
//
// Request:  { "token": "..." }
// Success:  204 No Content. Idempotent — an already-verified user holding
// a still-valid token also gets 204 (the token is consumed, the stamp is a
// no-op). Allowed regardless of session state; CSRF applies when a session
// resolves (the reset-password precedent). No session revocation, no
// auth_version bump — verification is not a security-state change.
//
// Failure:  400 (bad request), 413 (body too large), 422 — a bad/expired/
// used token (or a suspended account) is the generic
// /problems/auth/verification-token-invalid with NO distinction. 429
// (5/IP/15min), 500 (internal).
func (h *Auth) HandleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONBodyError(w, r, err)
		return
	}

	// Token shape first: a malformed token is the generic
	// token-invalid outcome — no field detail, nothing to distinguish.
	if len(req.Token) != 43 {
		writeVerificationTokenInvalid(w, r)
		return
	}

	userID, role, changed, err := h.auth.VerifyEmail(r.Context(), req.Token)
	if err != nil {
		writeAuthError(w, r, err, "verify-email", "error", err)
		return
	}

	// Success audit — only when the stamp actually LANDED. The
	// idempotent path (already verified, token consumed) is a no-op and
	// emits nothing (the role-change no-op precedent). Ledger-only — the
	// event does not join the account-security reconciliation list.
	if changed {
		logAuditTargetEvent(r, audit.EventEmailVerified, audit.ResultSuccess, userID, userID, role)
	}

	writeNoContent(w, r)
}

// writeVerificationTokenInvalid writes the generic verification-token
// problem — the one public outcome for missing/expired/used tokens and
// suspended accounts.
func writeVerificationTokenInvalid(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusUnprocessableEntity,
		"/problems/auth/verification-token-invalid",
		"Invalid or expired verification token")
}

// HandleResendVerification re-mints and re-sends the verification email for
// the authenticated user.
//
// Requires a valid authenticated session (middleware enforces origin +
// CSRF). No request body.
//
// Success:  204 No Content — ALWAYS, whether the account was unverified
// (email sent) or already verified (no-op, no send, and NO bucket
// consumption — the verified check precedes the limiter).
// Failure:  401 (unauthenticated), 429 (the shared 1/30min bucket —
// unverified sends only), 500 (send/store failure — an explicit send
// operation never silently drops).
func (h *Auth) HandleResendVerification(w http.ResponseWriter, r *http.Request) {
	logger := logging.From(r.Context())
	su, ok := requireSession(w, r, "resend verification requires authentication")
	if !ok {
		return
	}

	// The verified no-op is checked BEFORE the shared bucket ("no send,
	// nothing to throttle") — a verified user's
	// resend burns no quota. The advisory read races benignly: a verify
	// landing between the check and the send makes the send a no-op.
	verified, err := h.auth.IsEmailVerified(r.Context(), su.UserID)
	if err != nil {
		writeInternalError(w, r, logger, "resend-verification check failed", "error", err)
		return
	}
	if verified {
		writeNoContent(w, r)
		return
	}

	// Rate-limit by user (the SHARED verification-send
	// bucket — 1 per user per 30 minutes, the same instance the email
	// change shares). The key is the user id, logged as an attribute.
	allowed, retryAfter := h.resendLimiter.Allow(su.UserID)
	if !allowed {
		writeTooManyRequests(w, r, retryAfter, "bucket", "resend-verification", "userId", su.UserID)
		return
	}

	if err := h.auth.ResendVerification(r.Context(), su.UserID); err != nil {
		writeInternalError(w, r, logger, "resend-verification failed", "error", err)
		return
	}

	writeNoContent(w, r)
}
