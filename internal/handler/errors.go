package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// The handler layer's error vocabulary: the body-reading classification
// sentinels plus the domain-mapping helpers, in one place. The rules live in
// docs/patterns/go/errors.md.
var (
	// errBodyTooLarge reports a request body over its endpoint bound; 413.
	errBodyTooLarge = errors.New("request body exceeds size limit")

	// errUnsupportedMediaType reports a Content-Type other than
	// application/json; 415.
	errUnsupportedMediaType = errors.New("unsupported media type")

	// errInvalidBody reports a body that could not be read or has no
	// recognizable decode failure; 400.
	errInvalidBody = errors.New("invalid request body")

	// errInvalidJSON reports a syntactically invalid JSON body; 400.
	errInvalidJSON = errors.New("invalid JSON in request body")

	// errUnexpectedFieldType reports a JSON value whose type does not match
	// its target field; 400.
	errUnexpectedFieldType = errors.New("unexpected field type in request body")

	// errUnknownField reports a field outside the endpoint's strict DTO; 400.
	errUnknownField = errors.New("request body contains unknown field")

	// errEmptyBody reports an empty or truncated body; 400.
	errEmptyBody = errors.New("request body is empty or incomplete")

	// errTrailingData reports a body with more than one JSON value; 400.
	errTrailingData = errors.New("request body must contain exactly one JSON value")

	// errInvalidCursor reports a cursor that fails strict decode; 400 — the
	// cursor is one opaque token, and the reason is never distinguished.
	errInvalidCursor = errors.New("invalid cursor")
)

// classifyDecodeError converts an encoding/json decode failure into the body
// vocabulary above; the unknown-field message match is the tree's one
// decode-error text match, pinned against the live decoder by its test.
func classifyDecodeError(err error) error {
	if _, ok := errors.AsType[*json.SyntaxError](err); ok {
		return errInvalidJSON
	}
	if _, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return errUnexpectedFieldType
	}
	switch {
	case strings.Contains(err.Error(), "unknown field"):
		return errUnknownField
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errEmptyBody
	default:
		return errInvalidBody
	}
}

// writeStoreError maps a store error to its public outcome and returns the
// status it wrote; an unmapped error becomes the masked 500, logged once
// here. The table and its rules are in docs/patterns/go/errors.md rule 9.
func writeStoreError(w http.ResponseWriter, r *http.Request, err error, op string, args ...any) int {
	return writeStoreOutcome(w, r, err, op, false, args...)
}

// writeStoreAccountError is [writeStoreError] for an operation on the
// viewer's own account row: a vanished row answers the generic 401, not the
// masked 404.
func writeStoreAccountError(w http.ResponseWriter, r *http.Request, err error, op string, args ...any) int {
	return writeStoreOutcome(w, r, err, op, true, args...)
}

// writeStoreOutcome is the one store-error mapping; the exported-in-package
// wrappers name the two not-found contracts.
func writeStoreOutcome(w http.ResponseWriter, r *http.Request, err error, op string, accountRow bool, args ...any) int {
	switch {
	case errors.Is(err, store.ErrNotFound) && accountRow:
		writeUnauthorized(w, r, op+": account deleted")
		return http.StatusUnauthorized
	case errors.Is(err, store.ErrNotFound):
		NotFound(w, r)
		return http.StatusNotFound
	case errors.Is(err, store.ErrConflict):
		writePreconditionFailed(w, r)
		return http.StatusPreconditionFailed
	case errors.Is(err, store.ErrLastActiveSuperAdmin):
		writeProblem(w, r, http.StatusConflict,
			"/problems/auth/last-super-admin", "Conflict")
		return http.StatusConflict
	case errors.Is(err, store.ErrNotAuthor):
		writeForbidden(w, r, op+" requires the author")
		return http.StatusForbidden
	case errors.Is(err, store.ErrInvalidParent):
		writeValidationErrors(w, r, []Violation{{Field: "parentId", Code: "invalidValue"}})
		return http.StatusUnprocessableEntity
	case errors.Is(err, store.ErrSelfHeart):
		writeValidationErrors(w, r, []Violation{{Field: "commentId", Code: "invalidValue"}})
		return http.StatusUnprocessableEntity
	case errors.Is(err, store.ErrSlugTaken):
		writeValidationErrors(w, r, []Violation{{Field: "slug", Code: "alreadyTaken"}})
		return http.StatusUnprocessableEntity
	}
	writeInternalError(w, r, logging.From(r.Context()), op+" failed", args...)
	return http.StatusInternalServerError
}

// writeAuthError maps an auth service error to its public outcome and
// returns the status it wrote, with the same unmapped-500 rule as
// [writeStoreError]; the ErrValidation case extracts [auth.FieldError].
func writeAuthError(w http.ResponseWriter, r *http.Request, err error, op string, args ...any) int {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeUnauthorized(w, r, op+": invalid credentials")
		return http.StatusUnauthorized
	case errors.Is(err, auth.ErrUsernameTaken):
		writeValidationErrors(w, r, []Violation{{Field: "username", Code: "alreadyTaken"}})
		return http.StatusUnprocessableEntity
	case errors.Is(err, auth.ErrEmailTaken):
		writeValidationErrors(w, r, []Violation{{Field: "email", Code: "alreadyTaken"}})
		return http.StatusUnprocessableEntity
	case errors.Is(err, auth.ErrConflict):
		writeConflict(w, r, op+": conflict")
		return http.StatusConflict
	case errors.Is(err, auth.ErrResetTokenInvalid):
		writeResetTokenInvalid(w, r)
		return http.StatusUnprocessableEntity
	case errors.Is(err, auth.ErrVerificationTokenInvalid):
		writeVerificationTokenInvalid(w, r)
		return http.StatusUnprocessableEntity
	case errors.Is(err, auth.ErrValidation):
		violations := []Violation{{Field: "general", Code: "invalid"}}
		if fieldErr, ok := errors.AsType[*auth.FieldError](err); ok {
			violations = []Violation{{Field: fieldErr.Field, Code: fieldErr.Code}}
		}
		// The cause rides the one violation record: the internal validator
		// text is diagnostic only and never reaches the wire.
		writeValidationErrors(w, r, violations, "error", err)
		return http.StatusUnprocessableEntity
	}
	writeInternalError(w, r, logging.From(r.Context()), op+" failed", args...)
	return http.StatusInternalServerError
}
