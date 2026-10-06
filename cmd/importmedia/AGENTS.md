# cmd/importmedia — Agent Navigation

Legacy thumbnail import + reference rewrite + reconciliation report. Opens the target through the maintenance opener, reads legacy `thumbnail_url` values from BOTH `blog_posts` and `projects` as the join key (`migration.LegacyMediaKey` — legacy MinIO key or URL-hash key), processes objects from a local source directory through the media pipeline, stores under deterministic sha256-derived IDs, rewrites references, prints the report JSON to stdout.

## Open this file when

- changing the import command's contract, its input format, its source directory lookup, or its container wiring.

## Folder-local conventions

- `run(logger, sourceDir)` is the testable seam; `main()` only wires env (`MEDIA_SOURCE_DIR` default `./media-source`) and the exit code — `DATA_DIR` is loaded inside the seam through the shared `config.Load`.
- The processing pipeline and storage layout live in `internal/media` — this command orchestrates only (no image code here).
- The report goes to stdout as indented JSON (counts + reason codes, never URLs/keys/paths); operational logs go to stderr. Exit codes: 0 = zero failed objects, 1 = fatal error OR any failed object (the report still prints first) — automation must not infer failures from a missing report.
- Media IDs are `hex(sha256(source bytes))[:32]` (migration.MediaIDForBytes) — content-derived, NOT key-derived: the immutable-cache contract depends on changed bytes never reusing an ID. Do not switch back to key hashing.
- The S3 fetch replaces the local source directory at cutover; `cmd/fetchmedia` owns the rehearsal HTTP fetch — network fetch does not belong here.

## Authoritative docs

- [The media pipeline and its input bound](../../internal/media/AGENTS.md)
- [The migration transforms and media keys](../../internal/migration/AGENTS.md)
