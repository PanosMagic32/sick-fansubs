package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/store"
)

// resetTokenLifetime is the self-service reset-token expiry: 30 minutes.
const resetTokenLifetime = 30 * time.Minute

// ForgotPassword handles one forgot-password request.
//
// The identifier resolves through the sign-in lookup
// ([Service.lookupUserByIdentifier]).
//
// Outcomes:
//   - Unknown identifier or non-active account: the dummy bcrypt comparison
//     runs (the enumeration-resistance precedent), NO email, NO token
//     row, and ("", nil) — the caller answers 200 {} exactly like the
//     known-account path. The timing channel left by the SMTP send is the
//     accepted residual.
//   - Known active account: one token row is minted (replacing any existing
//     one — one active token per user), the email is sent, and (userID, nil).
//   - Store or send failure on a known account: (userID, err) — the caller
//     answers an honest 500 (a silently dropped email is worse) and audits
//     a failure row. The committed token row is harmless: the next request
//     replaces it.
//
// The token-row transaction commits BEFORE the send (no network inside write
// transactions — docs/patterns/go/sqlite.md rule 6).
func (s *Service) ForgotPassword(ctx context.Context, identifier string) (userID string, err error) {
	// The admin-reset surfaces (routes.Users, cmd/resetpassword) construct
	// the service WITHOUT a mailer — they never invoke this path. The guard
	// turns an accidental call into an honest 500 instead of a nil panic or
	// a logged credential.
	if s.sender == nil {
		return "", fmt.Errorf("auth: mail not configured on this surface")
	}

	// Sweep expired tokens first (bounded batch — cleanup discipline).
	// Runs for EVERY request, known account or not, so the known/unknown
	// paths share the write. The chain's rate limiters run before the
	// handler, so throttled requests never reach this.
	if _, err := store.DeleteExpiredResetTokens(ctx, s.db, time.Now().UnixMilli(), store.CleanupBatchSize); err != nil {
		return "", fmt.Errorf("auth: sweep reset tokens: %w", err)
	}

	u, err := s.lookupIdentifier(ctx, identifier)
	if err != nil {
		return "", err
	}
	if u == nil || u.Status != identity.StatusActive {
		// Unknown OR suspended — the same public outcome (suspended is treated
		// as unknown). The dummy comparison equalizes
		// the DB segment; the SMTP send on the known path remains the
		// recorded residual.
		_ = compareDummy(dummyBcryptHash(), []byte(identifier))
		return "", nil
	}

	// Mint the token (reuses the session-token material shape: 32 bytes
	// crypto/rand, base64url on the wire, SHA-256 at rest).
	_, encoded, digest, err := generateSessionToken()
	if err != nil {
		return u.ID, fmt.Errorf("auth: generate reset token: %w", err)
	}

	now := time.Now().UnixMilli()
	if err := store.CreateResetToken(ctx, s.db, store.CreateResetTokenParams{
		TokenDigest: digest,
		UserID:      u.ID,
		CreatedAtMS: now,
		ExpiresAtMS: now + resetTokenLifetime.Milliseconds(),
	}); err != nil {
		return u.ID, fmt.Errorf("auth: store reset token: %w", err)
	}

	subject, body := mail.ResetEmail(s.base + "/auth/reset?token=" + encoded)
	if err := s.sender.Send(ctx, u.Email, subject, body); err != nil {
		return u.ID, fmt.Errorf("auth: send reset email: %w", err)
	}

	return u.ID, nil
}

// ResetPasswordWithToken consumes a reset token and replaces the password.
// Validation order is token-first: a bad token never
// reaches bcrypt, and a bad token plus a weak password reports only the
// token outcome.
//
// Returns (userID, role, nil) on success — userID drives the conditional
// cookie clear, role feeds the audit target snapshot. Returns
// ErrResetTokenInvalid for missing/expired/used/suspended (one generic
// outcome); ErrValidation for password-policy violations.
func (s *Service) ResetPasswordWithToken(ctx context.Context, token, password string) (userID, role string, err error) {
	// Token shape first: exactly 43 base64url characters, decodable to 32
	// bytes (length-checked before hashing, no CPU
	// amplification).
	digest, ok := resetTokenDigest(token)
	if !ok {
		return "", "", ErrResetTokenInvalid
	}

	now := time.Now().UnixMilli()

	// Token EXISTENCE next: a never-issued or expired
	// token answers the generic token-invalid even when the password is
	// weak — token-first covers existence, not just shape. The consume
	// transaction re-validates, so this read is advisory (the standard repo
	// TOCTOU pattern).
	uid, err := store.ResetTokenUserID(ctx, s.db, digest, now)
	if err != nil {
		if errors.Is(err, store.ErrResetTokenInvalid) {
			return "", "", ErrResetTokenInvalid
		}
		return "", "", fmt.Errorf("auth: lookup reset token: %w", err)
	}

	// Password policy: the registration rules (8–72 bytes, no null bytes).
	// Never runs when the token is bad — the token-first order above.
	if fe := validateNewPassword("password", password); fe != nil {
		return "", "", fe
	}

	// Load the owning user for the CAS input + the audit role snapshot.
	u, err := store.UserByID(ctx, s.db, uid)
	if errors.Is(err, store.ErrNotFound) {
		return "", "", ErrResetTokenInvalid
	}
	if err != nil {
		return "", "", fmt.Errorf("auth: lookup reset user: %w", err)
	}

	// bcrypt runs here, BEFORE the store's write transaction (bcrypt must
	// never run inside one).
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return "", "", fmt.Errorf("auth: hash new password: %w", err)
	}

	if err := store.ResetPasswordWithToken(ctx, s.db, store.ResetPasswordWithTokenParams{
		TokenDigest:        digest,
		NowMS:              now,
		CurrentAuthVersion: u.AuthVersion,
		NewHash:            string(hash),
		UpdatedAtMS:        now,
	}); err != nil {
		if errors.Is(err, store.ErrResetTokenInvalid) || errors.Is(err, store.ErrNotFound) {
			return "", "", ErrResetTokenInvalid
		}
		return "", "", fmt.Errorf("auth: consume reset token: %w", err)
	}

	return u.ID, u.Role, nil
}

// resetTokenDigest decodes a submitted base64url token and returns
// its SHA-256 digest. It returns ok=false for any wrong shape: not exactly
// 43 characters (32 bytes of base64url), non-canonical padding, or invalid
// alphabet — checked before any hashing work. The reset and verification
// tokens share the material shape.
func resetTokenDigest(encoded string) ([]byte, bool) {
	if len(encoded) != 43 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	if len(raw) != 32 {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}
