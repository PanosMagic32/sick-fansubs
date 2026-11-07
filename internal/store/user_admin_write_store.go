package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sick-fansubs/internal/identity"
)

// guardLastActiveSuperAdminTx runs the last-active-super-admin check inside
// the caller's transaction: when the
// caller's operation would reduce the ACTIVE super-admin count — demotion,
// deletion, or suspension — the remaining active count must stay ≥ 1,
// otherwise the transaction returns ErrLastActiveSuperAdmin. The count and
// the write share one immediate transaction, so no concurrent demotion,
// deletion, or suspension can interleave between the check and the write.
// Each caller passes its own trigger condition (the operations differ in
// which changes reduce the active set).
func guardLastActiveSuperAdminTx(ctx context.Context, tx *sql.Tx, wouldReduce bool) error {
	if !wouldReduce {
		return nil
	}
	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = ? AND status = ?`,
		identity.RoleSuperAdmin, identity.StatusActive,
	).Scan(&active); err != nil {
		return fmt.Errorf("store: count active super-admins: %w", err)
	}
	if active <= 1 {
		return ErrLastActiveSuperAdmin
	}
	return nil
}

// ChangeUserRole atomically sets the user's role, bumps auth_version, and
// deletes all of the target's session rows — a role change always revokes
// every session: the bump is the authoritative revocation and the delete is
// cleanup, the same pairing as IncrementAuthVersionAndDeleteSessions.
//
// The actor's role is re-evaluated against the target's LIVE role inside the
// transaction: a promotion that lands after the handler's check must not let
// the actor change a peer's role. A refusal returns ErrActorNotPermitted.
//
// The last-active-super-admin guard runs INSIDE
// the transaction: if the target is an ACTIVE super-admin and newRole is not
// super-admin, the remaining active super-admin count must stay ≥ 1,
// otherwise the demotion — including self-demotion — is rejected with
// ErrLastActiveSuperAdmin. Counting and writing in one immediate transaction
// means no concurrent demotion, deletion, or suspension can interleave
// between the check and the write.
//
// There is no auth_version CAS: user-management operations do not use
// revision guards — the winner applies. The bump is the revocation
// mechanism, not a race guard (the ResetUserPassword precedent).
// The handler short-circuits the no-op (newRole == the current role) before
// this is called — a same-role write would pointlessly bump auth_version and
// revoke sessions.
//
// Returns ErrNotFound if the user does not exist.
// Returns ErrActorNotPermitted if the actor may not change the target's live role.
// Returns ErrLastActiveSuperAdmin if the change would leave zero active super-admins.
func ChangeUserRole(ctx context.Context, db *sql.DB, actorRole, userID, newRole string, updatedAtMS int64) (err error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin role-change tx: %w", err)
	}
	defer rollbackTx(tx, &err)

	// 1. Load the current role/status for the guard decision.
	var currentRole, status string
	err = tx.QueryRowContext(ctx,
		`SELECT role, status FROM users WHERE id = ?`, userID,
	).Scan(&currentRole, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: role-change lookup: %w", err)
	}
	if !identity.CanChangeRole(actorRole, currentRole, newRole) {
		return ErrActorNotPermitted
	}

	// 2. Guard: demoting an ACTIVE super-admin must not empty the active set.
	// A suspended super-admin is already outside the active count, so
	// changing their role cannot reduce it.
	wouldReduce := currentRole == identity.RoleSuperAdmin &&
		newRole != identity.RoleSuperAdmin && status == identity.StatusActive
	if err := guardLastActiveSuperAdminTx(ctx, tx, wouldReduce); err != nil {
		return err
	}

	// 3. Write the role + bump + stamp (0 rows = the user vanished
	// mid-request; impossible inside the same immediate tx after the lookup
	// above, but the check keeps the invariant local).
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET role = ?, auth_version = auth_version + 1, updated_at_ms = ? WHERE id = ?`,
		newRole, updatedAtMS, userID,
	)
	if err != nil {
		return fmt.Errorf("store: role-change update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: role-change rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	// 4. Delete all sessions for this user.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: role-change delete sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: role-change commit: %w", err)
	}
	return nil
}

// DeleteUser hard-deletes the user row in one transaction: the hearts_count
// of every comment the account hearted moves first (the counter/source
// agreement), then the foreign keys cascade the rest.
//
// The last-active-super-admin guard runs inside the same transaction:
// deleting an ACTIVE super-admin that would leave zero active super-admins
// — including self-deletion — is rejected with ErrLastActiveSuperAdmin. A
// suspended super-admin is outside the active count, so deleting one never
// triggers the guard.
//
// The actor's role is re-evaluated against the target's LIVE role inside the
// transaction, so a promotion that lands after the handler's check cannot let
// the actor delete a peer.
//
// Returns ErrNotFound if the user does not exist.
// Returns ErrActorNotPermitted if the actor may not delete the target's live role.
// Returns ErrLastActiveSuperAdmin if the deletion would leave zero active super-admins.
func DeleteUser(ctx context.Context, db *sql.DB, actorRole, userID string) (err error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin delete-user tx: %w", err)
	}
	defer rollbackTx(tx, &err)

	// 1. Load the current role/status for the guard decision.
	var currentRole, status string
	err = tx.QueryRowContext(ctx,
		`SELECT role, status FROM users WHERE id = ?`, userID,
	).Scan(&currentRole, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: delete-user lookup: %w", err)
	}
	if !identity.CanDeleteUser(actorRole, currentRole) {
		return ErrActorNotPermitted
	}

	// 2. Guard: deleting an ACTIVE super-admin must not empty the active set.
	wouldReduce := currentRole == identity.RoleSuperAdmin && status == identity.StatusActive
	if err := guardLastActiveSuperAdminTx(ctx, tx, wouldReduce); err != nil {
		return err
	}

	// 3. The account's heart rows cascade with the delete; move the
	// materialized counters of the SURVIVING comments they point at in the
	// same transaction, so the counter and its source cannot disagree. Rows
	// on comments the account authored are decremented too and then cascade
	// away with them — harmless.
	for _, k := range allContentKinds {
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+k.comments+` SET hearts_count = hearts_count - 1
			 WHERE id IN (SELECT comment_id FROM `+k.hearts+` WHERE user_id = ?)`, userID); err != nil {
			return fmt.Errorf("store: decrement %s comment hearts: %w", k.label, err)
		}
	}

	// 4. Hard-delete the row; the foreign keys cascade the rest.
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete user rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: delete-user commit: %w", err)
	}
	return nil
}

// SuspendUser atomically sets status='suspended', bumps auth_version, and
// deletes all of the target's session rows — suspension
// revokes every session: the bump is the authoritative revocation and the
// delete is cleanup, the ChangeUserRole pairing.
//
// The last-active-super-admin guard runs INSIDE
// the transaction: suspending an ACTIVE super-admin must leave ≥ 1 active
// super-admin, otherwise the suspension — including self-suspension — is
// rejected with ErrLastActiveSuperAdmin. A suspended super-admin is already
// outside the active count, so the guard can never fire for one.
//
// No auth_version CAS: user-management operations do not use revision
// guards — the winner applies. The handler short-circuits the
// already-suspended no-op before this is called, so a SEQUENTIAL repeat
// suspend never double-bumps; two concurrent suspends can both pass the
// handler's pre-read, and the loser of that race re-bumps — acceptable
// under that rule (the bump is revocation, not a race guard).
//
// The actor's role is re-evaluated against the target's LIVE role inside the
// transaction, so a promotion that lands after the handler's check cannot let
// the actor suspend a peer.
//
// Returns ErrNotFound if the user does not exist.
// Returns ErrActorNotPermitted if the actor may not suspend the target's live role.
// Returns ErrLastActiveSuperAdmin if the suspension would leave zero active super-admins.
func SuspendUser(ctx context.Context, db *sql.DB, actorRole, userID string, updatedAtMS int64) (err error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin suspend-user tx: %w", err)
	}
	defer rollbackTx(tx, &err)

	// 1. Load the current role/status for the guard decision.
	var currentRole, status string
	err = tx.QueryRowContext(ctx,
		`SELECT role, status FROM users WHERE id = ?`, userID,
	).Scan(&currentRole, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: suspend-user lookup: %w", err)
	}
	if !identity.CanChangeUserStatus(actorRole, currentRole) {
		return ErrActorNotPermitted
	}

	// 2. Guard: suspending an ACTIVE super-admin must not empty the active set.
	wouldReduce := currentRole == identity.RoleSuperAdmin && status == identity.StatusActive
	if err := guardLastActiveSuperAdminTx(ctx, tx, wouldReduce); err != nil {
		return err
	}

	// 3. Write the status + bump + stamp (0 rows = the user vanished
	// mid-request; impossible inside the same immediate tx after the lookup
	// above, but the check keeps the invariant local).
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET status = ?, auth_version = auth_version + 1, updated_at_ms = ? WHERE id = ?`,
		identity.StatusSuspended, updatedAtMS, userID,
	)
	if err != nil {
		return fmt.Errorf("store: suspend-user update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: suspend-user rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	// 4. Delete all sessions for this user.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: suspend-user delete sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: suspend-user commit: %w", err)
	}
	return nil
}

// ReactivateUser sets status='active' and bumps auth_version in one immediate
// transaction — reactivation requires a new sign-in. No session delete —
// suspension already removed them — and no guard — reactivation can never
// reduce the active super-admin count.
//
// The actor's role is re-evaluated against the target's LIVE role inside the
// transaction, so a promotion that lands after the handler's check cannot let
// the actor reactivate a peer.
//
// Returns ErrNotFound if the user does not exist.
// Returns ErrActorNotPermitted if the actor may not reactivate the target's live role.
func ReactivateUser(ctx context.Context, db *sql.DB, actorRole, userID string, updatedAtMS int64) (err error) {
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin reactivate-user tx: %w", err)
	}
	defer rollbackTx(tx, &err)

	var currentRole string
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&currentRole)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: reactivate-user lookup: %w", err)
	}
	if !identity.CanChangeUserStatus(actorRole, currentRole) {
		return ErrActorNotPermitted
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE users SET status = ?, auth_version = auth_version + 1, updated_at_ms = ? WHERE id = ?`,
		identity.StatusActive, updatedAtMS, userID,
	)
	if err != nil {
		return fmt.Errorf("store: reactivate user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: reactivate user rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: reactivate-user commit: %w", err)
	}
	return nil
}
