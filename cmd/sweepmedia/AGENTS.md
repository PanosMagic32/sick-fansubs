# cmd/sweepmedia — Agent Navigation

Offline media orphan sweep: the manual trigger (`make sweep-media-local`) for the same sweep the API process runs at startup and hourly.

## Open this file when

- changing the sweep mechanics, the grace window, the reference snapshot, or the command's exit/logging contract.

## Folder-local conventions

- Opens through the strict application opener (`database.OpenApplication`) — NEVER applies migrations; run `make migrate-local` first.
- Run while the application is stopped (the `cmd/rebuildsearch` maintenance pattern).
- The sweep logic itself lives in `internal/media.Sweep` + `store.ReferencedMediaPaths` — this command is wiring + logging only; do not duplicate policy here.
- Grace window and cadence constants live in `internal/media` (`SweepGrace`) and `cmd/api` (`mediaSweepInterval`); keep the command consistent with them, not ahead of them.

## Authoritative docs

- [The sweep policy and grace window](../../internal/media/AGENTS.md)
- [The SQLite foundation rules](../../docs/patterns/go/sqlite.md)
