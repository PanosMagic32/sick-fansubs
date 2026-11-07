package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sick-fansubs/internal/identity"
)

// UpdatePasswordAndRevokeSessions atomically updates the password hash,
// increments auth_version, deletes all sessions for the user, removes any
// active reset tokens, and drops the user's push subscriptions in a single
// transaction. Used by password change; the token deletion follows the rule
// that ANY password change kills in-flight reset links (the new password
// wins). Push delivery is account-bound, so a password change stops it on
// every device rather than leaving rows that can only be reclaimed by a
// later dead-endpoint report.
//
// Returns ErrAuthVersionMismatch if the version no longer matches.
// Returns ErrNotFound if the user does not exist.
func UpdatePasswordAndRevokeSessions(ctx context.Context, db *sql.DB, userID string, newHash string, currentAuthVersion int64, updatedAtMS int64) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer rollbackTx(tx, &err)

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
		// Disambiguate "user not found" from "auth version changed" inside
		// the transaction, so the answer matches the UPDATE's snapshot.
		exists, lookupErr := userExistsTx(ctx, tx, userID)
		if lookupErr != nil {
			return fmt.Errorf("store: password update disambiguation: %w", lookupErr)
		}
		if !exists {
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

	// Push subscriptions are account-bound: every session just died, so the
	// rows that would deliver to those sessions' devices die with them. A
	// device reopening the app re-registers its still-live browser
	// subscription through the background reconcile.
	if _, err := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete push subscriptions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// ResetUserPassword atomically replaces the user's password verifier with
// the admin-generated temp password, sets the forced-change flag, bumps
// auth_version, deletes all of the target's sessions, drops the target's
// push subscriptions, and removes in-flight reset links.
//
// The actor's role is re-evaluated against the target's LIVE role inside the
// transaction: a promotion that lands after the handler's check must not let
// an admin mint a temp password for a new super-admin. A refusal returns
// ErrActorNotPermitted.
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
//  0. Read the target's live role and authorize the actor against it.
//  1. UPDATE the verifier + flag + version (0 rows = the target vanished
//     mid-request → ErrNotFound).
//  2. DELETE all session rows for the target (idempotent).
//  3. DELETE any reset-token rows for the target (an admin
//     reset kills in-flight self-reset links).
//  4. DELETE the target's push subscriptions (the sessions carrying them
//     are gone; a re-login re-registers a still-live browser subscription).
//
// bcrypt runs in the service BEFORE this store call — never inside the
// write transaction.
//
// Returns ErrNotFound if the user does not exist.
// Returns ErrActorNotPermitted if the actor may not reset the target's live role.
func ResetUserPassword(ctx context.Context, db *sql.DB, actorRole, userID string, newHash string, updatedAtMS int64) (err error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin reset tx: %w", err)
	}
	defer rollbackTx(tx, &err)

	var currentRole string
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&currentRole)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: reset lookup: %w", err)
	}
	if !identity.CanResetPassword(actorRole, currentRole) {
		return ErrActorNotPermitted
	}

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

	if _, err := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: reset delete push subscriptions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: reset commit: %w", err)
	}
	return nil
}

// IncrementAuthVersionAndDeleteSessions atomically bumps the user's
// auth_version, deletes all of their session rows, and drops the user's push
// subscriptions in one transaction.
//
// Push delivery is account-bound — signing out all sessions revokes every
// device's access, so the rows that would keep notifying those devices die
// with the sessions. A device that signs in again re-registers its still-live
// browser subscription through the client's background reconcile.
//
// The bump+delete pairing must be atomic: the bump
// is the authoritative revocation and the row deletion is cleanup. As two
// separate operations, a sign-in that commits between them would have its
// fresh session deleted even though it verified the new version — breaking
// the "a concurrent sign-in that commits first is deleted; one that commits
// afterward survives" contract. With BEGIN IMMEDIATE, no other writer can
// interleave.
//
// Returns ErrNotFound if the user does not exist.
func IncrementAuthVersionAndDeleteSessions(ctx context.Context, db *sql.DB, userID string, updatedAtMS int64) (err error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin bump+delete tx: %w", err)
	}
	defer rollbackTx(tx, &err)

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

	if _, err := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: delete push subscriptions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
