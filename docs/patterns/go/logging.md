# Logging

## Purpose

One mechanism for logs: where a request's logger comes from, what a record says, and how a
failed request is recorded once at the boundary. The rules rest on the official sources named
in [toolchain.md](toolchain.md); a rule marked _house rule_ is this repository's own
convention.

## Rules

1. **One request-scoped logger, created in `middleware.RequestID`.** The middleware mints
   the request ID, then attaches `logging.From(ctx).With("requestId", id)` and the ID to the
   request context. Nothing else builds a request-scoped logger.
2. **`logging.From(ctx)` is how request-scoped code obtains a logger.** It returns the
   context's logger, or `slog.Default()` — the process-wide handler `cmd/api` installs —
   when the context carries none, so callers never nil-check. `logging.With(ctx, logger)`
   attaches a logger; middleware, commands, and tests are its callers. _House rule._ The
   standard library's design discussion rejected logger-in-context for itself; this
   repository keeps it because the request ID and the logger are created together in
   middleware, and the alternative is a logger parameter threaded through every handler for
   a value only middleware can produce.
3. **A failure message ends in `failed` and names the operation** — `blog list query
failed`, `sign-in failed`, `audit write failed`. A message that reports a state or a
   completed action is a lowercase noun phrase — `server starting`, `media deleted`. Do not
   interpolate request data into the message; it belongs in an attribute. A content-kind
   label is the one allowed prefix: it is a compile-time constant that distinguishes two
   otherwise identical operations (`blog comments list failed`, `projects comments list
failed`).
4. **Field keys are lowerCamel and use the wire spelling of the concept** — `requestId`,
   `actorId`, `targetId` — so a field means the same thing in a log and in a response body.
   Errors go under `error`. Never reuse a `slog` built-in key (`time`, `level`, `msg`,
   `source`) for your own attribute. `requestId` is present on every request-scoped record
   because the request logger carries it.
5. **Log once, at the boundary that owns the policy.** `handler.writeInternalError` is the
   only sanctioned 500 path for JSON endpoints: it records one Error at the handler
   boundary and writes the generic problem. `internal/handler/media.go`'s plain-text 500 is
   the recorded exception — it serves bytes, not JSON, so it keeps its own body and logs
   through the same mechanism.
6. **Levels express severity, not verbosity.** The names are integers (Debug −4, Info 0,
   Warn 4, Error 8) and this repository defines no custom level. The agreed use:

   | Level   | Use                                                                                                                |
   | ------- | ------------------------------------------------------------------------------------------------------------------ |
   | `debug` | Per-request tracing, dropped by default (probe success, fan-out queued)                                            |
   | `info`  | A completed state change worth an operator's attention (startup milestones, audit records, destructive operations) |
   | `warn`  | Refused or degraded but correct (a logged rejection, a retry, a dead push endpoint)                                |
   | `error` | The server failed to do its job (a 5xx, a failed write side effect)                                                |

   A command's fatal usage or startup refusal logs `error` with the `<operation> failed` message and
   the detail under `error`: a command that cannot start has failed its job, which is not the same
   as a runtime rejection inside a working service.

7. **A logging failure never fails the work.** The sink degrades to stderr, and a handler
   error is discarded by `slog`; the request outcome never depends on a log line.
8. **Hygiene: never log emails, tokens, cookies, CSRF values, session IDs, or password
   material (verifiers, bcrypt strings).** Never log a full request body, and never log a
   media file's storage path either — log the asset id. A startup record may name the
   configured data directory: the operator set it. The one audited exemption is
   `internal/mail`'s dev-only `LogLink`, which logs the message body (the reset link) at a
   loopback origin and can never run in production.
9. **Outside a request the logger is explicit.** Startup code in `cmd/` logs through
   `slog.Default()` or its own logger, and a command may install its own handler; a library
   package never calls `slog.SetDefault`. A function with no request context (for example
   `media.Sweep`) takes a logger parameter. Where a request context is in hand, the
   `*Context` method is the one used (`logger.WarnContext(ctx, ...)`); the plain method
   belongs to a scope that has an explicit logger and no request.

## Pattern

```go
// middleware.RequestID — the one place a request logger is built; the mint
// itself lives in internal/id (docs/patterns/go/ids.md).
reqID, err := id.New()
if err != nil {
	reqID = id.Fallback()
}
logger := logging.From(r.Context()).With("requestId", reqID)
ctx := logging.With(SetRequestID(r.Context(), reqID), logger)

// A handler: one local, then boundary logging through the shared helper.
func BlogList(db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		posts, hasNext, err := store.ListPublishedBlogPosts(r.Context(), db, limit, key)
		if err != nil {
			writeInternalError(w, r, logger, "blog list query failed", "error", err)
			return
		}
	}
}
```

## Examples

- `cmd/api/logging.go` — the process handler fans out with `slog.NewMultiHandler`, so a
  `With`-derived attribute reaches stderr and the file sink alike.
- `internal/handler/response.go` — `writeInternalError`, the one 500 path for JSON endpoints.
- `internal/audit/audit.go` — `emit` reads the request logger, so an audit record carries
  `requestId` without being passed one; the durable row keeps `Record.RequestID`.
- `internal/push/push_sender.go` — the fan-out keeps the originating request context for logs and
  runs the delivery under a fresh bounded context, so a cancelled request never cancels a
  delivery that follows its own commit.
- `cmd/resetpassword/main.go` — a command has no request, so it attaches its own logger with
  `logging.With` before it emits an audit event.

## Gotchas

- **No custom handler exists.** The dual sink is the standard library's multi-handler
  (`slog.NewMultiHandler`) around two built-in handlers. A handler added later is validated
  with `testing/slogtest`, the standard way to test one.
- **A test that asserts log output supplies its logger before the handler logs**, because
  `logging.From` reads the context and `RequestID` scopes whatever logger it finds.
  `internal/handler`'s `logContext` test helper attaches the capture logger and re-stamps it
  with the request ID the context already holds.
- **`From` never returns nil**, so a missing logger is a silent fall back to the process
  default, not a panic. Configure the default in `cmd/api` once; a test that forgets to
  inject one gets stderr output rather than a failure.
- **A response carries an `X-Request-ID` header only when the request went through
  `RequestID`.** The handler mints nothing; the health routes apply the middleware for the
  same reason.

## Pointers

- Index: [../../README.md](../../README.md)
- The pinned toolchain, its sources, and how they are refreshed: [toolchain.md](toolchain.md)
- Comments and doc comments: [comments.md](comments.md)
- Structure, naming, and the import rules: [structure.md](structure.md)
- Tests: [testing.md](testing.md)
- The log sink, its rotation, and the log viewer's read path: [`internal/logging`](../../../internal/logging/AGENTS.md)
- Mechanical guards for the checkable subset of these rules: [`internal/codestyle`](../../../internal/codestyle/AGENTS.md)
