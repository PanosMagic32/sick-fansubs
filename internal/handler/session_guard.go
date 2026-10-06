package handler

import (
	"net/http"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
)

// requireSession resolves the request's authenticated session and, when the
// request carries none, writes the generic 401 with the caller's log detail.
// The returned bool is false after the response is written, so the caller
// must return immediately.
func requireSession(w http.ResponseWriter, r *http.Request, logDetail string) (*identity.SessionUser, bool) {
	su := middleware.GetSession(r.Context())
	if su == nil {
		writeUnauthorized(w, r, logDetail)
		return nil, false
	}
	return su, true
}
