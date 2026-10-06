package auth

import (
	"errors"
	"fmt"
)

// The auth service's error vocabulary in one place. Handlers map these
// through internal/handler/errors.go; the rules live in
// docs/patterns/go/errors.md.
var (
	// ErrInvalidCredentials reports any authentication failure — unknown
	// user, wrong password, suspended account, or an auth-version race. The
	// public outcome is always the one generic 401.
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrResetTokenInvalid reports a reset token that cannot be consumed —
	// missing, expired, used, or an inactive account. One generic 422: the
	// token IS the identity, so no case may be distinguished.
	ErrResetTokenInvalid = errors.New("reset token invalid")

	// ErrVerificationTokenInvalid reports the same for a verification token,
	// with the same one-generic-outcome rule.
	ErrVerificationTokenInvalid = errors.New("verification token invalid")

	// ErrEmailTaken reports registration finding an existing email — a 422
	// {email, alreadyTaken}.
	ErrEmailTaken = errors.New("email already taken")

	// ErrUsernameTaken reports registration finding an existing username —
	// a 422 {username, alreadyTaken}.
	ErrUsernameTaken = errors.New("username already taken")

	// ErrValidation reports input that fails structural validation (length,
	// format, character set); the handler maps it to the 422 /problems/validation
	// shape. Callers test the category with errors.Is and extract the
	// [FieldError] detail with errors.AsType.
	ErrValidation = errors.New("validation failed")

	// ErrConflict reports an operation that lost a concurrent race — a 409.
	ErrConflict = errors.New("conflict: resource already exists")

	// errVerifierUnusable marks a stored verifier that cannot safely run the
	// real comparison: malformed, unsupported, or above maxBcryptCost.
	// Sign-in maps it to the wrong-password outcome and still runs the dummy
	// comparison, so timing leaks nothing.
	errVerifierUnusable = errors.New("auth: verifier unusable")
)

// FieldError is a structured validation error carrying the field name, a
// machine-readable violation code, and a diagnosis message that never reaches
// the wire; handlers map the field/code pair to an RFC 9457 violation.
type FieldError struct {
	Field   string
	Code    string
	Message string
}

// Error formats the field, code, and internal message for logs.
func (e *FieldError) Error() string {
	return fmt.Sprintf("validation: %s %s: %s", e.Field, e.Code, e.Message)
}

// Unwrap returns [ErrValidation] so a caller testing only the category keeps
// working with errors.Is.
func (e *FieldError) Unwrap() error {
	return ErrValidation
}
