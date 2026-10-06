# cmd/rebuildsearch — Agent Navigation

FTS5 search-index rebuild only: opens through the strict application opener, normalizes every publishable blog post/project in Go, refills both FTS tables in one transaction, exits 0/1.

## Open this file when

- changing the rebuild command's contract, its Makefile integration, or search-index maintenance behavior.

## Folder-local conventions

- Keep `main()` a thin wiring shell; the rebuild behavior lives in `internal/store.ReindexAll`.
- Opens through the STRICT application opener — it requires current migration history and never applies migrations (`make migrate-local` is the explicit migration step).
- The rebuild must normalize in Go (SQL cannot strip Greek accents): do not replace it with a pure-SQL `INSERT…SELECT`.
- Rows without a publish time are not indexed; queries additionally filter `status='published'`.
- `DATA_DIR` comes from the shared `config.Load` — do not fork configuration.

## Authoritative docs

- [The search normalization rules](../../internal/search/AGENTS.md)
- [The SQLite foundation rules](../../docs/patterns/go/sqlite.md)
