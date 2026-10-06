// Package auth holds the account-facing business rules: sign-in and registration,
// password change and reset, email verification, session lifecycle, and the
// session device label.
//
// A Service coordinates store operations for those rules. It owns no HTTP
// concerns (handler), no raw SQL (store), and no shared domain values
// (identity).
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sick-fansubs/internal/id"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/store"
)

// SessionLifetime is the absolute session lifetime (seven days).
// Exported because the HTTP layer derives the cookie Max-Age (seconds) from it.
const SessionLifetime = 7 * 24 * time.Hour

// Temp password generation: 16 characters from an
// unambiguous alphabet — the visually confusable 0/O/1/l/I are excluded so
// the operator can read it out or type it without ambiguity. It satisfies the
// 8–72-byte policy and uses crypto/rand (secrets never use math/rand).
const (
	tempPasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	tempPasswordLength   = 16
)

// Service orchestrates authentication operations: sign-in, registration,
// password change, self-service password reset, and session management.
//
// It owns business rules (password policy, canonicalization, timing defense)
// and coordinates store operations. It does not own HTTP parsing, SQL
// queries, or SMTP transport (the mail package owns that).
type Service struct {
	db     *sql.DB
	sender mail.Sender
	base   string // PublicBaseURL — the reset-link origin
	// bcryptCost is the cost for NEW hashes (the production default;
	// tests lower it — see the bcryptCost const comment).
	bcryptCost int
}

// New creates a Service backed by the given database pool.
// sender delivers the reset email (mail.Relay in production, mail.LogLink
// in loopback dev); base is the canonical public origin for link
// construction. Both are required — cmd/api wires them from config.
//
// The dummy bcrypt hash for timing defense is warmed at construction time
// so that a generation failure is detected at startup, not at request time.
func New(db *sql.DB, sender mail.Sender, base string) *Service {
	_ = dummyBcryptHash() // warm cache, fail early if bcrypt is broken
	return &Service{db: db, sender: sender, base: base, bcryptCost: bcryptCost}
}

// SignIn authenticates a user by username or email and creates a new session.
//
// The identifier accepts either a username or an email address; the
// resolution chain (canonicalization, the '@' email-first order, and the
// whitespace-lenient fallback) lives on [Service.lookupUserByIdentifier].
//
// On failure, the returned error is always ErrInvalidCredentials regardless
// of the underlying cause (unknown user, wrong password, suspended account,
// auth-version race). A dummy bcrypt comparison is performed when the user
// is not found to reduce timing differences.
//
// On success, returns the encoded session token (for the sf_session cookie),
// the encoded CSRF token (for the X-CSRF-Token header), the user's public
// identity, and the forced-change flag (the sign-in response carries
// mustChangePassword so the client routes proactively).
//
// userAgent is the raw User-Agent header. It is classified into the bounded
// device label stored with the session and is never persisted or logged
// itself (sessionClientLabel owns the privacy rule).
func (s *Service) SignIn(ctx context.Context, identifier, password, userAgent string) (sessionToken, csrfToken string, user identity.PublicUser, mustChangePassword bool, err error) {
	u, err := s.lookupUserByIdentifier(ctx, identifier)
	if errors.Is(err, store.ErrNotFound) {
		// Dummy bcrypt to maintain timing consistency with the real path.
		_ = compareDummy(dummyBcryptHash(), []byte(password))
		return "", "", identity.PublicUser{}, false, ErrInvalidCredentials
	}

	if err != nil {
		return "", "", identity.PublicUser{}, false, err
	}

	// Verify the password: the cost gate rejects unusable
	// verifiers before any expensive work, and a submission longer than 72
	// bytes is compared by its first 72 bytes — the credential a legacy
	// truncating library actually encoded. CompareHashAndPassword itself is
	// constant-time (subtle.ConstantTimeCompare), so an attacker cannot
	// distinguish "wrong password" from "correct password" by timing alone.
	cost, err := verifyPassword([]byte(u.Password), []byte(password))
	if err != nil {
		// A verifier that cannot safely perform the real comparison (malformed
		// hash, unsupported version, cost above the operational maximum) must
		// still run the dummy comparison before the generic failure. Without
		// it, this path is measurably faster than the unknown-user path and
		// leaks "this account has a broken verifier" (enumeration
		// resistance).
		if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			_ = compareDummy(dummyBcryptHash(), []byte(password))
		}
		return "", "", identity.PublicUser{}, false, ErrInvalidCredentials
	}

	// Decide whether the stored verifier needs a rehash: cost below the target
	// → rehash at cost 12; cost at or above the target → retain. One further
	// rule: an over-72-byte legacy credential cannot be rehashed (Go refuses
	// to hash it), so that sign-in proceeds under the unchanged verifier and
	// the account relies on the self-service password-change path.
	// Hash generation is best-effort — if it fails, sign-in proceeds under the
	// old verifier.
	rehashNeeded := false
	var newHash string
	// The COMPARISON side of the rehash gate uses the same field as the
	// generation side — a test-lowered cost must not turn every sign-in
	// into a perpetual rehash loop (the seam stays self-consistent).
	if cost < s.bcryptCost && len(password) <= legacyBcryptMaxPasswordBytes {
		if h, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost); err == nil {
			rehashNeeded, newHash = true, string(h)
		}
	}

	// Check account status after password verification so that the timing
	// profile of a suspended user matches that of an active user with a
	// correct password (both paths perform a real bcrypt comparison).
	if u.Status != identity.StatusActive {
		return "", "", identity.PublicUser{}, false, ErrInvalidCredentials
	}

	// Create session — atomically with the rehash when one is pending, in one
	// transaction under the new auth_version; splitting the rehash from the
	// session write would stamp the session with the stale version.
	sessionTok, csrfTok, err := s.createSessionAfterSignIn(ctx, u, rehashNeeded, newHash, sessionClientLabel(userAgent))
	if err != nil {
		if errors.Is(err, store.ErrAuthVersionMismatch) || errors.Is(err, store.ErrNotFound) {
			return "", "", identity.PublicUser{}, false, ErrInvalidCredentials
		}
		return "", "", identity.PublicUser{}, false, fmt.Errorf("auth: create session: %w", err)
	}

	return sessionTok, csrfTok, u.ToPublic(), u.MustChangePassword, nil
}

// Register creates a new user account with the default "user" role and
// "active" status, then signs them in automatically.
//
// Validation rules:
//   - username: 1–32 ASCII alphanumeric characters [a-zA-Z0-9] with single
//     inner spaces; whitespace is normalized first (trimmed, every run
//     collapsed to one space), so "  John   Doe " registers as "John Doe"
//   - email: basic shape validation (contains '@', non-empty local and domain)
//   - password: 8–72 bytes (bcrypt limit)
//
// Canonicalization: username and email are lowercased before storage. Since
// usernames are ASCII-only, no Unicode normalization is needed.
//
// Returns the session token, CSRF token, and public user identity.
//
// userAgent is the raw User-Agent header — classified into the session's
// bounded device label, never persisted itself.
func (s *Service) Register(ctx context.Context, username, email, password, userAgent string) (sessionToken, csrfToken string, user identity.PublicUser, err error) {
	username = strings.Join(strings.Fields(username), " ")
	if len(username) < 1 {
		return "", "", identity.PublicUser{}, &FieldError{Field: "username", Code: "minLength", Message: "username must be 1-32 characters"}
	}
	if len(username) > 32 {
		return "", "", identity.PublicUser{}, &FieldError{Field: "username", Code: "maxLength", Message: "username must be 1-32 characters"}
	}
	for _, ch := range username {
		if ch != ' ' && !isAlphanumeric(ch) {
			return "", "", identity.PublicUser{}, &FieldError{Field: "username", Code: "invalidFormat", Message: "username allows letters, digits, and single spaces"}
		}
	}

	email, fe := normalizeEmail(email)
	if fe != nil {
		return "", "", identity.PublicUser{}, fe
	}

	if fe := validateNewPassword("password", password); fe != nil {
		return "", "", identity.PublicUser{}, fe
	}

	usernameCanon := strings.ToLower(username)

	if _, err := store.UserByCanonical(ctx, s.db, usernameCanon); err == nil {
		return "", "", identity.PublicUser{}, ErrUsernameTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", "", identity.PublicUser{}, fmt.Errorf("auth: check username: %w", err)
	}
	// A legacy row differing only in whitespace is the SAME name: the lenient
	// lookup is the sign-in rule, so registration must not mint a
	// near-duplicate — not even when several legacy rows normalize alike.
	if _, err := store.UserByCanonicalLenient(ctx, s.db, usernameCanon); err == nil || store.IsAmbiguousUser(err) {
		return "", "", identity.PublicUser{}, ErrUsernameTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", "", identity.PublicUser{}, fmt.Errorf("auth: check username: %w", err)
	}
	if _, err := store.UserByEmail(ctx, s.db, email); err == nil {
		return "", "", identity.PublicUser{}, ErrEmailTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", "", identity.PublicUser{}, fmt.Errorf("auth: check email: %w", err)
	}
	// An email equal to a legacy username would shadow that username at
	// sign-in and recovery — both resolve through the lenient lookup, so the
	// shadow check must use it too. Several legacy rows normalizing alike
	// make the address ambiguous, and ambiguity is taken.
	if _, err := store.UserByCanonicalLenient(ctx, s.db, email); err == nil || store.IsAmbiguousUser(err) {
		return "", "", identity.PublicUser{}, ErrEmailTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", "", identity.PublicUser{}, fmt.Errorf("auth: check email against usernames: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return "", "", identity.PublicUser{}, fmt.Errorf("auth: hash password: %w", err)
	}

	// Generated before the store call: bcrypt has run, and nothing below may
	// hold a SQLite write transaction (bcrypt must never run inside one).
	m, err := generateSessionMaterial()
	if err != nil {
		return "", "", identity.PublicUser{}, err
	}

	userID, err := id.New()
	if err != nil {
		return "", "", identity.PublicUser{}, fmt.Errorf("auth: generate user id: %w", err)
	}

	// One transaction: a failed session insert rolls the account back, so a
	// failed registration can never leave a half-created user behind.
	err = store.CreateUserAndSession(ctx, s.db, store.CreateUserAndSessionParams{
		ID:            userID,
		Username:      username,
		UsernameCanon: usernameCanon,
		Email:         email,
		Password:      string(hash),
		Role:          identity.RoleUser,
		Status:        identity.StatusActive,
		AuthVersion:   1,
		CreatedAtMS:   m.nowMS,
		UpdatedAtMS:   m.nowMS,
		SessionID:     m.id,
		TokenDigest:   m.digest,
		CSRF:          m.rawCSRF,
		ClientLabel:   sessionClientLabel(userAgent),
		ExpiresAtMS:   m.nowMS + SessionLifetime.Milliseconds(),
	})
	if errors.Is(err, store.ErrDuplicate) {
		// ErrDuplicate here means either the username/email was claimed by a
		// concurrent registration (the common case) or, astronomically
		// unlikely, the new session's ID or token digest collided with an
		// existing row. Either way the whole registration rolled back; the
		// handler maps the sentinel to 409 Conflict, and a
		// retry regenerates the session material and succeeds.
		return "", "", identity.PublicUser{}, fmt.Errorf("%w: %w", ErrConflict, err)
	}
	if err != nil {
		return "", "", identity.PublicUser{}, fmt.Errorf("auth: create user and session: %w", err)
	}

	return m.encodedSession, m.encodedCSRF, identity.NewPublicUser(userID, username, identity.RoleUser), nil
}

// SignOut deletes a single session by its token digest and returns the
// number of rows deleted (0 or 1).
//
// It is idempotent — deleting a non-existent or already-revoked session
// is not an error, it simply deletes zero rows. The caller (handler) is
// responsible for computing the SHA-256 digest from the cookie value, and
// uses the count to distinguish "a session was actually revoked" from the
// idempotent no-session outcome (no audit event for the latter).
func (s *Service) SignOut(ctx context.Context, digest []byte) (int64, error) {
	n, err := store.DeleteSessionByDigest(ctx, s.db, digest)
	if err != nil {
		return 0, fmt.Errorf("auth: sign out: %w", err)
	}
	return n, nil
}

// SignOutAll revokes every session for a given user.
//
// One store transaction bumps the user's auth_version (authoritative
// revocation) and deletes all session rows (cleanup). A racing sign-in that
// commits before the bump is deleted; one that commits after survives under
// the new version — the "winner" contract. The
// caller (handler) is responsible for clearing the current request's session
// cookie.
func (s *Service) SignOutAll(ctx context.Context, userID string) error {
	now := time.Now().UnixMilli()
	if err := store.IncrementAuthVersionAndDeleteSessions(ctx, s.db, userID, now); err != nil {
		return fmt.Errorf("auth: sign out all: %w", err)
	}
	return nil
}

// lookupIdentifier resolves an identifier to a user. It returns (nil, nil)
// when no account matches; the SignIn resolution chain answers both surfaces.
func (s *Service) lookupIdentifier(ctx context.Context, identifier string) (*identity.User, error) {
	u, err := s.lookupUserByIdentifier(ctx, identifier)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return u, err
}

// lookupUserByIdentifier resolves a username-or-email identifier through the
// one shared chain: a lowercased '@'-containing value tries email first, then
// the canonical and whitespace-lenient username lookups (the fallback covers
// migrated usernames containing '@'); other identifiers try the two username
// lookups. A miss returns store.ErrNotFound so callers can branch on it;
// other failures are wrapped once here.
func (s *Service) lookupUserByIdentifier(ctx context.Context, identifier string) (*identity.User, error) {
	canon := strings.ToLower(identifier)

	var u *identity.User
	var err error
	if strings.Contains(canon, "@") {
		u, err = store.UserByEmail(ctx, s.db, canon)
		if errors.Is(err, store.ErrNotFound) {
			u, err = store.UserByCanonical(ctx, s.db, canon)
		}
		if errors.Is(err, store.ErrNotFound) {
			u, err = store.UserByCanonicalLenient(ctx, s.db, canon)
		}
	} else {
		u, err = store.UserByCanonical(ctx, s.db, canon)
		if errors.Is(err, store.ErrNotFound) {
			u, err = store.UserByCanonicalLenient(ctx, s.db, canon)
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("auth: lookup user: %w", err)
	}
	return u, nil
}
