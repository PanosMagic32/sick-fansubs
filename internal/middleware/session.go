package middleware

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"sick-fansubs/internal/logging"
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
// Missing, malformed, expired, revoked, or unknown session tokens result
// in an unauthenticated request — the next handler receives a context
// where GetSession returns nil. No error is returned to the client.
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
			if len(cookies) != 1 {
				next.ServeHTTP(w, r)
				return
			}

			digest, ok := SessionDigestValue(cookies[0].Value)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			su, err := store.SessionByDigest(r.Context(), db, digest)
			if errors.Is(err, store.ErrNotFound) {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				logging.From(r.Context()).ErrorContext(r.Context(), "session lookup failed", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			now := time.Now().UnixMilli()
			if su.IsExpired(now) {
				ClearSessionCookie(w, secure)
				next.ServeHTTP(w, r)
				return
			}

			ctx := SetSession(r.Context(), su)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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

// clearSessionCookie writes a deletion cookie matching the sf_session
// cookie contract (same name, path, host-only scope, past expiry).
// The secure parameter must match the original Set-Cookie — browsers
// enforce downgrade protection: a Secure=false deletion cannot clear a
// Secure=true cookie.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
