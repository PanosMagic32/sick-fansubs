// Package identity holds the account values every layer shares: the user row and
// its public projection, the session identity the middleware carries, the role
// model with its capability checks, and the account status vocabulary.
//
// These are plain data structs. They carry fields and validation concepts but own
// no HTTP, SQL, or business orchestration logic.
//
// Password and session-internal fields are included because the auth and store
// layers need them. They must never appear in API responses, logs, or error
// messages.
package identity

// User is the full domain representation of a user account.
//
// Password carries the bcrypt verifier string. It is never included in
// API responses, logs, error messages, fixtures, or OpenAPI examples.
// The PublicUser type (defined in this package) is the safe projection
// for HTTP responses.
type User struct {
	ID            string
	Username      string // display spelling as stored
	UsernameCanon string // lowercased, whitespace-folded lookup key
	Email         string // lowercased recovery identifier
	Password      string
	Role          string // one of the Role* values
	Status        string // one of the Status* values
	AuthVersion   int64  // revocation epoch; sessions match on it
	// MustChangePassword is the forced-change flag: an admin reset sets it and
	// the authenticated chain 403s every
	// non-exempt path until the user changes their password.
	MustChangePassword bool
	AvatarURL          *string // served media URL, nil when unset
	EmailVerifiedAtMS  *int64  // verification instant, nil while unverified
	CreatedAtMS        int64   // UTC milliseconds
	UpdatedAtMS        int64   // UTC milliseconds
}

// NewPublicUser projects the wire-safe identity fields into a PublicUser.
// Every envelope builds its user object through this constructor or one of
// the ToPublic methods, so the projection has one home.
func NewPublicUser(id, username, role string) PublicUser {
	return PublicUser{ID: id, Username: username, Role: role}
}

// PublicUser is the safe user projection returned in API responses.
//
// It includes only the fields the Lit layout shell needs on every
// authenticated page load: identity, display name, and role for
// conditional UI rendering. Email and avatar are fetched separately
// from the profile endpoint when needed.
type PublicUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// ToPublic returns a PublicUser projection of this user.
func (u *User) ToPublic() PublicUser {
	return NewPublicUser(u.ID, u.Username, u.Role)
}
