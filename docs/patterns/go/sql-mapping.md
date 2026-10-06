# SQL Mapping

## Purpose

One agreed way to move a Go value across the SQLite boundary: how time is represented, how absence
is represented, and how a nullable column is read back. The mechanism lives in
`internal/store/null.go`; the guards that keep it single-homed live in `internal/codestyle`.

## Rules

1. **Instants persist as UTC Unix milliseconds in `INTEGER` columns.** Go converts explicitly with
   `time.UnixMilli`/`Time.UnixMilli` — never through driver-specific automatic time conversion.
2. **The application supplies every timestamp from an injected clock.** A business or audit column
   never relies on a schema default such as `datetime('now')`; a store operation that needs "now"
   takes it from the caller's sample or its `Now` field.
3. **An unknown instant stays NULL.** The import reports a malformed source time instead of
   inventing one, and a NULL timestamp never becomes an epoch value.
4. **Every ordered read carries a deterministic tie-break**, normally the opaque id after the time
   column, so equal instants page and compare identically on every run.
5. **The null\* family is declared once, in `internal/store/null.go`.** The write side is
   `nullableString(string) any` (empty string → NULL); the read side is `nullStringPtr`
   (NULL → nil pointer) and `nullStringValue` (NULL → `""`). The guard `TestNullFamilyConfined`
   fails a declaration anywhere else. _House rule._
6. **A nullable column scans into `sql.NullString`, never into a plain string.** A migrated NULL
   scanned into a plain string is a runtime scan error, not a zero value; the read half of the
   family does the conversion. A nullable `INTEGER` scans into `sql.NullInt64`, and a pointer
   projection takes `&v.Int64` exactly when `Valid`.
7. **Absence is NULL, never an empty string.** The audit identity columns distinguish "no actor"
   (NULL) from an identifier, and a session's client label stores NULL for "unknown device".
8. **The family has no speculative members.** A helper appears with its first real caller — not
   "for later".
9. **The wire decides null versus absent, the handler owns that projection.** Use `nullStringPtr`
   where the contract has a nullable field (the key stays present with `null`) and the value half
   where the contract reads an empty string (the key is omitted); the store maps the column, never
   the JSON shape.

## Pattern

```go
// internal/store/null.go — the write and read halves.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// internal/store/audit_store.go — a write passes the mapping in the args.
INSERT INTO audit_events (id, event, result, actor_id, …) VALUES (?, ?, ?, ?, …)
id, rec.Event, rec.Result, nullableString(rec.ActorID), …

// internal/store/blog_admin_store.go — a nullable column reads through the family.
var creatorID sql.NullString
rows.Scan(&p.ID, &creatorID, …)
p.CreatorID = nullStringValue(creatorID)
```

## Examples

- `internal/store/null.go` — the family, and nothing else.
- `internal/store/audit_store.go` — the write side (`nullableString`) and the read side
  (`nullStringValue`).
- `internal/store/session_store.go` — a nullable client label on both CREATE and rotate paths.
- `internal/store/blog_admin_store.go` — the staff scans: raw creator/updater ids through
  `nullStringValue`, avatar refs through `nullStringPtr`.
- `internal/store/metrics_store.go` — the window boundary comes from one caller clock sample.

## Gotchas

- **`nullStringPtr` returns a fresh pointer.** Never compare two results by pointer identity, and
  never mutate through the returned pointer.
- **A NULL and an empty string can both be legal and still differ.** Collapsing them at the wrong
  layer breaks the audit contract (NULL actor) and the session contract (NULL label); decide in the
  handler's DTO, not in the query.
- **Do not "fix" a scan error by scanning into `any`.** The STRICT table rejects the wrong type
  outright; the fix is the `Null*` target and the read half.

## Pointers

- Index: [../../README.md](../../README.md)
- SQLite foundation rules (types, time, tables, migrations): [sqlite.md](sqlite.md)
- Identifier generation and storage shapes: [ids.md](ids.md)
- The store feature doc: [`internal/store/AGENTS.md`](../../../internal/store/AGENTS.md)
- Mechanical guards: [`internal/codestyle/AGENTS.md`](../../../internal/codestyle/AGENTS.md)
