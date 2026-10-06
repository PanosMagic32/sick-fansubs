// Package store provides SQLite persistence operations for the Sick-Fansubs
// application.
//
// Store functions own SQL queries, row scanning, and transaction management.
// They accept *sql.DB (the connection pool) and context.Context (for
// cancellation/deadlines). They return the identity package's values and sentinel
// errors for expected conditions.
//
// Stores do not own HTTP concerns, business rules, or password hashing.
// Those belong to the handler and auth layers respectively.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"sick-fansubs/internal/identity"
)

// UserByCanonical looks up a user by their canonicalized (lowercased)
// username. Returns ErrNotFound if no matching row exists.
func UserByCanonical(ctx context.Context, db *sql.DB, usernameCanon string) (*identity.User, error) {
	const q = `SELECT id, username, username_canon, email, password,
		role, status, auth_version, must_change_password, avatar_url, email_verified_at_ms, created_at_ms, updated_at_ms
	FROM users WHERE username_canon = ?`

	return scanUser(db.QueryRowContext(ctx, q, usernameCanon))
}

// UserRefByCanonical resolves one account's public content-byline projection
// (id, username, avatar reference). Returns ErrNotFound if no matching row
// exists.
func UserRefByCanonical(ctx context.Context, db *sql.DB, usernameCanon string) (*UserRef, error) {
	const q = `SELECT id, username, avatar_url FROM users WHERE username_canon = ?`

	var ref UserRef
	var avatar sql.NullString
	err := db.QueryRowContext(ctx, q, usernameCanon).Scan(&ref.ID, &ref.Username, &avatar)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: user ref lookup: %w", err)
	}
	ref.AvatarURL = nullStringPtr(avatar)
	return &ref, nil
}

// UserByCanonicalLenient resolves a canonical username while ignoring
// whitespace runs — the sign-in bridge for imported legacy spacing. An
// ambiguous match wraps ErrNotFound; IsAmbiguousUser reports it.
func UserByCanonicalLenient(ctx context.Context, db *sql.DB, usernameCanon string) (*identity.User, error) {
	// The SQL prefilter strips every Unicode whitespace rune from both sides —
	// a coarse superset — and the Go comparison below is the authority, so a
	// fetched row that only looks alike is discarded.
	q := `SELECT id, username, username_canon, email, password,
		role, status, auth_version, must_change_password, avatar_url, email_verified_at_ms, created_at_ms, updated_at_ms
	FROM users
	WHERE username_canon = ?
	   OR ` + stripWhitespaceSQL("username_canon") + ` = ?`

	norm := NormalizeCanonWhitespace(usernameCanon)
	stripped := stripWhitespace(strings.ToLower(usernameCanon))
	rows, err := db.QueryContext(ctx, q, usernameCanon, stripped)
	if err != nil {
		return nil, fmt.Errorf("store: lenient user lookup: %w", err)
	}
	defer rows.Close()

	// The whole set is scanned before deciding: an exact row wins wherever it
	// arrives in the cursor, and only a LONE normalized match answers.
	var exact, unique *identity.User
	normalized := 0
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		if u.UsernameCanon == usernameCanon {
			if exact == nil {
				exact = u
			}
			continue
		}
		if NormalizeCanonWhitespace(u.UsernameCanon) == norm {
			normalized++
			unique = u
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: lenient user lookup rows: %w", err)
	}
	if exact != nil {
		return exact, nil
	}
	if normalized == 1 {
		return unique, nil
	}
	if normalized > 1 {
		return nil, fmt.Errorf("%w: %w", ErrNotFound, errAmbiguousUser)
	}
	return nil, ErrNotFound
}

// NormalizeCanonWhitespace is the shared whitespace/case fold of a username
// identifier: lowercased, ends trimmed, every inner run collapsed to one
// space. The identifier-keyed limiters key on it too, so spelling variants
// of one account share a bucket.
func NormalizeCanonWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// whitespaceRunes is the set strings.Fields splits on (unicode.IsSpace), so
// the SQL prefilter and the Go fold agree on what a whitespace run is.
var whitespaceRunes = []rune{
	'\t', '\n', '\v', '\f', '\r', ' ',
	0x85, 0xA0, 0x1680,
	0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005,
	0x2006, 0x2007, 0x2008, 0x2009, 0x200A, 0x2028, 0x2029,
	0x202F, 0x205F, 0x3000,
}

// stripWhitespaceSQL builds the nested replace() chain removing those runes
// from a column, so the prefilter cannot miss a candidate the Go fold would
// match. char(N) spells each rune without embedding it in the statement.
func stripWhitespaceSQL(column string) string {
	expr := column
	for _, r := range whitespaceRunes {
		expr = fmt.Sprintf("replace(%s, char(%d), '')", expr, r)
	}
	return expr
}

// stripWhitespace removes every whitespace rune — the prefilter's Go twin.
func stripWhitespace(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// UserByEmail looks up a user by their lowercased email address.
// Returns ErrNotFound if no matching row exists.
func UserByEmail(ctx context.Context, db *sql.DB, email string) (*identity.User, error) {
	const q = `SELECT id, username, username_canon, email, password,
		role, status, auth_version, must_change_password, avatar_url, email_verified_at_ms, created_at_ms, updated_at_ms
	FROM users WHERE email = ?`

	return scanUser(db.QueryRowContext(ctx, q, email))
}

// UserByID looks up a user by their opaque ID.
// Returns ErrNotFound if no matching row exists.
func UserByID(ctx context.Context, db *sql.DB, id string) (*identity.User, error) {
	const q = `SELECT id, username, username_canon, email, password,
			role, status, auth_version, must_change_password, avatar_url, email_verified_at_ms, created_at_ms, updated_at_ms
		FROM users WHERE id = ?`

	return scanUser(db.QueryRowContext(ctx, q, id))
}

// UpdateUserAvatar sets the user's avatar reference and bumps
// updated_at_ms. The avatar change is NOT a security-state change —
// auth_version is deliberately untouched (password change, logout-all,
// suspension, reactivation, and explicit resets are the revocation events;
// a picture does not revoke sessions). The old media file stays on disk;
// the orphan sweep reclaims it after the grace window.
//
// Returns ErrNotFound if the user does not exist.
func UpdateUserAvatar(ctx context.Context, db *sql.DB, userID, avatarURL string, updatedAtMS int64) error {
	const q = `UPDATE users SET avatar_url = ?, updated_at_ms = ? WHERE id = ?`

	res, err := db.ExecContext(ctx, q, nullableString(avatarURL), updatedAtMS, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ChangeUserEmail atomically swaps the user's email, resets the
// verification stamp, and deletes the user's token rows in one transaction:
//
//  1. UPDATE users SET email = ?, email_verified_at_ms = NULL,
//     updated_at_ms = ? WHERE id = ? AND auth_version = ? — the CAS guard
//     closes the race against a concurrent password change/admin reset
//     (the auth_version CAS model). The stamp reset is the rule: a new
//     address is unverified until its inbox proves control.
//  2. DELETE both token tables for the user: an old-address reset link
//     sitting in a lost mailbox must not control the account, and an old
//     verification link must not verify the wrong address.
//
// Existing sessions survive: an email change is not a revocation event —
// the caller has re-authenticated with the current password — so this
// operation neither bumps auth_version nor deletes sessions. The CAS guard
// is race protection, not revocation.
//
// Returns ErrNotFound if the user does not exist, ErrAuthVersionMismatch
// if the version no longer matches (the same disambiguation shape as the
// other guarded writes), and ErrDuplicate when the new address is claimed
// by another row (the UNIQUE backstop behind the service's advisory
// pre-check — concurrent changes/registrations answer 422, never a raw
// constraint 500).
func ChangeUserEmail(ctx context.Context, db *sql.DB, userID, email string, currentAuthVersion int64, updatedAtMS int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin email-change tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET email = ?, email_verified_at_ms = NULL, updated_at_ms = ?
		 WHERE id = ? AND auth_version = ?`,
		email, updatedAtMS, userID, currentAuthVersion,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			// The advisory pre-check lost the race (a concurrent change or
			// registration claimed the address) — map to the duplicate
			// sentinel, the service answers 422 alreadyTaken. The DB
			// uniqueness is the authority; the pre-check is only fast-path.
			return ErrDuplicate
		}
		return fmt.Errorf("store: change user email: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: change user email rows affected: %w", err)
	}
	if n == 0 {
		// Not found or version changed — disambiguate outside the
		// transaction (the same disambiguation shape as the other guarded
		// writes).
		_, lookupErr := UserByID(ctx, db, userID)
		if errors.Is(lookupErr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrAuthVersionMismatch
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verification_tokens WHERE user_id = ?`, userID,
	); err != nil {
		return fmt.Errorf("store: change email delete verification tokens: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM password_reset_tokens WHERE user_id = ?`, userID,
	); err != nil {
		return fmt.Errorf("store: change email delete reset tokens: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit email-change tx: %w", err)
	}
	return nil
}

func scanUser(row *sql.Row) (*identity.User, error) {
	return scanUserRow(row)
}

// rowScanner is the Scan surface shared by *sql.Row and *sql.Rows — the
// lenient lookup scans a row set through the same mapping as the one-shot
// readers.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanUserRow(row rowScanner) (*identity.User, error) {
	var u identity.User
	var avatarURL sql.NullString
	var emailVerified sql.NullInt64

	err := row.Scan(
		&u.ID, &u.Username, &u.UsernameCanon, &u.Email, &u.Password,
		&u.Role, &u.Status, &u.AuthVersion, &u.MustChangePassword, &avatarURL,
		&emailVerified, &u.CreatedAtMS, &u.UpdatedAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan user: %w", err)
	}

	u.AvatarURL = nullStringPtr(avatarURL)
	if emailVerified.Valid {
		v := emailVerified.Int64
		u.EmailVerifiedAtMS = &v
	}

	return &u, nil
}

// IsUniqueViolation returns true if err is a SQLite UNIQUE constraint failure.
//
// modernc.org/sqlite surfaces the SQLite error text directly, so "UNIQUE
// constraint failed" is the discriminator database/sql offers (it has no
// portable constraint-violation sentinel). The match is pinned by a canary
// test that triggers a real UNIQUE violation through the driver and fails if
// the message format changes (session_store_test.go: TestIsUniqueViolation_Canary).
// Exported for migration tooling, which needs the same discriminator for its
// tool-owned INSERTs (migration.importUserOne).
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
