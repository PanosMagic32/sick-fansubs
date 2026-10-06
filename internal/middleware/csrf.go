package middleware

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

// CSRF is middleware that validates the X-CSRF-Token header against the
// session-bound CSRF token for unsafe HTTP methods.
//
// CSRF validation only applies when the request carries an authenticated
// session. Unauthenticated unsafe requests (e.g., sign-in, registration)
// are protected by the TrustedOrigin middleware; CSRF is skipped because
// the client doesn't have a token yet.
//
// Safe methods (GET, HEAD, OPTIONS) are never checked and must not mutate
// application state.
//
// On mismatch or missing header, the middleware responds with 403 Forbidden
// and does not call the next handler.
func CSRF() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			su := GetSession(r.Context())
			if su == nil {
				// No session — CSRF doesn't apply. TrustedOrigin covers this case.
				next.ServeHTTP(w, r)
				return
			}

			// Session-bound token: reject ambiguous credentials. Header.Values
			// returns every value for the key, so two header lines are rejected
			// instead of choosing the first; a comma-joined value fails the
			// base64 decode below.
			headerVals := r.Header.Values("X-CSRF-Token")
			if len(headerVals) == 0 || headerVals[0] == "" {
				writeForbidden(w, r, "missing CSRF token")
				return
			}
			if len(headerVals) > 1 {
				writeForbidden(w, r, "ambiguous CSRF token")
				return
			}

			clientToken, err := base64.RawURLEncoding.DecodeString(headerVals[0])
			if err != nil || len(clientToken) != 32 {
				writeForbidden(w, r, "invalid CSRF token format")
				return
			}

			// Defense-in-depth: ensure stored CSRF token is also 32 bytes.
			// subtle.ConstantTimeCompare returns immediately (non-constant-time)
			// if lengths differ, creating a minor timing side-channel; the
			// package documents the constant-time contract.
			if len(su.CSRF) != 32 {
				writeForbidden(w, r, "invalid stored CSRF token")
				return
			}

			if subtle.ConstantTimeCompare(clientToken, su.CSRF) != 1 {
				writeForbidden(w, r, "CSRF token mismatch")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isSafeMethod returns true for HTTP methods that must not mutate state.
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
