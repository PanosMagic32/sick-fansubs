package handler

import (
	"errors"

	"sick-fansubs/internal/store"
)

// maxMintAttempts bounds a handler's identifier-collision retry. The database
// is the uniqueness authority, and a 128-bit collision is practically
// impossible; three attempts separate a freak repeat from a bug.
const maxMintAttempts = 3

// notified is one store operation's notification outcome: the fan-out event
// and whether a notification row actually landed. It lets a mint-retry
// operation hand back both values through one result.
type notified struct {
	event   store.NotificationEvent
	emitted bool
}

// mintRetry runs op up to maxMintAttempts times while the database rejects a
// freshly minted identifier (store.ErrDuplicate). The store maps that
// constraint; this function is the call-site half of the bounded retry
// (docs/patterns/go/ids.md rule 5). Any other error — including a mint
// failure wrapped with errIDMint — returns immediately, and the last
// collision is returned when every attempt fails.
func mintRetry[T any](op func() (T, error)) (T, error) {
	var zero T
	var last error
	for range maxMintAttempts {
		v, err := op()
		if err == nil || !errors.Is(err, store.ErrDuplicate) {
			return v, err
		}
		last = err
	}
	return zero, last
}
