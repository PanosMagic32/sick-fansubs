package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

const (
	// sessionMaxAge is the sf_session cookie lifetime in seconds, derived from
	// the service-owned SessionLifetime (7 days). The service
	// package owns the value — the handler only converts units for the cookie.
	sessionMaxAge = int(auth.SessionLifetime / time.Second)
)

// setSessionCookie writes the sf_session cookie to the response.
//
// HttpOnly, SameSite=Lax, Path=/, host-only (no Domain),
// 7-day Max-Age. The secure parameter controls the Secure attribute (false
// for local HTTP dev, true for production).
func setSessionCookie(w http.ResponseWriter, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// cookieDigest reads the sf_session cookie, base64url-decodes it, and returns
// its SHA-256 digest. Missing, malformed, or ambiguous (duplicate) cookies
// return nil — an ambiguous credential is never acted on.
func cookieDigest(r *http.Request) []byte {
	cookies := middleware.SessionCookies(r)
	if len(cookies) != 1 {
		return nil
	}
	digest, ok := middleware.SessionDigestValue(cookies[0].Value)
	if !ok {
		return nil
	}
	return digest
}

// maxAuthBodySize is the maximum request body size for auth endpoints (4 KiB).
const maxAuthBodySize = 4 * 1024

// readJSON decodes a JSON request body into dst. It enforces:
//   - Content-Type: application/json (exact media type; charset parameter allowed)
//   - Body size limit (maxAuthBodySize) → errBodyTooLarge (handler maps to 413)
//   - DisallowUnknownFields
//   - Exactly one JSON value (no trailing data)
//
// Returns a classified sentinel error on failure; the caller maps it
// through writeJSONBodyError.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return readJSONLimit(w, r, dst, maxAuthBodySize)
}

// readJSONLimit is readJSON with an explicit byte bound — the comment
// endpoints use maxCommentBodySize.
func readJSONLimit(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	ct := r.Header.Get("Content-Type")
	// ParseMediaType normalizes case and separates parameters, so
	// "application/json; charset=utf-8" is accepted while look-alike types
	// ("application/jsonx", "application/json-patch+json") are rejected.
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}

	// Read the entire body under the size limit BEFORE parsing. Streaming
	// the reader into json.Decoder interacts subtly with its internal
	// buffering: depending on order, either oversized bodies could be
	// misreported as 400 (More() peeks only buffered bytes) or in-limit
	// trailing data could be missed after a drain. Read-then-parse keeps
	// the contract simple and airtight: oversize → 413 and trailing data
	// → 400, both regardless of where the bytes sit in the body.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return errBodyTooLarge
		}
		return errInvalidBody
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return classifyDecodeError(err)
	}

	// Exactly one JSON value: a second decode must reach end of input. The
	// second-decode idiom (not dec.More(), which treats a stray `}`/`]` at
	// the top level as "no more elements") is the one the cursor codecs use.
	// The decoder scans the fully buffered body, so this sees trailing data
	// at any offset within the limit.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errTrailingData
	}

	return nil
}

type signInRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type passwordChangeRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type forgotPasswordRequest struct {
	Identifier string `json:"identifier"`
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

type authUserResponse struct {
	CSRFToken          string              `json:"csrfToken"`
	User               identity.PublicUser `json:"user"`
	MustChangePassword bool                `json:"mustChangePassword"`
}

type sessionBootstrapResponse struct {
	Authenticated      bool                 `json:"authenticated"`
	CSRFToken          *string              `json:"csrfToken,omitempty"`
	User               *identity.PublicUser `json:"user"`
	MustChangePassword bool                 `json:"mustChangePassword"`
}

type signOutResponse struct {
	Authenticated bool `json:"authenticated"`
}

// Auth holds the dependencies of the authentication HTTP handlers, whose
// methods the route table registers.
type Auth struct {
	auth            *auth.Service
	secure          bool
	signInIDLimiter *middleware.RateLimiter // identifier-based, 5/15m
	pwChangeLimiter *middleware.RateLimiter // user-based, 5/15m
	forgotIDLimiter *middleware.RateLimiter // identifier-based, 3/1h
	resendLimiter   *middleware.RateLimiter // shared verification-send bucket, 1/30min
}

// NewAuth creates an Auth with the given dependencies.
func NewAuth(auth *auth.Service, secure bool, signInIDLimiter, pwChangeLimiter, forgotIDLimiter, resendLimiter *middleware.RateLimiter) *Auth {
	return &Auth{auth: auth, secure: secure, signInIDLimiter: signInIDLimiter, pwChangeLimiter: pwChangeLimiter, forgotIDLimiter: forgotIDLimiter, resendLimiter: resendLimiter}
}

// HandleSignIn authenticates a user and creates a new session.
//
// Request:  { "identifier": "...", "password": "..." }
// Success:  200 { "csrfToken": "...", "user": { "id", "username", "role" } }
//   - Set-Cookie: sf_session=...
//
// Failure:  400 (bad request), 401 (invalid credentials, same for all causes),
// 403 (already authenticated), 413 (body too large), 422 (validation)
func (h *Auth) HandleSignIn(w http.ResponseWriter, r *http.Request) {
	logger := logging.From(r.Context())
	// Sign-in-while-authenticated: a valid session means the client state is stale. Reject with
	// 403 so the frontend can reconcile via session re-bootstrap.
	if su := middleware.GetSession(r.Context()); su != nil {
		writeProblem(w, r, http.StatusForbidden,
			"/problems/auth/already-authenticated",
			"Already authenticated")
		logger.WarnContext(r.Context(), "sign-in while authenticated")
		return
	}

	var req signInRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONBodyError(w, r, err)
		return
	}

	// Validate field lengths before passing to the service.
	if len(req.Identifier) > 256 {
		writeValidationErrors(w, r, []Violation{
			{Field: "identifier", Code: "maxLength"},
		})
		return
	}
	if len(req.Password) > 128 {
		writeValidationErrors(w, r, []Violation{
			{Field: "password", Code: "maxLength"},
		})
		return
	}
	if req.Identifier == "" {
		writeValidationErrors(w, r, []Violation{
			{Field: "identifier", Code: "required"},
		})
		return
	}
	if req.Password == "" {
		writeValidationErrors(w, r, []Violation{
			{Field: "password", Code: "required"},
		})
		return
	}

	// Rate-limit by identifier (5 per identifier, 15m window).
	// The key uses the same whitespace/case fold the lenient lookup resolves,
	// so spelling variants of one account share a bucket (a raw lowercase key
	// let "bob" and " bob" each buy a fresh window). The identifier itself is
	// never logged (it may be a personal email address).
	rateLimitKey := store.NormalizeCanonWhitespace(req.Identifier)
	allowed, retryAfter := h.signInIDLimiter.Allow(rateLimitKey)
	if !allowed {
		writeTooManyRequests(w, r, retryAfter, "bucket", "sign-in-id")
		return
	}

	sessionTok, csrfTok, user, mustChange, err := h.auth.SignIn(r.Context(), req.Identifier, req.Password, r.UserAgent())
	if err != nil {
		// Audit the failed attempt. The identifier is
		// deliberately absent — failures must not disclose which account
		// exists or was targeted.
		h.logAudit(r, audit.EventSignInFailure, audit.ResultFailure, "")
		writeAuthError(w, r, err, "sign-in", "error", err)
		return
	}

	h.logAudit(r, audit.EventSignInSuccess, audit.ResultSuccess, user.ID)
	setSessionCookie(w, sessionTok, h.secure)
	writeJSON(w, r, http.StatusOK, authUserResponse{
		CSRFToken:          csrfTok,
		User:               user,
		MustChangePassword: mustChange,
	})
}

// logAudit delegates to the package's one actor-only emitter so its many call
// sites read unchanged and a change to the record shape reaches every one.
func (h *Auth) logAudit(r *http.Request, event, result, userID string) {
	logAuditEvent(r, event, result, userID)
}

// HandleSession returns the current authentication state.
//
// Authenticated:    200 { "authenticated": true,  "csrfToken": "...", "user": {...} }
// Unauthenticated:  200 { "authenticated": false, "user": null }
//
// Cache-Control: no-store. No cookie is modified.
func (h *Auth) HandleSession(w http.ResponseWriter, r *http.Request) {
	su := middleware.GetSession(r.Context())
	if su == nil {
		writeJSON(w, r, http.StatusOK, sessionBootstrapResponse{
			Authenticated: false,
			User:          nil,
		})
		return
	}

	csrfEncoded := base64.RawURLEncoding.EncodeToString(su.CSRF)
	user := su.ToPublic()

	writeJSON(w, r, http.StatusOK, sessionBootstrapResponse{
		Authenticated:      true,
		CSRFToken:          &csrfEncoded,
		User:               &user,
		MustChangePassword: su.MustChangePassword,
	})
}

// HandleSignOut deletes the current session and clears the cookie.
//
// Idempotent: a missing or already-revoked session returns the same 200.
// Trusted-origin validation applies even when no session is present.
// CSRF validation applies only when a valid session exists (middleware).
//
// Success: 200 { "authenticated": false } + cleared cookie.
func (h *Auth) HandleSignOut(w http.ResponseWriter, r *http.Request) {
	logger := logging.From(r.Context())
	// Try to delete the session if we can identify it.
	digest := cookieDigest(r)
	if digest != nil {
		deleted, err := h.auth.SignOut(r.Context(), digest)
		if err != nil {
			// The operation was attempted and failed — a failure-result
			// audit event keeps the attempt visible in the user's audit
			// trail (userID when the session middleware resolved it).
			userID := ""
			if su := middleware.GetSession(r.Context()); su != nil {
				userID = su.UserID
			}
			h.logAudit(r, audit.EventSignOut, audit.ResultFailure, userID)
			// Do NOT clear the cookie: the session row still exists server-side,
			// so the client must keep the credential and retry (clear the
			// cookie only after the deletion commits).
			writeInternalError(w, r, logger, "sign-out session delete failed", "error", err)
			return
		}
		// Audit only when a session row was actually deleted. A well-formed
		// but stale cookie (already revoked/expired session) hits the
		// idempotent no-session outcome — nothing changed server-side, and
		// replayed stale cookies must not generate spurious audit records.
		// userID comes from the session middleware when it resolved the
		// credential; a version-mismatched orphan row has none.
		if deleted > 0 {
			userID := ""
			if su := middleware.GetSession(r.Context()); su != nil {
				userID = su.UserID
			}
			h.logAudit(r, audit.EventSignOut, audit.ResultSuccess, userID)
		}
	}

	middleware.ClearSessionCookie(w, h.secure)
	writeJSON(w, r, http.StatusOK, signOutResponse{Authenticated: false})
}

// HandleSignOutAll revokes every session for the authenticated user.
//
// Idempotent: the auth chain carries no session requirement, so a call
// without a session has nothing to revoke, clears the cookie, and answers the
// same 200. Trusted-origin validation applies even without a session; CSRF
// validation applies only when a session exists (middleware). Increments
// auth_version and deletes all session rows via the service. A racing sign-in
// that commits after the bump wins; one that commits before is deleted.
//
// Success: 200 { "authenticated": false } + cleared cookie.
func (h *Auth) HandleSignOutAll(w http.ResponseWriter, r *http.Request) {
	logger := logging.From(r.Context())
	su := middleware.GetSession(r.Context())
	if su == nil {
		// The auth chain carries no session requirement, so an unauthenticated
		// call lands here: there is nothing to revoke, and the idempotent 200
		// (cookie cleared) matches sign-out's shape.
		middleware.ClearSessionCookie(w, h.secure)
		writeJSON(w, r, http.StatusOK, signOutResponse{Authenticated: false})
		return
	}

	if err := h.auth.SignOutAll(r.Context(), su.UserID); err != nil {
		// Attempted-and-failed revocation is itself audit-worthy: operators
		// investigating an account takeover need to see that a sign-out-all
		// attempt did NOT succeed.
		h.logAudit(r, audit.EventSignOutAll, audit.ResultFailure, su.UserID)
		writeInternalError(w, r, logger, "sign-out-all failed", "error", err)
		return
	}

	h.logAudit(r, audit.EventSignOutAll, audit.ResultSuccess, su.UserID)
	middleware.ClearSessionCookie(w, h.secure)
	writeJSON(w, r, http.StatusOK, signOutResponse{Authenticated: false})
}
