package middleware

import (
	"net/http"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/problem"
)

// ForcePasswordChange rejects a session whose user must change their password
// with 403 /problems/auth/password-change-required. It slots after Session and
// before CSRF; the exemption's shape is docs/patterns/go/route-chains.md rule 9.
func ForcePasswordChange() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			su := GetSession(r.Context())
			if su == nil || !su.MustChangePassword {
				next.ServeHTTP(w, r)
				return
			}

			reqID := GetRequestID(r.Context())
			logging.From(r.Context()).WarnContext(r.Context(), "password change required")
			problem.Write(w, r, http.StatusForbidden, reqID,
				"/problems/auth/password-change-required", "Password change required")
		})
	}
}
