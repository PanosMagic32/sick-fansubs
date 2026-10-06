package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sick-fansubs/internal/identity"
)

// CreateSessionParams holds the values needed to insert a session row.
//
// AuthVersion must match the user's current auth_version at insert time.
// The insert atomically checks this via a subquery; if the version changed
// or the user is suspended, zero rows are inserted and the function returns
// ErrAuthVersionMismatch.
//
// ClientLabel is the bounded classifier label from internal/auth. The empty
// string stores SQL NULL — "unknown device".
type CreateSessionParams struct {
	ID          string
	UserID      string
	TokenDigest []byte // SHA-256 of the raw session token (exactly 32 bytes)
	CSRF        []byte // raw 32-byte CSRF token
	AuthVersion int64  // must match current users.auth_version
	ClientLabel string // empty stores NULL
	CreatedAtMS int64
	ExpiresAtMS int64
}

// CreateSession inserts a session row atomically.
//
// The INSERT ... SELECT pattern with a WHERE clause on the users table
// ensures the session is only created if the user is active and the
// auth_version matches. If the condition fails, zero rows are inserted.
//
// The session's auth_version is stamped from the creation-time user
// value so that SessionByDigest can later enforce equality between
// s.auth_version and u.auth_version, automatically invalidating the
// session when the user's auth_version changes.
//
// Returns:
//   - ErrAuthVersionMismatch if the user's status or auth_version changed.
//   - ErrNotFound if the user does not exist.
//   - ErrDuplicate if a session with the same token digest already exists
//     (astronomically unlikely with SHA-256, but handled for correctness).
func CreateSession(ctx context.Context, db *sql.DB, p CreateSessionParams) error {
	const q = `INSERT INTO sessions (id, user_id, token_digest, csrf_token, auth_version, client_label, created_at_ms, expires_at_ms)
	SELECT ?, ?, ?, ?, ?, ?, ?, ?
	WHERE EXISTS (
		SELECT 1 FROM users
		WHERE id = ? AND auth_version = ? AND status = 'active'
	)`

	res, err := db.ExecContext(ctx, q,
		p.ID, p.UserID, p.TokenDigest, p.CSRF, p.AuthVersion, nullableString(p.ClientLabel), p.CreatedAtMS, p.ExpiresAtMS,
		p.UserID, p.AuthVersion,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("store: create session: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: create session rows affected: %w", err)
	}
	if n == 0 {
		// Could be not found, version mismatch, or suspended.
		// ErrAuthVersionMismatch covers all three because the service layer
		// maps all of them to the same public outcome (generic 401).
		_, lookupErr := UserByID(ctx, db, p.UserID)
		if errors.Is(lookupErr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrAuthVersionMismatch
	}

	return nil
}

// SessionByDigest looks up a session by its token digest and joins the
// owning user's identity and authorization fields.
//
// The query enforces s.auth_version = u.auth_version AND
// u.status = 'active', so sessions become invalid as soon as the user's
// auth_version is bumped (password change, logout-all, suspension) or the
// account is suspended directly. No service-layer discipline is needed —
// the database is the authoritative revocation source.
//
// It returns the session even if expired — the caller checks expiry
// with SessionUser.IsExpired().
//
// Returns ErrNotFound if no session matches the digest, if the session's
// auth_version no longer matches the user's, or if the account is not active.
func SessionByDigest(ctx context.Context, db *sql.DB, digest []byte) (*identity.SessionUser, error) {
	const q = `SELECT s.id, s.user_id, s.csrf_token, s.expires_at_ms,
		u.username, u.role, u.must_change_password
	FROM sessions s
	JOIN users u ON s.user_id = u.id
	WHERE s.token_digest = ? AND s.auth_version = u.auth_version
		AND u.status = 'active'`

	var su identity.SessionUser
	err := db.QueryRowContext(ctx, q, digest).Scan(
		&su.SessionID, &su.UserID, &su.CSRF, &su.ExpiresAtMS,
		&su.Username, &su.Role, &su.MustChangePassword,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: session by digest: %w", err)
	}

	return &su, nil
}

// DeleteSessionByDigest removes a single session row by token digest.
//
// It is idempotent: deleting a non-existent session is not an error.
// Returns the number of rows deleted (0 or 1).
func DeleteSessionByDigest(ctx context.Context, db *sql.DB, digest []byte) (int64, error) {
	const q = `DELETE FROM sessions WHERE token_digest = ?`

	res, err := db.ExecContext(ctx, q, digest)
	if err != nil {
		return 0, fmt.Errorf("store: delete session by digest: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete session by digest rows affected: %w", err)
	}

	return n, nil
}

// RehashPasswordAndCreateSessionParams holds the values needed to rehash a
// user's password verifier and create a session row atomically.
//
// CurrentAuthVersion must match the user's auth_version at call time: the
// conditional UPDATE bumps it to CurrentAuthVersion+1 and the session row is
// stamped with the NEW version.
type RehashPasswordAndCreateSessionParams struct {
	UserID             string
	CurrentAuthVersion int64
	NewHash            string
	UpdatedAtMS        int64

	SessionID   string
	TokenDigest []byte // SHA-256 of the raw session token (exactly 32 bytes)
	CSRF        []byte // raw 32-byte CSRF token
	ClientLabel string // empty stores NULL
	CreatedAtMS int64
	ExpiresAtMS int64
}

// RehashPasswordAndCreateSession atomically replaces the user's password
// verifier (bumping auth_version) and creates a session under the NEW version.
//
// These two writes must be one transaction:
// the rehash bumps auth_version, and the session insert must reference the
// bumped version. If the rehash and the insert were separate operations, the
// session would either be created against the stale version (INSERT…SELECT
// matches zero rows → the sign-in fails right after a successful rehash) or
// be invalidated by a bump that lands after its insert. One transaction
// removes the whole ordering problem.
//
// Transaction:
//  1. UPDATE users SET password = NewHash, auth_version = auth_version + 1
//     WHERE id = UserID AND auth_version = CurrentAuthVersion. Zero rows mean
//     security state changed concurrently (or the user is gone) — nothing is
//     written and the caller maps the error to a generic sign-in failure.
//  2. INSERT the session row with the same conditional INSERT…SELECT guard as
//     CreateSession, stamped with CurrentAuthVersion+1 (still rejects a
//     suspension that races in between).
//
// Returns:
//   - ErrAuthVersionMismatch if the version changed or the user was suspended.
//   - ErrNotFound if the user does not exist.
//   - ErrDuplicate if the token digest already exists.
func RehashPasswordAndCreateSession(ctx context.Context, db *sql.DB, p RehashPasswordAndCreateSessionParams) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin rehash+session tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Conditional verifier update + auth_version bump.
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET password = ?, auth_version = auth_version + 1, updated_at_ms = ?
		 WHERE id = ? AND auth_version = ?`,
		p.NewHash, p.UpdatedAtMS, p.UserID, p.CurrentAuthVersion,
	)
	if err != nil {
		return fmt.Errorf("store: rehash update user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rehash update rows affected: %w", err)
	}
	if n == 0 {
		// Disambiguate "user not found" from "version changed". The lookup
		// is outside the transaction — a benign TOCTOU window (the same
		// disambiguation shape as the other guarded writes) that cannot
		// produce a wrong outcome.
		_, lookupErr := UserByID(ctx, db, p.UserID)
		if errors.Is(lookupErr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrAuthVersionMismatch
	}

	// 2. Conditional session insert under the NEW version.
	const q = `INSERT INTO sessions (id, user_id, token_digest, csrf_token, auth_version, client_label, created_at_ms, expires_at_ms)
	SELECT ?, ?, ?, ?, ?, ?, ?, ?
	WHERE EXISTS (
		SELECT 1 FROM users
		WHERE id = ? AND auth_version = ? AND status = 'active'
	)`

	res, err = tx.ExecContext(ctx, q,
		p.SessionID, p.UserID, p.TokenDigest, p.CSRF, p.CurrentAuthVersion+1, nullableString(p.ClientLabel), p.CreatedAtMS, p.ExpiresAtMS,
		p.UserID, p.CurrentAuthVersion+1,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("store: rehash+create session: %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rehash+create session rows affected: %w", err)
	}
	if n == 0 {
		// The version we bumped to no longer matches or the user is
		// suspended — either way the sign-in must not produce a session.
		return ErrAuthVersionMismatch
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: rehash+session commit: %w", err)
	}
	return nil
}

// CreateUserAndSessionParams holds the values needed to create a user
// account and its first session atomically (registration auto sign-in).
//
// CreatedAtMS stamps both the user row and the session row — they come
// from one clock sample in the service so the account and its first
// session share a creation instant. UpdatedAtMS stamps the user row's
// updated_at_ms.
type CreateUserAndSessionParams struct {
	ID            string
	Username      string
	UsernameCanon string
	Email         string
	Password      string // bcrypt verifier string
	Role          string
	Status        string
	AuthVersion   int64 // initial auth_version (registration uses 1)

	CreatedAtMS int64 // user.created_at_ms AND sessions.created_at_ms
	UpdatedAtMS int64 // user.updated_at_ms

	SessionID   string
	TokenDigest []byte // SHA-256 of the raw session token (exactly 32 bytes)
	CSRF        []byte // raw 32-byte CSRF token
	ClientLabel string // empty stores NULL
	ExpiresAtMS int64
}

// CreateUserAndSession inserts a new user row and the account's first
// session row in ONE transaction.
//
// Registration is atomic: the user insert and the session insert commit
// together or not at all. A session failure after a committed user insert
// would leave a live account behind while the client saw 500 — a retry
// would then hit "username taken" and the name would be lost to the user.
//
// The session insert keeps the same conditional INSERT…SELECT guard as
// CreateSession (auth_version match + status = 'active'), so every
// session-creation path enforces the same revocation invariant. Inside
// this transaction the just-inserted user row is visible to the guard:
// the auth_version clause can never diverge (insert and guard share
// p.AuthVersion) — it is kept so the guard reads identically across
// session paths — and only a non-active Status can reject the insert.
//
// Transaction:
//  1. INSERT the user row.
//  2. INSERT the session row under AuthVersion with the conditional guard.
//
// Returns:
//   - ErrDuplicate if the username_canon, email, session ID, or token
//     digest is taken — the caller maps this to 409 Conflict. The
//     username/email case is the registration race; the session
//     collisions are ~2⁻¹²⁸ and a retry regenerates the values.
//   - ErrAuthVersionMismatch if the session guard rejected the just-created
//     user (non-active Status) — the user insert rolls back with it.
func CreateUserAndSession(ctx context.Context, db *sql.DB, p CreateUserAndSessionParams) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin register tx: %w", err)
	}
	defer tx.Rollback()

	// 1. User row.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO users (id, username, username_canon, email, password,
			role, status, auth_version, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Username, p.UsernameCanon, p.Email, p.Password,
		p.Role, p.Status, p.AuthVersion, p.CreatedAtMS, p.UpdatedAtMS,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("store: create user (register): %w", err)
	}

	// 2. Session row under the account's initial auth_version.
	const q = `INSERT INTO sessions (id, user_id, token_digest, csrf_token, auth_version, client_label, created_at_ms, expires_at_ms)
	SELECT ?, ?, ?, ?, ?, ?, ?, ?
	WHERE EXISTS (
		SELECT 1 FROM users
		WHERE id = ? AND auth_version = ? AND status = 'active'
	)`

	res, err := tx.ExecContext(ctx, q,
		p.SessionID, p.ID, p.TokenDigest, p.CSRF, p.AuthVersion, nullableString(p.ClientLabel), p.CreatedAtMS, p.ExpiresAtMS,
		p.ID, p.AuthVersion,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("store: create session (register): %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: create session (register) rows affected: %w", err)
	}
	if n == 0 {
		// The just-inserted user failed the guard: the caller passed a
		// non-active Status. Roll back — an account that cannot hold its
		// first session must not be created.
		return ErrAuthVersionMismatch
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: register commit: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes up to limit session rows whose expiry time
// has already passed. Returns the number of rows deleted.
//
// The limit keeps cleanup bounded: a large backlog is deleted in batches so it
// cannot hold the SQLite writer for an unbounded delete. SQLite's DELETE has no
// LIMIT clause (https://www.sqlite.org/lang_delete.html), so the batch is
// expressed as a subquery with LIMIT.
func DeleteExpiredSessions(ctx context.Context, db *sql.DB, nowMS int64, limit int) (int64, error) {
	const q = `DELETE FROM sessions WHERE id IN (
		SELECT id FROM sessions WHERE expires_at_ms <= ? LIMIT ?
	)`

	res, err := db.ExecContext(ctx, q, nowMS, limit)
	if err != nil {
		return 0, fmt.Errorf("store: delete expired sessions: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete expired sessions rows affected: %w", err)
	}

	return n, nil
}

// RevokeSession deletes one session by id, scoped to its owner.
//
// The user_id predicate is the authorization boundary: another user's session
// id matches no row here, so a foreign id can never revoke anything and its
// outcome is indistinguishable from an unknown or already-deleted one.
// The count reports whether a row was actually removed;
// the handler answers the same idempotent 204 either way.
func RevokeSession(ctx context.Context, db *sql.DB, userID, sessionID string) (int64, error) {
	const q = `DELETE FROM sessions WHERE id = ? AND user_id = ?`

	res, err := db.ExecContext(ctx, q, sessionID, userID)
	if err != nil {
		return 0, fmt.Errorf("store: revoke session: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: revoke session rows affected: %w", err)
	}

	return n, nil
}
