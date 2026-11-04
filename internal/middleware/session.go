package middleware

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/problem"
	"sick-fansubs/internal/store"
)

// CookieName is the session cookie name.
const CookieName = "sf_session"

// Session is middleware that extracts the opaque session from the
// sf_session cookie, looks it up in the database, and attaches the
// authenticated user to the request context.
//
// The cookie value is an unpadded base64url-encoded 32-byte session token
// (always exactly 43 characters). The middleware decodes it, computes its
// SHA-256 digest, and looks up the session via store.SessionByDigest
// (which enforces auth_version matching). If the session is valid and not
// expired, the user is attached to the context via SetSession.
//
// A live session within identity.SessionRenewBefore of expiry is renewed:
// the row's expires_at_ms slides forward, capped at the session's absolute
// lifetime, and the cookie is reissued with the new remaining Max-Age. This
// keeps active users signed in without a SQLite write on every request.
//
// Missing, malformed, expired, revoked, or unknown session tokens result
// in an unauthenticated request — the next handler receives a context
// where SessionFrom returns nil. A session-store failure is different: it is
// an infrastructure failure, so the middleware answers the masked 500 and
// does not call the next handler.
//
// Expired sessions have their cookie cleared as a courtesy to the browser.
// The secure parameter controls the Secure attribute on the deletion cookie
// and must match the attribute used when the session cookie was originally
// set (false in dev, true in production).
func Session(db *sql.DB, secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookies := SessionCookies(r)
			// An ambiguous credential (duplicate sf_session cookies) is rejected
			// rather than choosing the first value.
			// Treating it as unauthenticated keeps the outcome generic —
			// the client sees the same result as an unknown session.
			if len(cookies) == 0 {
				logSessionReject(r, "no_cookie")
				next.ServeHTTP(w, r)
				return
			}
			if len(cookies) > 1 {
				logSessionReject(r, "duplicate_cookie", "cookieCount", len(cookies))
				next.ServeHTTP(w, r)
				return
			}

			digest, ok := SessionDigestValue(cookies[0].Value)
			if !ok {
				logSessionReject(r, "malformed_cookie")
				next.ServeHTTP(w, r)
				return
			}

			su, err := store.SessionByDigest(r.Context(), db, digest)
			if errors.Is(err, store.ErrNotFound) {
				logSessionReject(r, "not_found_or_revoked")
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				problem.WriteInternal(w, r, RequestIDFrom(r.Context()), "session lookup failed", "error", err)
				return
			}

			now := time.Now().UnixMilli()
			if su.IsExpired(now) {
				logSessionReject(r, "expired")
				ClearSessionCookie(w, secure)
				next.ServeHTTP(w, r)
				return
			}

			renewSession(w, r, db, su, cookies[0].Value, secure, now)

			ctx := SetSession(r.Context(), su)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// logSessionReject records WHY a request resolved as anonymous. Normal
// anonymous traffic (no cookie, malformed input, unknown or revoked
// credentials) is Debug; a duplicate cookie is Warn (cookie tossing / a
// cookie set at an older path or domain); expired credentials are Info
// because they explain a user-visible logout. Cookie values and digests
// never enter the log.
func logSessionReject(r *http.Request, reason string, attrs ...any) {
	logger := logging.From(r.Context()).With("sessionReject", reason)
	switch reason {
	case "duplicate_cookie":
		logger.WarnContext(r.Context(), "session rejected", attrs...)
	case "expired":
		logger.InfoContext(r.Context(), "session rejected", attrs...)
	default:
		logger.DebugContext(r.Context(), "session rejected", attrs...)
	}
}

// renewSession slides a live session's expiry when it is inside the renewal
// window, reissues the cookie on a successful write, and never moves the
// expiry past the absolute cap. A renewal write failure is logged and the
// request continues on the current (still valid) expiry — renewal is an
// enhancement, not a gate.
func renewSession(
	w http.ResponseWriter,
	r *http.Request,
	db *sql.DB,
	su *identity.SessionUser,
	cookieValue string,
	secure bool,
	nowMS int64,
) {
	if su.ExpiresAtMS-nowMS >= identity.SessionRenewBefore.Milliseconds() {
		return
	}

	renewedUntil := nowMS + identity.SessionLifetime.Milliseconds()
	absoluteCap := su.CreatedAtMS + identity.SessionMaxLifetime.Milliseconds()
	if renewedUntil > absoluteCap {
		renewedUntil = absoluteCap
	}
	if renewedUntil <= su.ExpiresAtMS {
		return
	}

	renewed, err := store.RenewSession(r.Context(), db, su.SessionID, su.ExpiresAtMS, renewedUntil)
	if err != nil {
		logging.From(r.Context()).WarnContext(r.Context(), "session renewal failed", "error", err)
		return
	}
	if !renewed {
		// A concurrent request won the CAS; its cookie is already correct.
		return
	}

	su.ExpiresAtMS = renewedUntil
	// The stored instants are milliseconds; the cookie carries seconds.
	maxAge := max(int((renewedUntil-nowMS)/1000), 1)
	SetSessionCookie(w, cookieValue, secure, maxAge)
	logging.From(r.Context()).DebugContext(r.Context(), "session renewed", "expiresAtMs", renewedUntil)
}

// SetSessionCookie writes the sf_session cookie with the shared attribute
// contract (host-only, Path=/, HttpOnly, SameSite=Lax, Secure in production).
// maxAge is in seconds; a negative value deletes the cookie.
func SetSessionCookie(w http.ResponseWriter, value string, secure bool, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionDigest runs the one ambiguity gate shared by session resolution and
// sign-out: exactly one well-formed sf_session cookie, or nil. Callers never
// fall back to r.Cookie, which would silently choose the first duplicate.
func SessionDigest(r *http.Request) []byte {
	cookies := SessionCookies(r)
	if len(cookies) != 1 {
		return nil
	}
	digest, ok := SessionDigestValue(cookies[0].Value)
	if !ok {
		return nil
	}
	return digest
}

// SessionCookies returns every cookie named sf_session on the request.
//
// net/http's r.Cookie silently returns the first match; callers use this
// helper to detect ambiguity instead (reject duplicate session
// cookies rather than choosing one).
func SessionCookies(r *http.Request) []*http.Cookie {
	var matches []*http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == CookieName {
			matches = append(matches, c)
		}
	}
	return matches
}

// SessionDigestValue validates a raw sf_session cookie value and returns its
// SHA-256 digest. ok is false for anything that cannot be a session token.
//
// A valid token is exactly 43 base64url characters encoding 32 bytes. The
// length check runs before the decoder so an arbitrarily long string never
// reaches it; the ordering is review-protected, not outcome-observable.
//
// Shared by the session middleware and the sign-out handler so the digest
// computation lives in exactly one place.
func SessionDigestValue(value string) (digest []byte, ok bool) {
	if len(value) != 43 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) != 32 {
		return nil, false
	}
	h := sha256.Sum256(raw)
	return h[:], true
}

// ClearSessionCookie writes a deletion cookie matching the sf_session
// cookie contract (same name, path, host-only scope, past expiry).
// The secure parameter must match the original Set-Cookie — browsers
// enforce downgrade protection: a Secure=false deletion cannot clear a
// Secure=true cookie.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	SetSessionCookie(w, "", secure, -1)
}
