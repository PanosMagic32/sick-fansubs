package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// UpdatePasswordAndRevokeSessions atomically updates the password hash,
// increments auth_version, deletes all sessions for the user, and removes
// any active reset tokens in a single transaction. Used by password change;
// the token deletion follows the rule that ANY password change kills
// in-flight reset links (the new password wins).
//
// Returns ErrAuthVersionMismatch if the version no longer matches.
// Returns ErrNotFound if the user does not exist.
func UpdatePasswordAndRevokeSessions(ctx context.Context, db *sql.DB, userID string, newHash string, currentAuthVersion int64, updatedAtMS int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Update password + bump auth_version, clearing the forced-change flag.
	// The flag is cleared unconditionally: a normal password change on an
	// un-flagged account is a no-op on it, and a forced change (PUT
	// /api/v1/auth/password is the way out of the gate) must
	// clear it in the SAME transaction as the verifier swap.
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET password = ?, must_change_password = 0, auth_version = auth_version + 1, updated_at_ms = ?
		 WHERE id = ? AND auth_version = ?`,
		newHash, updatedAtMS, userID, currentAuthVersion,
	)
	if err != nil {
		return fmt.Errorf("store: update password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rows affected: %w", err)
	}
	if n == 0 {
		// Disambiguate "user not found" from "auth version changed."
		// This lookup is outside the transaction, creating a benign TOCTOU
		// window: the user could be deleted or have auth_version change again
		// between the UPDATE and this SELECT. The window cannot produce an
		// incorrect outcome (returning ErrNotFound for a deleted user is
		// correct; returning ErrAuthVersionMismatch for a re-bumped version
		// is also correct), but the returned sentinel might not reflect the
		// original failure cause.
		_, lookupErr := UserByID(ctx, db, userID)
		if errors.Is(lookupErr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrAuthVersionMismatch
	}

	// Delete all sessions for this user.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete sessions: %w", err)
	}

	// Kill any in-flight reset link — same transaction.
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_reset_tokens WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete reset tokens: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// ResetUserPassword atomically replaces the user's password verifier with
// the admin-generated temp password, sets the forced-change flag, bumps
// auth_version, and deletes all of the target's sessions.
//
// Unlike UpdatePasswordAndRevokeSessions there is NO auth_version CAS: the
// reset is an admin action against an account whose credentials may be
// unusable, so the current version is irrelevant.
// Concurrency with another admin action follows the user-management rule —
// operations do not use revision guards; the winner applies. The auth_version
// bump is therefore the REVOCATION mechanism (sessions die) rather than a
// race guard.
//
// Transaction:
//  1. UPDATE the verifier + flag + version (0 rows = the target vanished
//     mid-request → ErrNotFound).
//  2. DELETE all session rows for the target (idempotent).
//  3. DELETE any reset-token rows for the target (an admin
//     reset kills in-flight self-reset links).
//
// bcrypt runs in the service BEFORE this store call — never inside the
// write transaction.
//
// Returns ErrNotFound if the user does not exist.
func ResetUserPassword(ctx context.Context, db *sql.DB, userID string, newHash string, updatedAtMS int64) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin reset tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET password = ?, must_change_password = 1, auth_version = auth_version + 1, updated_at_ms = ?
		 WHERE id = ?`,
		newHash, updatedAtMS, userID,
	)
	if err != nil {
		return fmt.Errorf("store: reset user password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: reset user password rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: reset delete sessions: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM password_reset_tokens WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: reset delete reset tokens: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: reset commit: %w", err)
	}
	return nil
}

// IncrementAuthVersionAndDeleteSessions atomically bumps the user's
// auth_version and deletes all of their session rows in one transaction.
//
// This pairing must be atomic: the bump
// is the authoritative revocation and the row deletion is cleanup. As two
// separate operations, a sign-in that commits between them would have its
// fresh session deleted even though it verified the new version — breaking
// the "a concurrent sign-in that commits first is deleted; one that commits
// afterward survives" contract. With BEGIN IMMEDIATE, no other writer can
// interleave.
//
// Returns ErrNotFound if the user does not exist.
func IncrementAuthVersionAndDeleteSessions(ctx context.Context, db *sql.DB, userID string, updatedAtMS int64) error {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin bump+delete tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET auth_version = auth_version + 1, updated_at_ms = ? WHERE id = ?`,
		updatedAtMS, userID,
	)
	if err != nil {
		return fmt.Errorf("store: increment auth version: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: increment auth version rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
