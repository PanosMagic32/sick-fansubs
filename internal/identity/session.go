package identity

// SessionUser is the combined result of a session lookup joined with
// the owning user's identity and authorization fields: who the user is,
// their role, the forced-change flag, and the CSRF token for unsafe-request
// validation.
//
// The SessionByDigest query enforces s.auth_version = u.auth_version and
// u.status = 'active' in its WHERE clause, so a SessionUser is only returned
// while the session is authoritative: a version bump or a suspension makes
// the lookup answer ErrNotFound. Absolute expiry is the caller's check
// (IsExpired); the query deliberately returns an expired row so the
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
	ExpiresAtMS        int64 // absolute expiry, UTC milliseconds
}

// IsExpired reports whether the session has reached its absolute expiry. The
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
