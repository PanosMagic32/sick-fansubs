package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sick-fansubs/internal/identity"
)

// CreateVerificationTokenParams holds the values needed to mint one
// verification token.
type CreateVerificationTokenParams struct {
	TokenDigest []byte // SHA-256 of the raw 32-byte token (exactly 32 bytes)
	UserID      string
	CreatedAtMS int64
	ExpiresAtMS int64
}

// CreateVerificationToken replaces the user's active token set with ONE
// new token in a single transaction: delete every existing row for the
// user, then insert the new one (one active token per user; the latest mint
// wins, so the newest email carries the only valid link).
//
// Returns ErrDuplicate if the token digest already exists (astronomically
// unlikely with SHA-256; the service wraps it — the caller logs and the
// resend surface is the recovery path). A user deleted
// between the caller's lookup and this write surfaces as the raw FK error
// — the caller logs it and the resend surface is the recovery path.
func CreateVerificationToken(ctx context.Context, db *sql.DB, p CreateVerificationTokenParams) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin verification-token tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verification_tokens WHERE user_id = ?`, p.UserID,
	); err != nil {
		return fmt.Errorf("store: delete existing verification tokens: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO email_verification_tokens (token_digest, user_id, created_at_ms, expires_at_ms)
		VALUES (?, ?, ?, ?)`,
		p.TokenDigest, p.UserID, p.CreatedAtMS, p.ExpiresAtMS,
	); err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		}
		return fmt.Errorf("store: insert verification token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit verification-token tx: %w", err)
	}
	return nil
}

// VerificationTokenUserID returns the owning user of an unexpired
// verification token. The service uses it to load the user (role for the
// audit snapshot) BEFORE the consume transaction, which re-validates the
// token — this read is advisory (the standard repo TOCTOU pattern).
//
// Returns ErrVerificationTokenInvalid if no unexpired token matches the
// digest.
func VerificationTokenUserID(ctx context.Context, db *sql.DB, tokenDigest []byte, nowMS int64) (string, error) {
	var userID string
	err := db.QueryRowContext(ctx,
		`SELECT user_id FROM email_verification_tokens
		 WHERE token_digest = ? AND expires_at_ms > ?`,
		tokenDigest, nowMS,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrVerificationTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("store: lookup verification token user: %w", err)
	}
	return userID, nil
}

// VerifyEmailParams holds the values needed to consume a verification
// token and stamp the account verified.
type VerifyEmailParams struct {
	TokenDigest []byte // SHA-256 of the submitted raw token
	NowMS       int64  // expiry check + the stamp value
}

// VerifyEmail consumes a verification token and stamps
// email_verified_at_ms in ONE transaction:
//
//  1. SELECT the token row (digest + unexpired). No row →
//     ErrVerificationTokenInvalid.
//  2. Conditional stamp: UPDATE users SET email_verified_at_ms = NowMS
//     WHERE id = ? AND status = 'active' AND email_verified_at_ms IS NULL.
//     The status gate kills links for suspended accounts; the NULL gate
//     makes the verify idempotent for an already-verified user (their
//     reset stamped it while the token stayed valid).
//     3a. Stamp applied: DELETE the token row (RowsAffected must be 1 —
//     the single-use gate; a concurrent consumer makes it 0 and the whole
//     transaction aborts with ErrVerificationTokenInvalid — the
//     immediate transaction serializes writers, so of two racing verifies
//     exactly one wins).
//     3b. Zero rows: disambiguate. Gone or suspended → kill the token
//     (digest-scoped: a fresher mint must not die with our stale link),
//     commit that deletion alone, ErrVerificationTokenInvalid. Already
//     verified (stamp non-NULL) → consume the token and commit — success,
//     idempotent. Anything else (a racing consumer's token) → the
//     consumption gate answers invalid.
//
// Returns ErrVerificationTokenInvalid for a missing/expired token or a
// suspended/gone account (one generic outcome).
func VerifyEmail(ctx context.Context, db *sql.DB, p VerifyEmailParams) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin verification-consume tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Token row.
	var userID string
	err = tx.QueryRowContext(ctx,
		`SELECT user_id FROM email_verification_tokens
		 WHERE token_digest = ? AND expires_at_ms > ?`,
		p.TokenDigest, p.NowMS,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrVerificationTokenInvalid
	}
	if err != nil {
		return fmt.Errorf("store: lookup verification token: %w", err)
	}

	// 2. Conditional stamp.
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET email_verified_at_ms = ?
		 WHERE id = ? AND status = 'active' AND email_verified_at_ms IS NULL`,
		p.NowMS, userID,
	)
	if err != nil {
		return fmt.Errorf("store: verification stamp update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: verification stamp rows affected: %w", err)
	}

	// 3a. Success path: conditional consume (the single-use gate).
	if n == 1 {
		res, err = tx.ExecContext(ctx,
			`DELETE FROM email_verification_tokens
			 WHERE token_digest = ? AND expires_at_ms > ?`,
			p.TokenDigest, p.NowMS,
		)
		if err != nil {
			return fmt.Errorf("store: consume verification token: %w", err)
		}
		n, err = res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: consume verification token rows affected: %w", err)
		}
		if n != 1 {
			// A concurrent consumer slipped in — abort without a partial
			// state change (the stamp above rolls back with the tx).
			return ErrVerificationTokenInvalid
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit verification-consume tx: %w", err)
		}
		return nil
	}

	// 3b. Disambiguate the zero-row stamp.
	var status string
	var verified sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT status, email_verified_at_ms FROM users WHERE id = ?`, userID,
	).Scan(&status, &verified)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrVerificationTokenInvalid
		}
		return fmt.Errorf("store: verification disambiguation: %w", err)
	}

	if status != identity.StatusActive || !verified.Valid {
		// Suspended or gone (the NULL+active-but-no-stamp case cannot
		// happen under the immediate transaction: the UPDATE matched when
		// the row was active+NULL). The link dies either way — commit the
		// digest-scoped deletion alone.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM email_verification_tokens WHERE token_digest = ?`, p.TokenDigest,
		); err != nil {
			return fmt.Errorf("store: kill verification token: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit verification-kill tx: %w", err)
		}
		return ErrVerificationTokenInvalid
	}

	// Already verified — consume the token and succeed (idempotent).
	res, err = tx.ExecContext(ctx,
		`DELETE FROM email_verification_tokens
		 WHERE token_digest = ? AND expires_at_ms > ?`,
		p.TokenDigest, p.NowMS,
	)
	if err != nil {
		return fmt.Errorf("store: consume stale verification token: %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: consume stale verification token rows affected: %w", err)
	}
	if n != 1 {
		// A concurrent consumer owns this token — it won. We answer the
		// generic invalid outcome.
		return ErrVerificationTokenInvalid
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit stale-consume tx: %w", err)
	}
	return nil
}

// DeleteExpiredVerificationTokens removes up to limit token rows whose
// expiry has passed (the DeleteExpiredResetTokens batch shape — a
// request-time sweep on the mint paths; one-active-per-user already
// bounds the table).
func DeleteExpiredVerificationTokens(ctx context.Context, db *sql.DB, nowMS int64, limit int) (int64, error) {
	const q = `DELETE FROM email_verification_tokens WHERE token_digest IN (
		SELECT token_digest FROM email_verification_tokens WHERE expires_at_ms <= ? LIMIT ?
	)`

	res, err := db.ExecContext(ctx, q, nowMS, limit)
	if err != nil {
		return 0, fmt.Errorf("store: delete expired verification tokens: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete expired verification tokens rows affected: %w", err)
	}

	return n, nil
}
