package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/store"
)

// verificationTokenLifetime is the email-verification token expiry (7 days —
// lower-stakes than a reset token; the account-page resend is the recovery
// path).
const verificationTokenLifetime = 7 * 24 * time.Hour

// SendVerificationEmail mints a fresh verification token and emails the
// link to the user's address. The token-row transaction commits BEFORE the
// send (no network inside write transactions — docs/patterns/go/sqlite.md
// rule 6). One attempt, one
// token — the newest mint replaces any existing one (one active token per
// user).
//
// The registration path treats a failure here as non-fatal (the
// account exists and the resend surface is the recovery); the resend path
// maps it to an honest 500. Either way the caller logs the error.
func (s *Service) SendVerificationEmail(ctx context.Context, userID string) error {
	if s.sender == nil {
		return fmt.Errorf("auth: mail not configured on this surface")
	}

	u, err := store.UserByID(ctx, s.db, userID)
	if errors.Is(err, store.ErrNotFound) {
		// The user was deleted between registration/session-resolution and
		// this mint — nothing to send to, nothing recoverable.
		return fmt.Errorf("auth: verification user gone: %w", err)
	}
	if err != nil {
		return fmt.Errorf("auth: lookup verification user: %w", err)
	}

	// Sweep expired tokens first (bounded batch — cleanup
	// discipline; one-active-per-user already bounds the table).
	if _, err := store.DeleteExpiredVerificationTokens(ctx, s.db, time.Now().UnixMilli(), store.CleanupBatchSize); err != nil {
		return fmt.Errorf("auth: sweep verification tokens: %w", err)
	}

	// Mint the token (the shared 32-byte material shape).
	_, encoded, digest, err := generateSessionToken()
	if err != nil {
		return fmt.Errorf("auth: generate verification token: %w", err)
	}

	now := time.Now().UnixMilli()
	if err := store.CreateVerificationToken(ctx, s.db, store.CreateVerificationTokenParams{
		TokenDigest: digest,
		UserID:      userID,
		CreatedAtMS: now,
		ExpiresAtMS: now + verificationTokenLifetime.Milliseconds(),
	}); err != nil {
		return fmt.Errorf("auth: store verification token: %w", err)
	}

	subject, body := mail.VerifyEmail(s.base + "/auth/verify?token=" + encoded)
	if err := s.sender.Send(ctx, u.Email, subject, body); err != nil {
		return fmt.Errorf("auth: send verification email: %w", err)
	}

	return nil
}

// ResendVerification re-mints and re-sends the verification email for the
// authenticated user. A VERIFIED user is a nil no-op — the
// handler answers the same 204 without a send (idempotent; the client
// treats it as success).
func (s *Service) ResendVerification(ctx context.Context, userID string) error {
	u, err := store.UserByID(ctx, s.db, userID)
	if errors.Is(err, store.ErrNotFound) {
		// Deleted between session resolution and here — a benign race; the
		// handler maps it to the generic 500 (nothing to send).
		return fmt.Errorf("auth: resend verification user gone: %w", err)
	}
	if err != nil {
		return fmt.Errorf("auth: lookup resend user: %w", err)
	}
	if u.EmailVerifiedAtMS != nil {
		return nil
	}
	return s.SendVerificationEmail(ctx, userID)
}

// IsEmailVerified reports whether the account proved inbox control. The
// resend handler checks it BEFORE the shared verification-send bucket so
// a verified no-op never throttles ("no send, nothing
// to throttle"). The advisory read races benignly: a verify landing
// between this check and the resend makes the resend a no-op — no email,
// no harm.
func (s *Service) IsEmailVerified(ctx context.Context, userID string) (bool, error) {
	u, err := store.UserByID(ctx, s.db, userID)
	if errors.Is(err, store.ErrNotFound) {
		// Deleted between session resolution and here — the handler maps
		// it to the generic 500.
		return false, fmt.Errorf("auth: verified-check user gone: %w", err)
	}
	if err != nil {
		return false, fmt.Errorf("auth: lookup verified-check user: %w", err)
	}
	return u.EmailVerifiedAtMS != nil, nil
}

// VerifyEmail consumes a verification token and stamps the account
// verified. Token-shape first (the reset precedent — no hashing
// work on malformed input), then the advisory existence read, then the
// consume transaction. Returns (userID, role, changed, err): changed=false
// is the idempotent path (the account was ALREADY verified — the reset
// stamped it while the token stayed valid), where the handler skips the
// audit (a no-op is not a state change — the role-change precedent).
// userID and role feed the audit event.
//
// Returns ErrVerificationTokenInvalid for missing/expired/used/suspended
// (one generic outcome); idempotent for an already-verified user holding a
// still-valid token (the reset path stamped it).
func (s *Service) VerifyEmail(ctx context.Context, token string) (userID, role string, changed bool, err error) {
	digest, ok := resetTokenDigest(token)
	if !ok {
		return "", "", false, ErrVerificationTokenInvalid
	}

	now := time.Now().UnixMilli()

	uid, err := store.VerificationTokenUserID(ctx, s.db, digest, now)
	if err != nil {
		if errors.Is(err, store.ErrVerificationTokenInvalid) {
			return "", "", false, ErrVerificationTokenInvalid
		}
		return "", "", false, fmt.Errorf("auth: lookup verification token: %w", err)
	}

	// Load the owning user for the audit role snapshot + the changed flag
	// (advisory — the consume transaction is the authority; the
	// already-verified-in-tx branch fires only when the stamp landed
	// between this read and the transaction, and then the audit honestly
	// reports a verification that DID just happen).
	u, err := store.UserByID(ctx, s.db, uid)
	if errors.Is(err, store.ErrNotFound) {
		return "", "", false, ErrVerificationTokenInvalid
	}
	if err != nil {
		return "", "", false, fmt.Errorf("auth: lookup verification user: %w", err)
	}
	changed = u.EmailVerifiedAtMS == nil

	if err := store.VerifyEmail(ctx, s.db, store.VerifyEmailParams{
		TokenDigest: digest,
		NowMS:       now,
	}); err != nil {
		if errors.Is(err, store.ErrVerificationTokenInvalid) {
			return "", "", false, ErrVerificationTokenInvalid
		}
		return "", "", false, fmt.Errorf("auth: consume verification token: %w", err)
	}

	return u.ID, u.Role, changed, nil
}

// ChangeEmail swaps the account's email after re-authenticating with the
// current password. The email is the
// account-recovery identifier — a stolen session must not reroute recovery
// to an attacker's mailbox, so the current password is mandatory.
//
// Order: password verification (bcrypt, before any write), then the
// registration email validation (trimmed, ASCII charset, lowercased), then
// the same-as-current no-op, then the uniqueness checks (the email column and
// the legacy-username shadow), then the CAS
// transaction. Returns the refreshed user row on success so the handler
// can answer with the updated profile.
//
// The verification send to the new address and the notice to the previous
// address are NOT part of this method — the handler runs them after the
// commit and logs a failure without failing the change.
//
// Returns (u, previousEmail, changed, err): u is the refreshed row,
// previousEmail is the address the account held before the swap (empty
// unless changed is true), and changed=false is the same-as-current no-op
// (the handler skips the sends and the audit). ErrInvalidCredentials (wrong
// password, deleted race, or concurrent security-state change — the generic
// 401, the password-change precedent), ErrValidation (email format),
// ErrEmailTaken (uniqueness or the shadow rule).
func (s *Service) ChangeEmail(ctx context.Context, userID, email, currentPassword string) (*identity.User, string, bool, error) {
	u, err := store.UserByID(ctx, s.db, userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", false, ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("auth: lookup email-change user: %w", err)
	}
	previousEmail := u.Email

	// Re-authentication first: the password gates the recovery identifier.
	// No dummy comparison is needed — the account is known (the
	// authenticated session names it), so no enumeration channel exists
	// to equalize.
	if _, err := verifyPassword([]byte(u.Password), []byte(currentPassword)); err != nil {
		return nil, "", false, ErrInvalidCredentials
	}

	// Email validation: the registration rule, one helper for both paths.
	email, fe := normalizeEmail(email)
	if fe != nil {
		return nil, "", false, fe
	}

	// Same-as-current: a 200 no-op — no state change, no sends, no audit.
	if email == u.Email {
		return u, "", false, nil
	}

	// Uniqueness (the register precedent).
	if _, err := store.UserByEmail(ctx, s.db, email); err == nil {
		return nil, "", false, ErrEmailTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, "", false, fmt.Errorf("auth: check email uniqueness: %w", err)
	}
	// An email equal to another account's legacy username would shadow that
	// username at sign-in and recovery — the lenient lookup both surfaces
	// resolve through, so the shadow check uses it too. The caller's own row
	// is not a shadow; several rows normalizing alike make the address
	// ambiguous, and ambiguity is taken.
	if found, err := store.UserByCanonicalLenient(ctx, s.db, email); err == nil {
		if found.ID != userID {
			return nil, "", false, ErrEmailTaken
		}
	} else if store.IsAmbiguousUser(err) {
		return nil, "", false, ErrEmailTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, "", false, fmt.Errorf("auth: check email against usernames: %w", err)
	}

	// The write's own stamp lands on the held row instead of a re-read: a
	// post-commit read failure must not turn a committed change into a 500,
	// and the transaction just cleared the verification stamp and the token
	// tables the re-read would race.
	now := time.Now().UnixMilli()
	if err := store.ChangeUserEmail(ctx, s.db, userID, email, u.AuthVersion, now); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrAuthVersionMismatch) {
			// Deleted between the session resolution and this write, or a
			// concurrent security-state change — the generic 401 (the
			// password-change precedent, nothing distinguishable).
			return nil, "", false, ErrInvalidCredentials
		}
		if errors.Is(err, store.ErrDuplicate) {
			// The advisory pre-check lost the race — the UNIQUE backstop
			// fired. Same public outcome as the pre-check: 422 alreadyTaken.
			return nil, "", false, ErrEmailTaken
		}
		return nil, "", false, fmt.Errorf("auth: change email: %w", err)
	}

	u.Email = email
	u.EmailVerifiedAtMS = nil
	u.UpdatedAtMS = now
	return u, previousEmail, true, nil
}

// normalizeEmail trims, shape-checks, and lowercases an address under the
// registration rule. Register and ChangeEmail share it so the two surfaces
// cannot drift.
func normalizeEmail(email string) (string, *FieldError) {
	email = strings.TrimSpace(email)
	if !looksLikeEmail(email) {
		return "", &FieldError{Field: "email", Code: "invalidFormat", Message: "invalid email format"}
	}
	return strings.ToLower(email), nil
}

// SendEmailChangeNotice emails the PREVIOUS address that the account's email
// changed. The notice is a security signal sent after the committed swap; a
// failure is logged and never surfaced (the verification-send precedent).
func (s *Service) SendEmailChangeNotice(ctx context.Context, previousEmail, newEmail string) error {
	if s.sender == nil {
		return fmt.Errorf("auth: mail not configured on this surface")
	}
	subject, body := mail.EmailChangedNotice(newEmail)
	if err := s.sender.Send(ctx, previousEmail, subject, body); err != nil {
		return fmt.Errorf("auth: send email change notice: %w", err)
	}
	return nil
}

// looksLikeEmail performs shape and charset validation: non-empty local part,
// exactly one '@', non-empty domain part, at most 254 characters (RFC 5321
// §4.5.3.1.3 path-length limit), and a strict ASCII charset.
//
// Registration accepts only Latin letters, digits, and standard email
// punctuation [A-Za-z0-9@._+-]. Whitespace, control characters, and other
// punctuation are rejected with `invalidFormat`. Non-ASCII addresses (e.g.
// Greek local parts) are rejected, which also makes Unicode NFC normalization
// unnecessary for uniqueness.
func looksLikeEmail(email string) bool {
	if len(email) > 254 {
		return false
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return false
	}
	// Reject multiple '@'.
	if strings.IndexByte(email[at+1:], '@') != -1 {
		return false
	}
	// Strict charset: Latin letters, digits, and standard email punctuation
	// only. This rejects whitespace/control characters as well as any
	// non-ASCII character in a single pass.
	for _, r := range email {
		if !isEmailChar(r) {
			return false
		}
	}
	return true
}

// isEmailChar reports whether r is allowed in a registration email address:
// Latin letters, digits, and standard email punctuation.
func isEmailChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '@' || r == '.' || r == '_' || r == '+' || r == '-'
}
