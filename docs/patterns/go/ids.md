# Identifiers

## Purpose

One place mints every identifier this application stores, and every layer treats an identifier as
an opaque string. The mechanism is `internal/id`; the checkable half of these rules is enforced by
guards in `internal/codestyle`.

## Rules

1. **`internal/id` is the tree's only identifier minter.** `id.New()` produces every stored
   identifier — users, sessions, content, comments, notifications, audit rows, uploads. The guard
   `TestIDMintingConfined` fails a buildable file that combines the mint idiom's imports
   (`crypto/rand` plus `encoding/hex`) outside `internal/id` and the two recorded temp-filename
   helpers (`internal/database/backup_restore.go`, `internal/media/backup.go`). _House rule._
2. **A generated identifier is 32 lowercase hex characters from 16 random bytes.** The shape is a
   storage-generation fact, not an API contract: the wire accepts opaque URL-safe identifiers of
   1–64 characters. An internal-only table whose key is never a public identity may use
   `INTEGER PRIMARY KEY` instead; no application table does — the runner's `schema_migrations`
   metadata table is the one integer-PK table, and session ids are public revocation handles and
   stay `TEXT`.
3. **Migrated records keep their legacy identifier where the row preserves one.** The import
   preserves a valid MongoDB ObjectId string exactly for user IDs and project batch sub-ObjectIds
   when present (a migration key, never a MongoDB runtime object); content identifiers instead
   derive deterministically from the legacy ObjectId (`deriveTargetID`). The tree therefore stores
   two shapes: 24-hex preserved ids and 32-hex generated or derived ones.
4. **Identifier checks stay shape-agnostic.** No query, DTO, or path check may require the 24-hex
   or 32-hex form of a domain or public identifier; the two shapes exist on purpose and public
   clients must not infer creation time or record type from a value. Two deliberately narrow
   exceptions exist outside the domain identity: the media-serving and storage-reference grammars
   require the 32-hex generated shape (the storage layout depends on it), and a project slug is
   human-readable and separate from the identifier.
5. **The database is the uniqueness authority.** A practically improbable collision is retried at
   the call site with a small bounded retry; the minter never loops, checks existence, or encodes
   a timestamp. A mint site that lets a uniqueness violation escape as a 500 owns that bug.
6. **A mint failure belongs to the caller.** A handler answers its masked 500 (the
   `writeInternalError` path); a store emitter wraps the failure into its own `<op> id` error. No
   stored identifier has a fallback value — `id.Fallback()` is never persisted.
7. **`id.Fallback()` exists for the request-id path only.** It never fails, is marked by its
   `ffffffffffff` prefix, and is a correlation value (time-ordered, unique within the process),
   not a secret: it keeps `X-Request-ID` and the problem document shaped like a real id when the
   entropy source fails.
8. **The store mints through `internal/id` and imports no media code.** Storage and processing code
   is not an identity source; the guard `TestStoreDoesNotImportMedia` keeps the edge out of
   `internal/store`. _House rule._

## Pattern

```go
// internal/id/id.go — the whole mechanism.
func New() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("id: random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// internal/store/audit_store.go — a store emitter wraps the site failure.
eventID, err := id.New()
if err != nil {
	return fmt.Errorf("store: audit event id: %w", err)
}

// internal/middleware/request_id.go — the one degraded consumer.
reqID, err := id.New()
if err != nil {
	reqID = id.Fallback()
}
```

## Examples

- `internal/id/id.go` — `New`, `Fallback`, and the shape constants.
- `internal/middleware/request_id.go` — the only `Fallback` caller: mints, degrades, logs once.
- `internal/handler/comments_write.go` — handler mints with op-specific failure messages
  (`"comment id generation failed"`).
- `internal/migration/ids.go` — content ids derive from the legacy ObjectId (`deriveTargetID`);
  blog download rows get fresh ids at the import edge (`import.go`), while project batch rows
  preserve the legacy sub-ObjectId when present (`project.go`) — the rule-3 migrated-identifier
  case.
- `internal/codestyle/rules_test.go` — `TestIDMintingConfined`, `TestStoreDoesNotImportMedia`.

## Gotchas

- **Do not add the standard library's `uuid` package** (since Go 1.27; [toolchain.md](toolchain.md)).
  The stored shapes predate it, the migration preserves 24-hex ObjectIds, and a third shape would
  buy nothing.
- **A `Fallback` value is guessable.** It is fine for correlating a request in logs and never for
  anything a user could act on, guess, or trade on.
- **`internal/id` has no module imports.** It stays a leaf package so every layer may depend on it
  without a cycle.

## Pointers

- Index: [../../README.md](../../README.md)
- Go structure and naming: [structure.md](structure.md)
- SQLite storage of identifiers: [sqlite.md](sqlite.md)
- The feature doc: [`internal/id/AGENTS.md`](../../../internal/id/AGENTS.md)
- Mechanical guards: [`internal/codestyle/AGENTS.md`](../../../internal/codestyle/AGENTS.md)
