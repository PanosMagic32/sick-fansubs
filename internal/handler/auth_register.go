package handler

import (
	"net/http"
	"unicode/utf8"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
)

// HandleRegister creates a new user account and signs them in automatically.
//
// Request:  { "username": "...", "email": "...", "password": "..." }
// Success:  201 { "csrfToken": "...", "user": {...} } + Set-Cookie
//   - Location: /api/v1/auth/session
//
// Failure:  400 (bad request), 403 (already authenticated —
// /problems/auth/already-authenticated), 409 (duplicate race),
// 413 (body too large), 422 (validation)
func (h *Auth) HandleRegister(w http.ResponseWriter, r *http.Request) {
	logger := logging.From(r.Context())
	// Reject if already authenticated.
	if su := middleware.GetSession(r.Context()); su != nil {
		writeProblem(w, r, http.StatusForbidden,
			"/problems/auth/already-authenticated",
			"Already authenticated")
		logger.WarnContext(r.Context(), "register while authenticated")
		return
	}

	var req registerRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONBodyError(w, r, err)
		return
	}

	var violations []Violation

	if req.Username == "" {
		violations = append(violations, Violation{Field: "username", Code: "required"})
	} else if utf8.RuneCountInString(req.Username) > 128 {
		// The raw bound mirrors the wire schema's maxLength in code points;
		// the service then folds whitespace and applies its normalized 1-32
		// rule.
		violations = append(violations, Violation{Field: "username", Code: "maxLength"})
	}

	if req.Email == "" {
		violations = append(violations, Violation{Field: "email", Code: "required"})
	}

	if req.Password == "" {
		violations = append(violations, Violation{Field: "password", Code: "required"})
	}

	if len(violations) > 0 {
		writeValidationErrors(w, r, violations)
		return
	}

	sessionTok, csrfTok, user, err := h.auth.Register(r.Context(), req.Username, req.Email, req.Password, r.UserAgent())
	if err != nil {
		writeAuthError(w, r, err, "register", "error", err)
		return
	}

	// Verification email: mint + send AFTER the account committed.
	// A failure here never fails the registration — the user is signed in,
	// the response stays 201, and the account page's resend button is the
	// recovery path. Logged, never surfaced.
	if err := h.auth.SendVerificationEmail(r.Context(), user.ID); err != nil {
		logger.ErrorContext(r.Context(), "verification email send failed", "error", err)
	}

	setSessionCookie(w, sessionTok, h.secure)
	w.Header().Set("Location", "/api/v1/auth/session")
	writeJSON(w, r, http.StatusCreated, authUserResponse{
		CSRFToken: csrfTok,
		User:      user,
	})
}
