package store

import "errors"

// The store's error vocabulary: every sentinel a caller may test, in one
// place. Callers test identity with errors.Is; the public-outcome mapping
// lives in internal/handler/errors.go.
var (
	// ErrNotFound reports that a lookup found no matching row; masked as 404.
	ErrNotFound = errors.New("store: not found")

	// errAmbiguousUser reports a lenient username lookup where several rows
	// normalize alike. It is wrapped with ErrNotFound so fail-closed callers
	// keep their outcome; IsAmbiguousUser is how a caller detects it.
	errAmbiguousUser = errors.New("store: ambiguous user")

	// ErrAuthVersionMismatch reports a revoked, suspended, or deleted
	// account; every cause shares the one generic 401 outcome.
	ErrAuthVersionMismatch = errors.New("store: auth version mismatch")

	// ErrDuplicate reports a UNIQUE constraint violation at create time. The
	// caller disambiguates which constraint held (see IsUniqueViolation).
	ErrDuplicate = errors.New("store: duplicate")

	// ErrLastActiveSuperAdmin reports a write that would leave zero active
	// super-admins; the guard is a 409.
	ErrLastActiveSuperAdmin = errors.New("store: last active super-admin")

	// ErrConflict reports a revision-conditional write that matched no row
	// although the resource exists; the caller's revision is stale — a 412.
	ErrConflict = errors.New("store: conflict")

	// ErrSlugTaken reports a projects slug UNIQUE violation — a 422
	// {slug, alreadyTaken}.
	ErrSlugTaken = errors.New("store: slug taken")

	// ErrNotAuthor reports a comment write by someone other than its author;
	// a 403.
	ErrNotAuthor = errors.New("store: comment author mismatch")

	// ErrInvalidParent reports a reply addressed to a reply; a 422
	// {parentId, invalidValue} (reply depth is an API rule).
	ErrInvalidParent = errors.New("store: reply to a reply")

	// ErrSelfHeart reports a heart on the viewer's own comment; a 422
	// {commentId, invalidValue}. The heart DELETE path stays ungated.
	ErrSelfHeart = errors.New("store: cannot heart own comment")

	// ErrResetTokenInvalid reports a reset token that cannot be consumed —
	// missing, expired, used, or an inactive account. One generic 422
	// outcome: the token IS the identity, so no case may be distinguished.
	ErrResetTokenInvalid = errors.New("store: reset token invalid")

	// ErrVerificationTokenInvalid reports a verification token that cannot
	// be consumed, with the same one-generic-outcome rule as a reset token.
	ErrVerificationTokenInvalid = errors.New("store: verification token invalid")
)

// IsAmbiguousUser reports whether a lenient username lookup failed because
// more than one stored row normalizes alike. Such a value also satisfies
// errors.Is(err, ErrNotFound) — the lookup still answers nothing.
func IsAmbiguousUser(err error) bool {
	return errors.Is(err, errAmbiguousUser)
}
