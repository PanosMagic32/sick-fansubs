package identity

import "time"

// Session timing is a two-bound policy:
//
//   - SessionLifetime is the IDLE lifetime (30 days). A request that lands
//     with less than SessionRenewBefore left renews the row and reissues the
//     cookie, so an active session keeps sliding forward.
//   - SessionMaxLifetime is the ABSOLUTE cap (90 days) measured from the
//     session's creation. Renewal never moves expires_at_ms past it, so a
//     stolen cookie cannot live forever.
const (
	SessionLifetime    = 30 * 24 * time.Hour
	SessionMaxLifetime = 90 * 24 * time.Hour
	// SessionRenewBefore is the remaining-lifetime threshold that triggers a
	// renewal. It is deliberately smaller than SessionLifetime so a focus /
	// navigation storm cannot turn every request into a SQLite write.
	SessionRenewBefore = 7 * 24 * time.Hour
)

// SessionUser is the combined result of a session lookup joined with
// the owning user's identity and authorization fields: who the user is,
// their role, the forced-change flag, and the CSRF token for unsafe-request
// validation.
//
// The SessionByDigest query enforces s.auth_version = u.auth_version and
// u.status = 'active' in its WHERE clause, so a SessionUser is only returned
// while the session is authoritative: a version bump or a suspension makes
// the lookup answer ErrNotFound. The current idle expiry is the caller's
// check (IsExpired); the query deliberately returns an expired row so the
// middleware can clear its cookie.
type SessionUser struct {
	SessionID string // the session row's own id
	UserID    string
	Username  string
	Role      string
	// MustChangePassword is the owning user's forced-change flag — selected so
	// the forced-change middleware can gate without an extra query.
	MustChangePassword bool
	CSRF               []byte
	CreatedAtMS        int64 // session creation, UTC milliseconds (the absolute-cap origin)
	ExpiresAtMS        int64 // current idle expiry, UTC milliseconds (never past the absolute cap)
}

// IsExpired reports whether the session has reached its current expiry. The
// expiry instant itself is expired; this agrees with the store's liveness
// predicate (expires_at_ms > now).
func (su *SessionUser) IsExpired(nowMS int64) bool {
	return nowMS >= su.ExpiresAtMS
}

// ToPublic returns the wire-safe public projection carried by session
// envelopes (the session bootstrap and the sign-in response).
func (su *SessionUser) ToPublic() PublicUser {
	return NewPublicUser(su.UserID, su.Username, su.Role)
}
