// Package id mints the opaque identifiers this application stores: New is the
// tree's only random-identifier minter, and Fallback is the never-fail
// degraded value for the request-id path. See: docs/patterns/go/ids.md
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// fallbackPrefix marks a degraded identifier. It is long enough that a
// generated value cannot collide with a random one by accident.
const fallbackPrefix = "ffffffffffff"

// fallbackSeq makes concurrent fallback values distinct within one process.
var fallbackSeq atomic.Uint64

// New returns a fresh identifier: 16 cryptographically random bytes as 32
// lowercase hex characters. Database uniqueness is authoritative, so a
// collision is retried at the call site rather than predicted here.
func New() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("id: random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Fallback returns a never-failing 32-character identifier for a caller that
// must keep answering when New fails. It is time-ordered and unique within the
// process — a correlation value, never a secret and never a stored identity.
func Fallback() string {
	return fmt.Sprintf("%s%012x%08x", fallbackPrefix, time.Now().UnixMilli(), fallbackSeq.Add(1))
}
