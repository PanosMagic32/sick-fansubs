package handler

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/store"
)

// Current-user profile: GET /api/v1/users/me.
//
// The first protected GET outside the auth namespace. The session bootstrap
// and auth responses project only id/username/role, while "email and avatar
// are fetched from the profile endpoint when needed". This handler owns that
// full read.
//
// The precise contract lives in docs/api/openapi.yaml (operationId
// getCurrentUserProfile); the route chain is
// RequestID → TrustedOrigin → Session (no CSRF — safe method; no rate
// limit — a cheap authenticated read, like the session bootstrap).

// UserProfile is the public profile projection of the current user.
//
// It is deliberately distinct from identity.User (full row, internal fields)
// and identity.PublicUser (the tiny bootstrap projection): this wire DTO
// carries the read-only account facts the /account page renders.
// Status is not exposed — SessionByDigest already filters out suspended
// users, so an authenticated request is by definition from an active
// account. Password and canonical keys never appear.
//
// AvatarURL is the ABSOLUTE served URL: the stored value is the 4-segment storage form whose
// 2-hex subdirectory never appears on the wire, so the handler
// projects through mediaURL — the same rule every content-ref avatarUrl
// uses. Null when no avatar is set (or the stored value fails the scheme
// guard).
type UserProfile struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	Email         string  `json:"email"`
	Role          string  `json:"role"`
	EmailVerified bool    `json:"emailVerified"`
	CreatedAt     string  `json:"createdAt"`
	AvatarURL     *string `json:"avatarUrl"`
}

// CurrentUserProfile returns the handler for GET /api/v1/users/me.
//
// avatarUrl is projected to the absolute served URL: the handler maps the
// stored storage-form path through mediaURL so the 2-hex images subdirectory
// never reaches the wire — the frontend renders the value directly as an img
// src. Null stays null; a stored value that fails the scheme guard projects
// as null too.
//
// Outcomes:
//   - 200 with the UserProfile projection for an authenticated request.
//   - 401 generic problem for an unauthenticated request, or for the
//     residual case where the session row outlives its user row (a user
//     deleted between the session lookup and this read) — the same
//     generic outcome keeps the discrepancy invisible.
//   - 500 generic problem if the read itself fails.
func CurrentUserProfile(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "profile requires authentication")
		if !ok {
			return
		}

		u, err := store.UserByID(r.Context(), db, su.UserID)
		if err != nil {
			writeStoreAccountError(w, r, err, "profile lookup", "error", err)
			return
		}

		writeJSON(w, r, http.StatusOK, userProfileDTO(u, publicBaseURL))
	}
}

// userProfileDTO projects a user row to the wire UserProfile. The avatar
// upload handler reuses it for its 200 response, so the GET and the PUT
// can never drift apart.
func userProfileDTO(u *identity.User, publicBaseURL string) UserProfile {
	return UserProfile{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		Role:          u.Role,
		EmailVerified: u.EmailVerifiedAtMS != nil,
		CreatedAt:     formatAPITime(u.CreatedAtMS),
		AvatarURL:     avatarURLOrNull(publicBaseURL, u.AvatarURL),
	}
}
