# cmd/fetchmedia — Agent Navigation

Downloads the legacy media objects referenced by a legacy export file (thumbnails for `-kind blog-posts|projects`, avatars for `-kind users`) into `MEDIA_SOURCE_DIR` (default `./media-source`), keyed by `migration.LegacyMediaKey` (legacy MinIO key or URL-hash). This is the fetch half of the media migration; `cmd/importmedia` and `cmd/importavatars` consume the directory.

## Open this file when

- changing the fetch contract, its input format, reason codes, or its container wiring.

## Folder-local conventions

- `run(logger, importPath, kind, outDir)` is the testable seam; `main()` only wires the `-kind` flag, env (`IMPORT_FILE`, `MEDIA_SOURCE_DIR`), and the exit code.
- Only http(s) media URLs are fetched (`unsupportedScheme` for others); every well-formed absolute URL is attempted — external-host shapes (Discord CDN, postimg) are the majority of the production data and must never be skipped; failures are per-object reason codes (`notFound`, `httpStatus`, `networkError`, `oversized`, `badReferenceShape`, `unsupportedScheme`, `readBody`, `writeFile`) and never abort the run. The default HTTP client redirect policy applies (up to 10 hops, cross-host, https→http permitted): the initial URL's scheme is the gate, and the `MaxInputBytes` body cap plus the atomic write are the backstops.
- Downloads are atomic (tmp + rename); existing files are skipped so re-runs are idempotent — the media import hashes the bytes, so content-derived IDs stay stable regardless.
- The object bound is `media.MaxInputBytes` — keep fetch and import bounds in lockstep.
- The report goes to stdout as indented JSON; operational logs go to stderr — a start line plus a progress line every 25 objects (the download phase of a large run must not be silent). Exit codes 0/1 only: 1 is a fatal error; per-object failures are counted in the report, never turned into the exit code.

## Authoritative docs

- [The migration transforms and media keys](../../internal/migration/AGENTS.md)
- [The media pipeline and its input bound](../../internal/media/AGENTS.md)
