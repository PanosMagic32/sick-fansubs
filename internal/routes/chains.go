package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/middleware"
)

// This file is the only place a middleware chain is composed; every other
// file names one of the constructors below. The rules are in
// docs/patterns/go/route-chains.md.

// publicChain wraps a handler that needs only the request ID: public reads,
// health probes, media serving, and the JSON 404 fallbacks.
func publicChain(h http.Handler) http.Handler {
	return middleware.RequestID()(h)
}

// optionalSessionChain wraps a public read that personalizes when a session is
// present: RequestID → TrustedOrigin → Session.
func optionalSessionChain(h http.Handler, db *sql.DB, secure bool, trustedOrigin string) http.Handler {
	h = middleware.Session(db, secure)(h)
	h = middleware.TrustedOrigin(trustedOrigin)(h)
	return middleware.RequestID()(h)
}

// authenticatedReadsChain wraps an authenticated read:
// RequestID → TrustedOrigin → Session → ForcePasswordChange.
func authenticatedReadsChain(h http.Handler, db *sql.DB, secure bool, trustedOrigin string) http.Handler {
	h = middleware.ForcePasswordChange()(h)
	h = middleware.Session(db, secure)(h)
	h = middleware.TrustedOrigin(trustedOrigin)(h)
	return middleware.RequestID()(h)
}

// authenticatedChain wraps an unsafe handler: RequestID → TrustedOrigin →
// Session → ForcePasswordChange → CSRF.
func authenticatedChain(h http.Handler, db *sql.DB, secure bool, trustedOrigin string) http.Handler {
	h = middleware.CSRF()(h)
	h = middleware.ForcePasswordChange()(h)
	h = middleware.Session(db, secure)(h)
	h = middleware.TrustedOrigin(trustedOrigin)(h)
	return middleware.RequestID()(h)
}

// pathLimit pairs a rate limiter with the subtree-relative path it guards.
type pathLimit struct {
	limiter *middleware.RateLimiter
	path    string
}

// authChain wraps the auth subtree mux:
// RequestID → TrustedOrigin → the path-scoped IP limits → Session → CSRF.
// It carries no forced-change gate (docs/patterns/go/route-chains.md rule 9).
func authChain(h http.Handler, db *sql.DB, secure bool, trustedOrigin string, limits ...pathLimit) http.Handler {
	h = middleware.CSRF()(h)
	h = middleware.Session(db, secure)(h)
	for _, limit := range limits {
		h = middleware.RateLimitIPByPath(limit.limiter, limit.path)(h)
	}
	h = middleware.TrustedOrigin(trustedOrigin)(h)
	return middleware.RequestID()(h)
}
