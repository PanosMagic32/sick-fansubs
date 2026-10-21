package database

import (
	"database/sql"
	"errors"
	"fmt"
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
	*err = errors.Join(*err, fmt.Errorf("database: rollback: %w", rerr))
}
