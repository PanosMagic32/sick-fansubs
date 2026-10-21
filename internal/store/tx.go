package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sick-fansubs/internal/id"
)

// rollbackTx discards a transaction whose operation failed and joins a
// rollback failure into the operation's error. Callers defer it with the
// address of the named error result:
//
//	defer rollbackTx(tx, &err)
//
// A commit that already succeeded makes Rollback report sql.ErrTxDone, which
// is the expected no-op. Any other rollback failure leaves the connection's
// state unknown, so it may never be silent (docs/patterns/go/sqlite.md
// rule 6).
func rollbackTx(tx *sql.Tx, err *error) {
	rerr := tx.Rollback()
	if rerr == nil || errors.Is(rerr, sql.ErrTxDone) {
		return
	}
	*err = errors.Join(*err, fmt.Errorf("store: rollback: %w", rerr))
}

// mintCollisionRetries bounds a mint-site retry when SQLite reports a UNIQUE
// collision on a freshly minted identifier (docs/patterns/go/ids.md rule 5).
const mintCollisionRetries = 3

// insertMintedTx mints one identifier per attempt and runs insert with it,
// retrying a UNIQUE collision a bounded number of times. The database is the
// uniqueness authority: a collision on a 128-bit random identifier is a
// repeat or a bug, never a business conflict, so it must not surface as a
// constraint 500. Any other failure is returned wrapped with op.
func insertMintedTx(ctx context.Context, tx *sql.Tx, op string, insert func(ctx context.Context, newID string) error) error {
	var collision error
	for range mintCollisionRetries {
		newID, err := id.New()
		if err != nil {
			return fmt.Errorf("%s id: %w", op, err)
		}
		err = insert(ctx, newID)
		if err == nil {
			return nil
		}
		if !IsUniqueViolation(err) {
			return fmt.Errorf("%s: %w", op, err)
		}
		collision = err
	}
	return fmt.Errorf("%s: %w: %v", op, ErrDuplicate, collision)
}

// userExistsTx reports whether the user row exists, inside the caller's
// transaction. The guarded writes use it to disambiguate a zero-row UPDATE
// without leaving the transaction (sqlite.md rule 6) — a read on the parent
// pool while the write transaction is open answers from a different
// connection and reopens the TOCTOU window the transaction exists to close.
func userExistsTx(ctx context.Context, tx *sql.Tx, userID string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
