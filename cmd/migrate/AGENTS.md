# cmd/migrate — Agent Navigation

Schema migrations only: opens through the maintenance opener, applies pending embedded migrations, verifies the history, exits 0/1.

## Open this file when

- changing the migrate command's contract, its container/Makefile integration, or schema-migration behavior.

## Folder-local conventions

- `PORT` is read but unused — keep the shared `config.Load` rather than forking configuration.
- Keep `main()` a thin wiring shell; everything meaningful lives in `internal/database` (`OpenMaintenance`, `Apply`, `BackupForMigration`, `RestoreOfflineBackup`).
- Migration safety net (docs/patterns/go/sqlite.md): a backup is taken ONLY when history exists AND pending migrations exist — `runWith` owns the seams (`statusFn`/`openFn`/`backupFn`/`restoreFn`) so tests can simulate failure without poisoning embedded migrations. `RestoreOfflineBackup` is safe only because this command owns the offline window (no app writes between backup and rollback) — never call it from a serving process. Restoring OLDER backups goes through the full staged restore in `cmd/restore` (docs/patterns/go/sqlite.md, `make restore-local`) — not through this command.
- The opener refuses to touch a data directory with a restore marker, and `openDB` refuses to create a missing live main while `.broken-<millis>`/`.tmp` rollback remnants remain (a crashed `RestoreOfflineBackup`): the run exits non-zero with the remnant named, and the operator recovers the live file from it (or the retained backup) before re-running. No silent empty database.
- History ahead of this artifact fails BEFORE any backup/migration work.
- Each migration runs in its own immediate transaction under a 30 s budget (the named `migrationTimeout` in `internal/database/migrate.go`): a hung script fails instead of holding the single-writer offline window open. A real migration that needs longer requires raising that constant deliberately — record the reason with the change.

## Authoritative docs

- [The SQLite foundation rules](../../docs/patterns/go/sqlite.md)
