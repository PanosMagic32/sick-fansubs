# Errors

## Purpose

One mechanism for errors: how a package declares its vocabulary, how errors travel up the stack, and
how the HTTP boundary turns them into a masked public outcome with one log record. The rules rest on
the official sources named in [toolchain.md](toolchain.md); a rule marked _house rule_ is this
repository's own convention, and the mapping tables themselves live in
[`internal/handler/errors.go`](../../../internal/handler/errors.go).

## Rules

1. **Check every error, and return it as the last result.** Never signal failure with an in-band
   value (`-1`, an empty string) and never discard an error silently; a deliberate discard needs a
   comment saying why the call cannot fail.
2. **One vocabulary per package, in one file.** `internal/store/errors.go`, `internal/auth/errors.go`,
   `internal/search/errors.go`, and `internal/media/errors.go` hold the exported sentinels; the
   handler layer's own classification sentinels live in `internal/handler/errors.go`. A sentinel is
   declared with `errors.New`, its comment names the public outcome, and a new sentinel means a new
   consumer — never a speculative one.
3. **Test identity with `errors.Is` and `errors.AsType`.** A sentinel is never compared with `==` or
   `!=`; `nil` is the only comparison operand. `errors.AsType` replaces `errors.As` (toolchain
   rule); `internal/codestyle` guards both.
4. **Return the sentinel, or wrap it when you add context.** A bare `return ErrNotFound` is the
   normal form; `fmt.Errorf("load user: %w", ErrNotFound)` is the form when the operation adds
   context. Never return a sentinel's text or a copy. Wrapping is an API commitment: only `%w` an
   error you are willing to return in perpetuity.
5. **Flatten a cause that is not part of the contract.** `fmt.Errorf("%w: %v", ErrEncode, err)` keeps
   the sentinel in the chain and prints the cause, without committing a third-party error type.
   `internal/media`'s pipeline is the recorded shape of this choice.
6. **`Unwrap`, `Is`, or `As` only when default matching is wrong.** `auth.FieldError` is the recorded
   case: it carries `{Field, Code, Message}` and its `Unwrap` returns `ErrValidation`, so a category
   check and a structured extraction both work on one value.
7. **Error strings are lowercase, unpunctuated, and say what was attempted.** "failed to" is allowed;
   the log shape is [logging.md](logging.md)'s rule 3 (`<subject> <action> failed`). Never put
   personal data, tokens, or storage paths in an error string — errors are logged. The operator
   commands (`cmd/backup`, `cmd/restore`, `cmd/migrate`), `internal/database`'s setup and
   maintenance errors, and `internal/media`'s tar/manifest diagnostics (consumed only by the
   restore/backup commands) are the recorded exemption: they may name the data directory or an
   operation path under it (marker, quarantine, rollback remnant, media tar entry), because that
   message IS the operator's diagnostic surface. Request-path errors and user-content paths are
   never exempt.
8. **Classify by type; text matching is the recorded exception.** A decode failure is classified by
   its error type (`*json.SyntaxError`, `*json.UnmarshalTypeError`, `io.EOF`,
   `io.ErrUnexpectedEOF`). The `encoding/json` API exposes no typed unknown-field error, so
   `handler.classifyDecodeError` keeps the tree's ONE decode-error text match for it; a test pins
   that match against the live decoder, and an `internal/codestyle` guard confines it to that file.
   The pinned toolchain's `encoding/json` is v2-backed, and its message text is not a contract. _House
   rule_ for the exception, not for the classification.
9. **Map at the boundary, in one place.** `internal/handler/errors.go` owns the mapping:
   `writeStoreError` (404 for absence, 412 for a stale revision, 409 for the last-super-admin guard,
   403 for a non-author, 422 for the field violations), `writeStoreAccountError` (the same table, but
   a vanished viewer row answers the generic 401 — the session-outlives-user-row race), and
   `writeAuthError` (401 invalid credentials, 409 race, 422 taken fields, the two masked token
   problems, and `ErrValidation` with its `FieldError` extraction). Each helper returns the status it
   wrote, so a caller with an audit hook (a 401, the guard 409, the unmapped 500) reads it instead of
   re-testing the error.
10. **Mask the outcome.** A 404 never reveals whether the row exists or is merely invisible; a 401
    never reveals which check failed; a 422 carries only the stable `{field, code}` pair — internal
    validator text is never public API.
11. **The unmapped default is the masked 500.** A sentinel without a documented handler outcome — and
    every non-sentinel error — is an infrastructure failure: `writeInternalError` logs once at the
    boundary and writes the generic problem. Never add a sentinel to the table merely to avoid a 500.
12. **The boundary logs once.** `writeInternalError` (500), `writeValidationErrors` (422, with the
    caller's cause on the same record), and `writeTooManyRequests` (429, with the caller's `bucket`
    and `userId`) own their records; a caller never logs the same rejection first. The middleware's
    path-scoped IP limiter keeps its own one-record writer (`retryAfter` only) beside the same
    problem document. _House rule._
13. **Panic only on an impossible condition, never on user input.** A `must`/`Must` helper belongs to
    startup; a panic whose condition a request can trigger is a bug. The recorded cases are the
    collation and `sf_fold` registration `init`s in `internal/database`, the nil rate limiters in
    `internal/handler` and `internal/middleware` (wiring errors), and `internal/auth`'s dummy-hash
    warm, which `New` calls at startup so the lazily-built value cannot first materialize on a
    request.
14. **Violation codes are a fixed vocabulary.** `required`, `maxLength`, `minLength`, `maxItems`,
    `alreadyTaken`, `tooLarge`, `outOfRange`, `invalidFormat` (a malformed or unsearchable shape: an
    email, a date, control characters, a link grammar, a query that sanitizes to no tokens),
    `invalidValue` (a well-formed value outside the allowed set — every unknown query or body enum: a
    filter, a sort, a content status; a path enum answers the masked 404 instead), and `invalid` (a
    general, cause not expressible as a field fact). The frontend catalog maps codes to Greek copy.
    _House rule._

## Pattern

```go
// internal/store/errors.go — the vocabulary, one place.
var (
	// ErrNotFound reports that a lookup found no matching row; masked as 404.
	ErrNotFound = errors.New("store: not found")
)

// internal/store/comment_write_store.go — the bare form, and the context form.
func commentAuthor(...) (string, error) {
	...
	if err := row.Scan(&authorID); errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read comment author: %w", err)
	}
}

// internal/handler/auth_password.go — audit hooks read the status the helper wrote.
if err := h.auth.ChangePassword(r.Context(), su.UserID, req.CurrentPassword, req.NewPassword); err != nil {
	switch status := writeAuthError(w, r, err, "password change", "error", err); status {
	case http.StatusUnauthorized, http.StatusInternalServerError:
		h.logAudit(r, audit.EventPasswordChanged, audit.ResultFailure, su.UserID)
	}
	return
}
```

## Examples

- `internal/handler/errors.go` — the classification sentinels, `classifyDecodeError`, and the two
  mapping helpers with their tables.
- `internal/store/errors.go`, `internal/auth/errors.go`, `internal/search/errors.go`, and
  `internal/media/errors.go` — the package vocabularies.
- `internal/handler/errors_internal_test.go` — the mapping matrix, the live-decoder classification
  tripwire, and the one-record pins for the 429 and validation writers.
- `internal/codestyle/rules_test.go` — the sentinel-equality guard and the decode-text confinement
  guard.

## Gotchas

- **The store's `ErrNotFound` masks two facts on purpose.** An unknown id and a row the viewer may
  not see share one sentinel and one 404; do not split them for a "nicer" error.
- **A mapping table describes the whole vocabulary, not one call site.** A table case may cover a
  sentinel the function at hand cannot return today; that is deliberate — the table is the
  vocabulary's contract, and the next caller of the same function inherits the right outcome.
- **A viewer-account write uses `writeStoreAccountError`.** `store.UserByID` on the session's own id
  that finds nothing is the deleted-account race; it answers the same generic 401 a dead session
  does, not a 404.
- **`auth.ErrValidation` without a `*FieldError` is defensive.** Every service return is a
  `FieldError`; the fallback answers `{general, invalid}` rather than failing.
- **The store's SQLite text checks are a different, narrow class.** `IsUniqueViolation` and the
  foreign-key check match the driver's message because SQLite exposes no typed constraint error
  there; they live in `internal/store` and are not covered by the decode-text guard. The migration
  import refines the same exported discriminator with the collided constraint's name
  (`uniqueUserCollisionReason`) — the same narrow class, also outside the guard.
- **The push subscription endpoint is the one `invalidValue` shape exception.** Its URL validation
  answers `invalidValue` for a malformed URL and a disallowed form alike (one branch, one outcome);
  every other site keeps the malformed-shape rule.
- **Do not log the cause twice.** A site that used to log the error and then call a writer would now
  produce two records for one rejection; pass the cause to the writer instead.
- **Duplicate JSON member names are accepted, last value wins.** `encoding/json` does not reject a
  repeated object name, and the decoder adds no rejection: a body with `"status"` twice is read as
  its second value (RFC 8259 §4 says names SHOULD be unique — uniqueness is the producer's
  obligation under this toolchain). `TestReadJSONLimit_DuplicateMemberLastWins` drives the
  production body reader, so a decoder switch that rejects duplicates fails a test instead of
  silently changing the contract.

## Pointers

- Index: [../../README.md](../../README.md)
- Logging, levels, and the one-record rule: [logging.md](logging.md)
- The pinned toolchain and its sources: [toolchain.md](toolchain.md)
- Package structure and naming: [structure.md](structure.md)
- Tests and their failure messages: [testing.md](testing.md)
- The handler boundary's contracts: [`internal/handler/AGENTS.md`](../../../internal/handler/AGENTS.md)
- Mechanical guards for the checkable subset of these rules: [`internal/codestyle`](../../../internal/codestyle/AGENTS.md)
