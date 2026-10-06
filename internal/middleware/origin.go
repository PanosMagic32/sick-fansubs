package middleware

import (
	"net/http"
	"strings"
)

// TrustedOrigin is middleware that validates the Origin (or Referer) header
// against a configured trusted origin for all unsafe HTTP methods.
//
// This is the first line of defense against cross-site request forgery:
// the browser sets Origin automatically on cross-origin requests, and a
// matching check confirms the request came from our own frontend.
//
// The check applies to all unsafe methods (POST, PUT, PATCH, DELETE),
// including sign-in and registration where no authenticated session exists
// yet. Safe methods are not checked.
//
// The trustedOrigin parameter is the exact scheme+host(+port) of the
// frontend, e.g. "http://localhost:5173" in development. Comparison is
// case-insensitive for the scheme portion (RFC 7230).
//
// If neither Origin nor Referer is present or they don't match, the
// middleware responds with 403 Forbidden.
//
// WARNING: TrustedOrigin MUST wrap CSRF in the middleware chain. If CSRF
// runs first, its 403 response would leak session validity to cross-origin
// attackers before the origin check has a chance to reject the request.
// Correct order: RequestID → TrustedOrigin → Session → CSRF.
func TrustedOrigin(trustedOrigin string) func(http.Handler) http.Handler {
	trustedOrigin = strings.ToLower(trustedOrigin)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			// Duplicate Origin values are ambiguous credentials — reject rather
			// than choosing the first.
			origins := r.Header.Values("Origin")
			if len(origins) > 1 {
				writeForbidden(w, r, "ambiguous origin")
				return
			}
			origin := ""
			if len(origins) == 1 {
				origin = origins[0]
			}
			if origin == "" {
				refs := r.Header.Values("Referer")
				if len(refs) > 1 {
					writeForbidden(w, r, "ambiguous referer")
					return
				}
				if len(refs) == 1 {
					origin = refs[0]
				}
				// Strip path, query, and fragment from Referer (e.g. Referrer-Policy: origin).
				if idx := strings.Index(origin, "://"); idx != -1 {
					rest := origin[idx+3:]
					if slash := strings.IndexAny(rest, "/?#"); slash != -1 {
						origin = origin[:idx+3+slash]
					}
				}
			}

			// Case-insensitive comparison for scheme portion (RFC 7230 §2.7.3).
			if !strings.EqualFold(origin, trustedOrigin) {
				writeForbidden(w, r, "untrusted origin")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
