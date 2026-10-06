# SQLite Foundation

## Purpose

One agreed way to open, configure, migrate, back up, and restore the SQLite database. These rules
cover the engine boundary — the driver, the DSN, pragmas, the pool, transactions, table and column
rules, migrations, the openers, backup, restore, and file ownership. Time and identifier mapping
have their own docs: [sql-mapping.md](sql-mapping.md) and [ids.md](ids.md).

## Rules

1. **The driver is the pinned pure-Go `modernc.org/sqlite`, opened through its connector.** No CGO
   driver, no object-relational mapper, no query builder, no migration library. A database handle
   is bound to a driver value (`sqlite.NewConnector` + `sql.OpenDB`), never looked up by a
   registered name; the registered name `sqlite3` belongs to a different driver and appears nowhere
   in this tree. The guard `TestDriverNameAndDSNConfined` enforces that.
2. **Runtime configuration supplies one absolute data directory, never a DSN.** Go validates and
   (where the operation may) creates the directory, requires owner-only access, and appends the
   fixed filename `sick-fansubs.db`. Maintenance commands may place the same fixed filename inside
   an owner-only operation subdirectory beneath the data directory.
3. **The DSN is constructed with `net/url`, inside `internal/database` only.** The pragma list
   below is the whole tuning surface; do not add cache size, memory mapping, page size, checkpoint,
   or other tuning pragmas without measurements on representative data and VPS resources. The
   guard `TestDriverNameAndDSNConfined` confines the `_pragma` text.

   | Setting                       | Value           | Reason                                                                                                                    |
   | ----------------------------- | --------------- | ------------------------------------------------------------------------------------------------------------------------- |
   | `_pragma=foreign_keys(ON)`    | required        | declared relationships are enforced on every connection                                                                   |
   | `_pragma=busy_timeout(5000)`  | 5 seconds       | wait briefly for the single writer instead of failing                                                                     |
   | `_pragma=synchronous(FULL)`   | required in WAL | each committed transaction survives OS crash or power loss; relaxing to `NORMAL` needs measurements and a recorded ruling |
   | `_pragma=trusted_schema(OFF)` | required        | schema objects cannot invoke unsafe application behavior                                                                  |
   | `_txlock=immediate`           | required        | the write reservation is taken at transaction start                                                                       |
   | `_dqs=0`                      | required        | double-quoted string literals are rejected, not silently accepted                                                         |

4. **WAL mode is set deliberately and verified, never assumed.** A dedicated connection runs
   `PRAGMA journal_mode=WAL` and checks the returned mode before the application is ready; a DSN
   that merely mentions WAL proves nothing. `_txlock=immediate` gives ordinary (non-read-only)
   `BeginTx` calls immediate write locking; `TxOptions{ReadOnly: true}` selects deferred behavior
   but is not a write prohibition, so it is never treated as an authorization boundary.
5. **The pool is bounded by measurement, not by optimism.** `SetMaxOpenConns(4)` and
   `SetMaxIdleConns(4)`, no forced lifetime churn; a larger pool creates no second SQLite writer.
   Every database API that accepts a context gets one with an operation-appropriate deadline. On
   the request path that context is the HTTP request context — canceled when the client
   disconnects — and `busy_timeout` bounds lock waits, not query duration; maintenance commands
   set their own deadlines, and the long import tools deliberately run under the operator's
   control.
6. **Transactions are explicit and short.** Parameterized SQL only. Multi-row or security-state
   changes that must succeed together run in one `sql.Tx` owned by a store operation — no generic
   transaction manager reaches a handler. Every query inside the transaction uses the `*sql.Tx`,
   never the parent pool. Expensive work (hashing, image processing, network calls, large
   transforms) happens before the write transaction opens. Every `BeginTx`, `ExecContext`,
   `QueryContext`, row iteration, `Rows.Close`, `Commit`, and `Rollback` error is checked and
   wrapped where relevant; a failed commit is a failed operation, never a success. Busy/locked
   retries are bounded by the busy timeout or the context, and a business operation is never
   blindly replayed. Races are closed by conditional writes and affected-row checks, not by
   read-then-write application logic.
7. **Tables are `STRICT` unless a documented exception exists.** `TEXT` for opaque identifiers and
   user text; `INTEGER` booleans with `CHECK (value IN (0, 1))`; `INTEGER` UTC Unix-millisecond
   instants; `BLOB` with exact-length checks for fixed binary values (session-token digests, CSRF
   bytes); `NULL` for absent values (the mapping rules are [sql-mapping.md](sql-mapping.md)).
   Identifier columns follow [ids.md](ids.md) rule 2: `TEXT` for every public identity, with
   `INTEGER PRIMARY KEY` (SQLite's rowid alias) only for the internal-only exception that rule
   records. `CHECK` constraints
   for enum values are added from an inventoried production value set. If JSON text is ever
   selected for a model, it requires `json_valid(column)` and update tests; it is not a default
   replacement for relational modeling. Every relationship states its delete/update action
   explicitly — SQLite's default is never relied on. Indexes come from real query, uniqueness,
   foreign-key, cleanup, and ordering needs, added with plans or tests rather than speculation.
   SQLite's `NOCASE` collation is ASCII-oriented and never satisfies Greek case/accent behavior.
8. **Migrations are embedded, ordered, forward-only SQL files.** They live under
   `internal/database/migrations/`, named `0001_users.sql` style (monotonic zero-padded
   versions, parsed as integers), embedded with `embed`, and read and hashed as a complete set —
   invalid names, duplicate versions/names, and gaps fail before any database write. The runner
   owns exactly one unversioned idempotent statement, the `schema_migrations` bootstrap, and runs
   it in its own immediate transaction; it then verifies the metadata table through
   `PRAGMA table_list`/`table_xinfo`/`index_list`/`index_xinfo` and validates every stored row in
   Go (SQLite does not expose `CHECK` clauses as metadata, so no SQL parser is added). The raw
   32-byte SHA-256 of the exact file bytes is stored; an applied migration is never edited — a
   change is a new migration. Because the stored checksum pins the exact bytes, the applied
   migration files are the one recorded exemption to the decision-citation check: a comment
   inside `internal/database/migrations/**` may cite a retired decision record, while no living
   file may. Applied versions must be a contiguous prefix beginning at `1`;
   history order is never inferred from `applied_at_ms`. Each version runs in its own immediate
   transaction that re-reads the row and skips an exact match; the runner executes the complete
   script (no hand-written semicolon splitter), and a migration file contains no `BEGIN`,
   `COMMIT`, or `ROLLBACK` because the runner owns the transaction. `VACUUM` and other
   non-transactional maintenance never appear in a migration file.
9. **Migrations run in one explicit offline step; the request path never migrates.** Production
   deployment runs the maintenance opener while the writer is stopped or unready, in a
   single-owner window. Application startup only performs the strict read-only compatibility
   check. Because the application opener verifies the complete embedded history, an older artifact
   is not assumed runnable after a later migration: rollback means staged restore or fix-forward,
   and expand/migrate/contract design reduces risk without authorizing an unknown migration to be
   ignored. The migration suite covers: empty database to current; every prefix of the embedded set
   to current through the public runner (shape coverage); a migration whose purpose is a data
   transform or backfill ships its own seeded upgrade test at its predecessor's schema (the 0012
   backfill test); a
   second run as a no-op; a failing script rolling back both schema and record; checksum mutation
   rejected; malformed metadata and unknown future versions rejected; concurrent invocations
   serializing with at most one application per version; and both openers accepting and refusing
   exactly their documented states.
10. **Two openers, one readiness contract.** The application opener checks the restore marker
    before touching SQLite at all, then accepts only the exact embedded history; the maintenance
    opener (explicit migrate/import/diagnostic commands) also refuses a present restore marker for
    the live data directory, and accepts an absent metadata table or a valid contiguous older
    prefix (the staged restore's own marker-exempt open of its operation subdirectory is the one
    internal path exempt from that gate). `/health/live` stays up when SQLite is unavailable;
    `/health/ready` verifies, with a short context, the marker's absence, a simple query, the
    migration history, and the schema — it never runs integrity scans. Readiness failures report
    safely: operation names, durations, migration version, and error categories — never SQL
    arguments, credentials, personal data, raw records, session fields, or a full DSN. Setup errors
    return to `main` — this package never calls `log.Fatal` — and pool metrics are exposed only when
    an observed operational need exists.
11. **Backups use the driver's online backup API, never a file copy.** Driver-specific backup
    access stays inside `internal/database` behind `sql.Conn.Raw` and a small private interface;
    the driver connection and its backup object never escape the callback. The destination is an
    unpredictable name in an owner-only directory on the same filesystem, restricted to `0600`
    before the first page copy; pages are copied in bounded steps with the deadline checked
    between steps and bounded `SQLITE_BUSY` handling, because a step already executing cannot be
    cancelled by a context. `Finish` runs exactly once on success or failure, with both the
    primary and the cleanup error preserved. An incomplete destination and its sidecars are
    removed. The completed file is validated by an independent reopen: migration history
    (exact history for the artifact that will serve it, or a contiguous prefix for a pre-migration
    or deploy-trigger unit), `PRAGMA quick_check` returning exactly one `ok` row, and
    `PRAGMA foreign_key_check` returning zero rows — then closed, synced, renamed into place on
    the same filesystem, and the parent directory synced so publication survives a crash. A failed
    or incomplete backup is never promoted, and a quick-check-only file is never described as
    fully integrity-validated: full `PRAGMA integrity_check` (exactly one `ok`) runs before any
    restore activation and during scheduled drills.
12. **Restore is a fail-closed staged workflow, never an overwrite.** One versioned restore
    command owns the whole run while the application is stopped: the exclusive owner-only restore
    marker is acquired first (the application opener, readiness, and the live maintenance openers
    fail closed while it exists); a recent independently verified backup is the rollback artifact
    and is never overwritten; the selected backup is restored into a new owner-only operation
    subdirectory and opened through the maintenance opener; history is validated, a supported older
    schema upgraded, full `integrity_check` and `foreign_key_check` run, every session is deleted in
    one transaction and independently verified as zero; account-security events newer in the live
    database than in the staged copy refuse activation unless the operator passes the explicit
    acknowledge-loss override. The staged file then passes a `wal_checkpoint(TRUNCATE)` and a
    self-containment check, the previous live main/WAL/SHM files move into a retained quarantine
    set, and the staged main file is atomically renamed into place with a parent-directory sync.
    While still unready, the activated file passes the strict WAL/settings/schema check, then the
    marker is removed and its deletion synced. A crash leaves a deterministic state: re-running the
    command resumes or aborts on the on-disk state, and the per-attempt activation record (written
    just before promotion, removed with the marker) is the discriminator — every operation
    directory is scoped to the marker's attempt, so a retained quarantine from an earlier restore
    can never be read as the current attempt's; a failure before activation removes the marker
    again (provably untouched), and a failure after it keeps the marker so ordinary startup stays
    closed. A missing live main beside `.broken-<millis>` rollback remnants refuses to open — no
    empty database is ever created in its place. No restored file ever lands at the live path
    unprepared, and no restored session may authenticate. **One narrow carve-out exists for the
    migration command:** a backup created moments earlier by the same offline run may be restored
    directly — re-validated (history plus full `integrity_check`), the live files quarantined by
    rename, the replacement activated through a temporary file and an atomic rename with the
    quarantine rolled back on failure, and the retained backup never overwritten. Restoring any
    OLDER backup still requires the staged workflow above.
13. **Files and process ownership.** A live WAL database is the main file plus its `-wal` and
    `-shm` sidecars: never back up by copying the main file alone, never place them in an
    ephemeral container layer or on a network filesystem, and keep them on durable local storage.
    One application writer deployment runs against the production file; background writers go
    through the same store policy or an explicit coordination. Graceful shutdown stops background
    database work, waits for in-flight requests, closes the pool, and reports close errors.
    Disk-free and WAL growth are monitored (the disk guardrails plus the app's hourly WAL-size
    log line); manual checkpoint tuning arrives only with observed evidence.

## Pattern

```go
// internal/database/db.go — the one DSN builder and the connector binding.
func openDSN(dsn string) (*sql.DB, error) {
	connector, err := sqlite.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

// internal/database/backup_restore.go — buildDSNForFile is the one pragma home.
q.Add("_pragma", "foreign_keys(ON)")
q.Add("_pragma", "busy_timeout(5000)")
q.Add("_pragma", "synchronous(FULL)")
q.Add("_pragma", "trusted_schema(OFF)")
q.Set("_txlock", "immediate")
q.Set("_dqs", "0")
```

## Examples

- `internal/database/db.go` — DSN construction, the connector, pool limits, WAL setup and pragma
  verification.
- `internal/database/migrate.go` — the embedded migration runner and its verification.
- `internal/database/backup.go` — the online backup workflow and its validation.
- `internal/database/restore.go` — the staged restore state machine and the marker gate.
- `internal/database/schema.go` — the Go mirror of the embedded schema, verified in readiness.
- `internal/database/functions.go`, `collation.go` — the registered SQL functions (`sf_fold`,
  `sf_greek`) that SQL cannot express.
- `cmd/migrate`, `cmd/backup`, `cmd/restore` — the explicit maintenance commands.

## Gotchas

- **An applied migration file is immutable.** Its checksum is stored; editing it fails every
  existing database. A comment that cites a retired document stays in place for the same reason.
- **The DSN pragmas apply per connection.** A new connection is configured by the driver, not by
  earlier statements, which is why every pooled connection is proven, not assumed.
- **SQLite has one writer.** Queueing behind the busy timeout is the design; "more connections"
  is never the fix for write contention.
- **A failed commit can still have written pages.** Treat the operation as failed, surface it, and
  let the caller decide; never report success before `Commit` returned nil.
- **The request path never creates or repairs migration metadata.** A missing or drifted history
  fails closed at startup.

## Pointers

- Index: [../../README.md](../../README.md)
- NULL and time mapping: [sql-mapping.md](sql-mapping.md)
- Identifier generation: [ids.md](ids.md)
- The database feature doc: [`internal/database/AGENTS.md`](../../../internal/database/AGENTS.md)
- The migration and restore commands: [`cmd/migrate/AGENTS.md`](../../../cmd/migrate/AGENTS.md),
  [`cmd/restore/AGENTS.md`](../../../cmd/restore/AGENTS.md)
- Mechanical guards: [`internal/codestyle/AGENTS.md`](../../../internal/codestyle/AGENTS.md)
- Official references: [SQLite WAL](https://www.sqlite.org/wal.html),
  [pragmas](https://www.sqlite.org/pragma.html), [foreign keys](https://www.sqlite.org/foreignkeys.html),
  [online backup](https://www.sqlite.org/backup.html), [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite),
  [Go database/sql](https://pkg.go.dev/database/sql)
