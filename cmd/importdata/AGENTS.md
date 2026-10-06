# cmd/importdata — Agent Navigation

Legacy record import + reconciliation report. Opens the target through the maintenance opener, imports, runs the foreign-key check, prints the report JSON to stdout. `-kind blog-posts|projects|users|favorites` selects the record shape (default `blog-posts`; `favorites` reads the USERS export and runs after users + content).

## Open this file when

- changing the import command's contract, its input format, or its container wiring.

## Folder-local conventions

- `run(logger, importPath, kind)` is the testable seam; `main()` only wires the `-kind` flag, `IMPORT_FILE`, and the exit code — `DATA_DIR` is loaded inside the seam through the shared `config.Load`.
- The source file is the legacy export as a JSON array or JSON-lines — read via `migration.ReadLegacyBlogPostsFile`, `migration.ReadLegacyProjectsFile`, or `migration.ReadLegacyUsersFile` (all wrap the shared generic `readLegacyRecords`), never a bespoke reader. `-kind favorites` shares the USERS reader — the arrays live on the legacy user documents.
- The report goes to stdout as indented JSON; operational logs go to stderr. Exit codes 0/1 only.

## Authoritative docs

- [The migration transforms and reports](../../internal/migration/AGENTS.md)
- [The SQLite foundation rules](../../docs/patterns/go/sqlite.md)
