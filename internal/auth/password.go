package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sick-fansubs/internal/store"
)

// bcryptCost is the cost parameter for new password hashes.
// Service carries it as a field: New sets this production
// default, and same-package tests lower it so success-path hashing does not
// burn a second per hash (the COST of verification is read from the stored
// hash, so the seam only speeds hash GENERATION).
const bcryptCost = 12

// maxBcryptCost is the operational maximum cost accepted for password
// verification. A verifier above it fails fast WITHOUT
// running the expensive comparison and routes the account to recovery.
// New hashes are generated at bcryptCost, which stays below the maximum.
// migration.classifyVerifier mirrors this boundary — keep both aligned.
const maxBcryptCost = 13

// legacyBcryptMaxPasswordBytes is bcrypt's 72-byte key boundary. The legacy
// libraries (crypt_blowfish/bcryptjs) silently truncated submissions at this
// boundary when hashing, so a legacy verifier encodes only the first 72
// bytes. VERIFICATION compares the 72-byte prefix to keep that credential
// working; hashing still rejects over-72-byte values.
const legacyBcryptMaxPasswordBytes = 72

// verifyPassword compares a submitted password against a stored bcrypt
// verifier under the rules below:
//
//   - The prefix is checked FIRST against the supported family. Go's parser
//     accepts ANY minor character ($2x$, $2z$, …), so the gate is explicit —
//     the legacy $2x$ variant (a PHP-bug workaround with different high-bit
//     semantics) and anything unknown fail fast as unusable instead of
//     running a potentially wrong comparison.
//   - The verifier's cost is parsed next. Malformed/unsupported verifiers
//     and costs above maxBcryptCost return errVerifierUnusable without
//     running the expensive comparison — the DoS guard, not a comparison
//     result.
//   - A submitted password longer than legacyBcryptMaxPasswordBytes is
//     compared by its first 72 bytes. The legacy libraries truncated at this
//     boundary when hashing, so a legacy verifier encodes the 72-byte prefix;
//     Go's comparison would otherwise cycle the FULL password through the
//     blowfish key schedule and never match. Truncation is verification-only:
//     hashing new passwords still rejects over-72-byte values.
//
// Returns the verifier cost (always <= maxBcryptCost) and nil on a match.
func verifyPassword(storedHash, submitted []byte) (int, error) {
	if !supportedVerifierPrefix(storedHash) {
		return 0, errVerifierUnusable
	}
	cost, err := bcrypt.Cost(storedHash)
	if err != nil || cost > maxBcryptCost {
		return 0, errVerifierUnusable
	}
	if len(submitted) > legacyBcryptMaxPasswordBytes {
		submitted = submitted[:legacyBcryptMaxPasswordBytes]
	}
	if err := bcrypt.CompareHashAndPassword(storedHash, submitted); err != nil {
		return 0, err
	}
	return cost, nil
}

// supportedVerifierPrefix reports whether a stored hash carries one of the
// supported bcrypt family prefixes. Imported verifiers are
// preserved byte-for-byte in every category, so the runtime gate — not the
// migration tool — decides what sign-in will actually compare.
// migration.classifyVerifier applies the same family — keep both aligned.
func supportedVerifierPrefix(hash []byte) bool {
	return bytes.HasPrefix(hash, []byte("$2a$")) ||
		bytes.HasPrefix(hash, []byte("$2b$")) ||
		bytes.HasPrefix(hash, []byte("$2y$"))
}

// ChangePassword verifies the current password and replaces it with a new one.
//
// The operation increments the user's auth_version, which invalidates all
// existing sessions (SessionByDigest enforces s.auth_version = u.auth_version
// in its WHERE clause). Sessions are also explicitly deleted as cleanup.
//
// The caller is responsible for clearing the current request's session cookie
// and requiring the user to sign in again.
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	// Validate new password before touching the database.
	if fe := validateNewPassword("newPassword", newPassword); fe != nil {
		return fe
	}

	// Look up user.
	u, err := store.UserByID(ctx, s.db, userID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("auth: lookup user: %w", err)
	}

	// Verify current password under the cost gate and the 72-byte legacy
	// prefix rule — a migrated over-72-byte credential verifies the same way
	// it did on the legacy system.
	if _, err := verifyPassword([]byte(u.Password), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	// Hash new password.
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.bcryptCost)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}

	// Atomically update password, bump auth_version, and delete all sessions
	// in a single transaction.
	now := time.Now().UnixMilli()
	if err := store.UpdatePasswordAndRevokeSessions(ctx, s.db, userID, string(newHash), u.AuthVersion, now); err != nil {
		if errors.Is(err, store.ErrAuthVersionMismatch) || errors.Is(err, store.ErrNotFound) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("auth: update password: %w", err)
	}

	return nil
}

// ResetPassword replaces a user's password with a freshly generated
// 16-character temp password (the admin reset path, no SMTP). The store sets
// the forced-change flag, bumps auth_version, and deletes the target's
// sessions in one transaction; the plaintext is returned ONCE for the
// operator to hand over out of band, and never logged or stored.
//
// bcrypt runs here, before the store's write transaction (bcrypt must never
// run inside one).
//
// Returns store.ErrNotFound if the target vanished between the caller's
// authorization load and this call.
func (s *Service) ResetPassword(ctx context.Context, userID string) (tempPassword string, err error) {
	temp, err := generateTempPassword()
	if err != nil {
		return "", fmt.Errorf("auth: generate temp password: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(temp), s.bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash temp password: %w", err)
	}

	now := time.Now().UnixMilli()
	if err := store.ResetUserPassword(ctx, s.db, userID, string(hash), now); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", store.ErrNotFound
		}
		return "", fmt.Errorf("auth: reset user password: %w", err)
	}

	return temp, nil
}

// generateTempPassword draws tempPasswordLength characters from the
// unambiguous alphabet via crypto/rand. Rejection sampling (a byte >= the
// largest multiple of the alphabet size is re-drawn) avoids the modulo bias
// that would make some characters slightly more likely.
func generateTempPassword() (string, error) {
	n := len(tempPasswordAlphabet)
	// Largest byte value that maps to a full multiple of n.
	limit := 256 - (256 % n)

	var b strings.Builder
	b.Grow(tempPasswordLength)
	for range tempPasswordLength {
		var rnd [1]byte
		for {
			if _, err := rand.Read(rnd[:]); err != nil {
				return "", err
			}
			if int(rnd[0]) < limit {
				break
			}
		}
		b.WriteByte(tempPasswordAlphabet[int(rnd[0])%n])
	}
	return b.String(), nil
}

// validateNewPassword enforces the shared new-password policy:
// 8–72 bytes, no null bytes. Null bytes are rejected because
// bcrypt treats the password as a C string and stops at the first one — an
// effective length shorter than the byte count (passwords are never silently
// truncated). field names the violation's wire field
// ("password" for registration/reset, "newPassword" for password change).
func validateNewPassword(field, password string) *FieldError {
	if bytes.Contains([]byte(password), []byte{0}) {
		return &FieldError{Field: field, Code: "invalidFormat", Message: "password must not contain null bytes"}
	}
	pwBytes := len([]byte(password))
	if pwBytes < 8 {
		return &FieldError{Field: field, Code: "minLength", Message: "password must be at least 8 bytes"}
	}
	if pwBytes > legacyBcryptMaxPasswordBytes {
		return &FieldError{Field: field, Code: "maxLength", Message: "password must be at most 72 bytes"}
	}
	return nil
}

// isAlphanumeric returns true if ch is [a-zA-Z0-9].
func isAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

var (
	dummyHashOnce sync.Once
	dummyHashVal  []byte
)

// dummyBcryptHash returns a bcrypt hash of a known dummy password at the
// current cost. It is used to perform a real (constant-time) bcrypt
// comparison when the user is not found, reducing timing differences
// between "unknown user" and "wrong password" sign-in attempts.
//
// The hash is generated once on first use and cached.
func dummyBcryptHash() []byte {
	dummyHashOnce.Do(func() {
		var err error
		dummyHashVal, err = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing-defense"), bcryptCost)
		if err != nil {
			panic("failed to generate dummy bcrypt hash: " + err.Error())
		}
	})
	return dummyHashVal
}

// compareDummy runs the dummy-hash comparison. It is a package-level variable
// so tests can swap it with a spy and assert the intended dummy path executes
// (path-execution assertions, never brittle wall-clock thresholds).
// Production always uses bcrypt.CompareHashAndPassword.
var compareDummy = bcrypt.CompareHashAndPassword
