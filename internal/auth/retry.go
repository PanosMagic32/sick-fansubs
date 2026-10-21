package auth

import (
	"errors"

	"sick-fansubs/internal/store"
)

// duplicateAttempts bounds a retry when the database rejects freshly minted
// credential material. The database is the uniqueness authority, and a
// 128-bit collision is practically impossible; three attempts separate a
// freak repeat from a bug (docs/patterns/go/ids.md rule 5).
const duplicateAttempts = 3

// retryDuplicate runs op until it succeeds or returns an error other than
// store.ErrDuplicate. The last collision is returned when the budget is spent.
func retryDuplicate[T any](op func() (T, error)) (T, error) {
	var zero T
	var last error
	for range duplicateAttempts {
		v, err := op()
		if !errors.Is(err, store.ErrDuplicate) {
			return v, err
		}
		last = err
	}
	return zero, last
}
