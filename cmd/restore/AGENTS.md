# cmd/restore — Agent Navigation

Operator-run fail-closed staged restore, whose workflow rules live in [`docs/patterns/go/sqlite.md`](../../docs/patterns/go/sqlite.md) rule 12.

## Open this file when

- changing the restore command's contract, its Makefile/container integration, or the staged-restore workflow.

## Folder-local conventions

- `main()` is a thin wiring shell; everything meaningful lives in `internal/database/restore.go` (`StagedRestore`). The command only parses flags, loads config, composes the media half, calls the workflow, and prints the retained quarantine path — it never implements restore mechanics inline.
- The command restores COMPLETE backup units only: the `-backup` path must have the `.media-manifest.json` sidecar next to it. The manifest is read, validated (`media.ValidateManifest`), checked against the backup's base name, and its tarball verified (`media.VerifyMediaTar`) BEFORE `database.StagedRestore` runs — a rejected unit never creates the marker. A pre-migrate DB-only artifact is rejected here; that is `cmd/migrate`'s rollback path.
- The media extraction is wired through `StagedRestoreOptions.PostActivation`: `media.ExtractMediaTar` runs after database activation and before marker removal. An extraction failure keeps the activated DB + the marker (no rollback) — re-run resumes, extraction is idempotent. The database package stays media-free.
- The command is an OFFLINE maintenance step (single-owner window — docs/patterns/go/sqlite.md rule 9; the workflow is rule 12): the operator runs it while the application is stopped, exactly like `cmd/migrate`. The marker gate in `OpenApplication`/readiness is the enforcement, not the command.
- `-acknowledge-loss` maps 1:1 onto `StagedRestoreOptions.AcknowledgeLoss` — the security reconciliation v1 override. The command logs the acknowledged loss at Warn with the two timestamps on the run that COMPLETES the restore; a crash-resume completion carries no reconciliation outcome, so the Warn line is not repeated.
- The post-run operator sequence (start + smoke) lives in `docs/ops/restore.md` rule 4 — the command cannot start the application.
- `run` is the testable seam and validates the empty-backup-path contract itself (main's flag check is the user-facing half).
- The whole workflow is exercised by `internal/database/restore_test.go` (the staged-restore test matrix + the hook sites); this package's tests cover the unit contract (missing manifest/broken tar/checksum/mismatch rejects, media extraction, extraction-failure-then-rerun).

## Authoritative docs

- [The SQLite foundation rules](../../docs/patterns/go/sqlite.md)
- [The staged-restore procedure](../../docs/ops/restore.md)
