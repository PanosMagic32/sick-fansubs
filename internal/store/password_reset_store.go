package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CreateResetTokenParams holds the values needed to mint one reset token.
type CreateResetTokenParams struct {
	TokenDigest []byte // SHA-256 of the raw 32-byte token (exactly 32 bytes)
	UserID      string
	CreatedAtMS int64
	ExpiresAtMS int64
}

// CreateResetToken replaces the user's active token set with ONE new token
// in a single transaction: delete every existing row for the user, then
// insert the new one (one active token per user; the
// latest request wins, so the newest email carries the only valid link).
//
// The UNIQUE(user_id) constraint makes the one-active invariant a schema
// guarantee, not just store discipline.
//
// Returns ErrDuplicate if the token digest already exists (astronomically
// unlikely with SHA-256; a retry regenerates the token). A user deleted
// between the caller's lookup and this write surfaces as the raw FK error
// — the service maps any failure here to the generic 500 path.
func CreateResetToken(ctx context.Context, db *sql.DB, p CreateResetTokenParams) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin reset-token tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM password_reset_tokens WHERE user_id = ?`, p.UserID,
	); err != nil {
		return fmt.Errorf("store: delete existing reset tokens: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (token_digest, user_id, created_at_ms, expires_at_ms)
		VALUES (?, ?, ?, ?)`,
		p.TokenDigest, p.UserID, p.CreatedAtMS, p.ExpiresAtMS,
	); err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("store: insert reset token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit reset-token tx: %w", err)
	}
	return nil
}

// ResetTokenUserID returns the owning user of an unexpired reset token.
// The service uses it to load the user (auth_version CAS input + role for
// the audit snapshot) BEFORE hashing the new password; the consume
// transaction re-validates the token, so this read is advisory (the
// standard repo TOCTOU pattern).
//
// Returns ErrResetTokenInvalid if no unexpired token matches the digest.
func ResetTokenUserID(ctx context.Context, db *sql.DB, tokenDigest []byte, nowMS int64) (string, error) {
	var userID string
	err := db.QueryRowContext(ctx,
		`SELECT user_id FROM password_reset_tokens
		 WHERE token_digest = ? AND expires_at_ms > ?`,
		tokenDigest, nowMS,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrResetTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("store: lookup reset token user: %w", err)
	}
	return userID, nil
}

// ResetPasswordWithTokenParams holds the values needed to consume a reset
// token and replace the password atomically.
type ResetPasswordWithTokenParams struct {
	TokenDigest        []byte // SHA-256 of the submitted raw token
	NowMS              int64  // expiry check + timestamps
	CurrentAuthVersion int64  // CAS guard (the UpdatePasswordAndRevokeSessions model)
	NewHash            string // bcrypt verifier
	UpdatedAtMS        int64
}

// ResetPasswordWithToken consumes a reset token and swaps the password in
// ONE transaction:
//
//  1. SELECT the token row (digest + unexpired). No row → ErrResetTokenInvalid.
//  2. UPDATE users: verifier swap, must_change_password cleared (a
//     self-chosen password exits the forced gate), auth_version
//     bump, CAS-guarded on the caller-supplied version AND status =
//     'active'. The CAS is the single-use gate under concurrency: the
//     immediate transaction serializes writers, so of two racing resets
//     exactly one UPDATE matches the pre-reset version
//     (docs/patterns/go/sqlite.md rule 6: purpose-built conditional SQL and
//     affected-row checks).
//     3a. On the success path: DELETE the token row (RowsAffected must be 1 —
//     a consistency assert on top of the CAS), delete all session rows
//     (cleanup; the bump is the authoritative revocation), COMMIT.
//     3b. On the rejection path (suspended account or version mismatch):
//     DELETE this token's row and COMMIT that deletion alone — a
//     rejection still KILLS the link (suspension invalidates in-flight
//     links immediately), while the verifier and version are untouched.
//     No partial password change can survive: the
//     UPDATE either applied (3a) or matched zero rows (3b).
//
// bcrypt runs in the service BEFORE this call — never inside the write
// transaction.
//
// Returns ErrResetTokenInvalid for a missing/expired token or a
// suspended/mismatched account (one generic outcome).
func ResetPasswordWithToken(ctx context.Context, db *sql.DB, p ResetPasswordWithTokenParams) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin reset-consume tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Token row.
	var userID string
	err = tx.QueryRowContext(ctx,
		`SELECT user_id FROM password_reset_tokens
		 WHERE token_digest = ? AND expires_at_ms > ?`,
		p.TokenDigest, p.NowMS,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrResetTokenInvalid
	}
	if err != nil {
		return fmt.Errorf("store: lookup reset token: %w", err)
	}

	// 2. Conditional verifier swap + revocation + forced-flag clear + the
	// verification stamp (a completed self-service reset proves
	// inbox control — stamp email_verified_at_ms when it is still NULL,
	// in the same transaction as the password write).
	res, err := tx.ExecContext(ctx,
		`UPDATE users
		 SET password = ?, must_change_password = 0, auth_version = auth_version + 1,
		     email_verified_at_ms = COALESCE(email_verified_at_ms, ?), updated_at_ms = ?
		 WHERE id = ? AND auth_version = ? AND status = 'active'`,
		p.NewHash, p.UpdatedAtMS, p.UpdatedAtMS, userID, p.CurrentAuthVersion,
	)
	if err != nil {
		return fmt.Errorf("store: reset update user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: reset update rows affected: %w", err)
	}
	if n == 0 {
		// Suspended, version-changed, or gone. The link must die either
		// way — commit the token deletion alone; the
		// verifier and auth_version are provably untouched (0 rows). The
		// delete is digest-scoped: a fresh token minted by a newer
		// forgot-password request between our read and this transaction
		// must NOT die with our stale link.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM password_reset_tokens WHERE token_digest = ?`, p.TokenDigest,
		); err != nil {
			return fmt.Errorf("store: reset kill token: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit reset-kill tx: %w", err)
		}
		return ErrResetTokenInvalid
	}

	// 3a. Conditional consume (the consistency assert) + session cleanup.
	res, err = tx.ExecContext(ctx,
		`DELETE FROM password_reset_tokens
		 WHERE token_digest = ? AND expires_at_ms > ?`,
		p.TokenDigest, p.NowMS,
	)
	if err != nil {
		return fmt.Errorf("store: consume reset token: %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: consume reset token rows affected: %w", err)
	}
	if n != 1 {
		// A concurrent consumer slipped past the CAS — abort without a
		// partial state change (the UPDATE above rolls back with the tx).
		return ErrResetTokenInvalid
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ?`, userID,
	); err != nil {
		return fmt.Errorf("store: reset delete sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit reset-consume tx: %w", err)
	}
	return nil
}

// DeleteExpiredResetTokens removes up to limit token rows whose expiry has
// passed. The batch shape mirrors DeleteExpiredSessions: SQLite DELETE has
// no LIMIT clause, so the batch is a subquery (bounded-cleanup
// discipline; the sweep runs at the start of each
// forgot-password request, so a tiny table keeps it trivial).
func DeleteExpiredResetTokens(ctx context.Context, db *sql.DB, nowMS int64, limit int) (int64, error) {
	const q = `DELETE FROM password_reset_tokens WHERE token_digest IN (
		SELECT token_digest FROM password_reset_tokens WHERE expires_at_ms <= ? LIMIT ?
	)`

	res, err := db.ExecContext(ctx, q, nowMS, limit)
	if err != nil {
		return 0, fmt.Errorf("store: delete expired reset tokens: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete expired reset tokens rows affected: %w", err)
	}

	return n, nil
}
