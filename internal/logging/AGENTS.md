# internal/logging — Agent Navigation

The application's log SINK: the JSON-lines file beside stderr, its size rotation, and the reader the super-admin viewer uses.

## Open this file when

- changing log rotation, the log file's location or shape, or the log viewer's read path;
- adding a `slog` call site or a request-scoped logger (see the pattern doc and the hygiene rule below).

## Folder-local conventions

- No configuration surface, by decision: no env var, no level knob, no retention policy (the "no new env vars" rule). `DefaultMaxBytes` (5 MB), `DefaultMaxFiles` (4 = the live file plus three backups → the ~20 MB ceiling) and the `logs/app.log` layout are constants pinned by `TestDefaults_MatchTheDecision`; changing them is a contract change, not a tweak.
- **The request-scoped logger lives here too:** `With(ctx, logger)` attaches one and `From(ctx)` reads it, returning `slog.Default()` when the context carries none (never nil). `middleware.RequestID` is the one production caller that builds one, stamping the `requestId` field; handlers, services, and tests obtain theirs through `From`. Rules: [`docs/patterns/go/logging.md`](../../docs/patterns/go/logging.md).
- The writer rotates BEFORE the write that would cross the threshold, so a record is never split across two files — and it adopts the size of a file an earlier process left behind, so a restart does not postpone rotation.
- **A logging failure never fails the request.** slog discards the error a handler returns, so `RotatingWriter` reports a failure once per episode to its injected reporter (main passes `reportLogSinkFailure`, which writes to stderr directly — routing it through slog would loop the logger into itself) and keeps going. `rotate()` closes the live handle before renaming, so on a rotation failure the writer REOPENS the file: the ceiling is best-effort, the record is not. A single record larger than `maxBytes` passes through whole; the ceiling bounds the steady state, not an individual write.
- The process handler fans records to stderr and the file with the standard library's multi-handler (`slog.NewMultiHandler`), so a `With`-derived attribute reaches both sinks and a handler that filters below the other's level cannot silence the record.
- Files are `0700`/`0600` and live under the data directory (`data/logs`), beside `data/backups` and `data/media`. They are NOT part of the backup unit and must never be copied into docs, fixtures, or prompts — a log carries client addresses.
- The reader (`Read`) takes the request `context` — a superseded request stops rather than scanning the whole sink — walks the files newest → oldest and each file's lines BACKWARDS, stopping as soon as the limit is satisfied, and caps any single file at the last 8 MB. A missing directory or file is an empty result, not an error (a fresh install has no log yet), and a line that does not parse — a truncated tail after a kill, a foreign line — is skipped. `ParseLevel` is the ONE definition of the level vocabulary (`debug|info|warn|error`), shared by the query parameter and the stored-line parse, and `LevelName` derives the canonical spelling from the parsed level, so a foreign line spelled " warn" can never reach the wire.
- **What a record may say is the pattern doc's rule** ([`docs/patterns/go/logging.md`](../../docs/patterns/go/logging.md) rules 3–8): the message ends in `failed` when it reports a failure, data rides attributes, and no record carries an email, token, cookie, CSRF value, session id, or media storage path.

## Authoritative docs

- [`docs/patterns/go/logging.md`](../../docs/patterns/go/logging.md) — the mechanism, the record shape, and the level table
