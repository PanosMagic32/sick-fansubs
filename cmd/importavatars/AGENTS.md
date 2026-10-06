# cmd/importavatars — Agent Navigation

Legacy avatar import + reference rewrite + reconciliation report. Opens the target through the maintenance opener, reads the legacy avatar URLs from the USERS EXPORT FILE (`IMPORT_FILE` — the target DB stores NULL for imported users, so the join key is NOT in the DB), joins to `users` by the preserved 24-hex ObjectId, processes each distinct URL once through `media.ProcessAvatar` (200×200 PNG — the avatar pipeline, never the thumbnail one), stores under deterministic sha256-derived IDs, rewrites `users.avatar_url`, prints the report JSON to stdout.

## Open this file when

- changing the command's contract, its input format, its source directory lookup, or its container wiring.

## Folder-local conventions

- `run(logger, importPath, sourceDir)` is the testable seam; `main()` only wires env (`IMPORT_FILE` required, `MEDIA_SOURCE_DIR` default `./media-source`) and the exit code — `DATA_DIR` is loaded inside the seam through the shared `config.Load`.
- The export carries real emails and bcrypt verifiers (hygiene: owner-only, never logged) — only the id→avatar URL pairs are read, and the report never emits URLs, keys, user ids, or filesystem paths.
- The pipeline and storage layout live in `internal/media`; the join-key rule in `internal/migration` (`LegacyMediaKey`, `MediaIDForBytes`) — this command orchestrates only.
- Exit codes mirror `cmd/importmedia`: 0 = zero failed objects, 1 = fatal error OR any failed object (the report still prints first). Per-object commits make interruption safe — a re-run completes the remainder.
- The S3 fetch replaces the local source directory at cutover; `cmd/fetchmedia -kind users` owns the rehearsal HTTP fetch.

## Authoritative docs

- [The migration transforms and media keys](../../internal/migration/AGENTS.md)
- [The media pipeline and its input bound](../../internal/media/AGENTS.md)
