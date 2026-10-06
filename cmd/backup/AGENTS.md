# cmd/backup — Agent Navigation

Operator-run daily backup unit.

## Open this file when

- changing the backup command's contract, its Makefile targets, or the unit layout/manifest schema.

## Folder-local conventions

- `main()` is a thin wiring shell; `run`/`runBackup`/`runStatus` are the testable seams. The command only parses flags/env, resolves config, calls the database/media packages, encrypts, prunes, and prints the summary — it never implements backup mechanics inline.
- The command is the OPERATOR half of the single-owner unready window: the host cron stops the application first, then runs this command. The database half's restore-marker gate (docs/patterns/go/sqlite.md) is the enforcement — the command fails closed, it cannot stop the app itself.
- Environment contract: `DATA_DIR` (default `./data`), `BACKUP_DIR` (default `./backups`), `AGE_RECIPIENT`, `APP_ENV`. Flags override env. `APP_ENV=production` without `AGE_RECIPIENT` fails closed before creating anything; development without it publishes plaintext with a warning (the owner-confirmed mode split).
- Unit layout: `BACKUP_DIR/YYYY-MM-DD/` holds exactly three artifacts — `sick-fansubs.db`, `sick-fansubs.media.tar`, and the manifest at `media.ManifestPathFor(<db path>)` — each age-encrypted in place as `<name>.age` (ciphertext-only folder in production). The Athens-local date names the folder (Europe/Athens, `time/tzdata` embedded for alpine).
- Access control: an ENCRYPTED unit is readable by any local account — folder 0755, `.age` files 0644 — because age encryption is the boundary and the host-side push script reads the unit as the deploy account (uid 1000 belongs to the owner's account on the VPS, so uid alignment is off the table). The modes are set with explicit publish-time `os.Chmod` calls (umask-proof), and the folder widens only AFTER it holds ciphertext alone — the plaintext halves are created 0600, removed once their ciphertext is fsynced, and a PLAINTEXT (development) unit stays owner-only (folder 0700). Pinned by the end-to-end tests + the umask-independence test. A pre-existing `<date>.staging` can only be a crash leftover (the host lock serializes triggers): it is cleared at entry, and the publish refuses any entry beyond the three expected artifacts.
- Retention: pruning runs only after a successful publish and touches only `YYYY-MM-DD` folders (regex-pinned) — never the pre-migrate safety nets under `<DATA_DIR>/backups/`, never foreign entries under `BACKUP_DIR`. Keep-newest-10.
- `-replace-today` is the deploy-trigger escape hatch: with it, an existing today unit is DELETED (the exact date-named folder only) and republished fresh. It defaults OFF — only `backup-now.sh replace` (from `deploy.sh`) passes it; the cron and `down.sh` keep the fail-closed refusal.
- `-pre-migrate` is the deploy trigger's HISTORY mode: the deploy backup runs BEFORE the deploy's migration, so the live database can be an older prefix of the NEW backup image's embedded set — the database half is then validated as a contiguous prefix (`database.BackupForPreMigration`) instead of the exact set. Only the deploy scripts pass it; the cron and `down.sh` keep exact history. The published unit stays a normal restorable unit (staged restore validates prefix history and upgrades on activation).
- `-status` is the `make backup-status` glance: newest folder + age, exit 0 even with no backups (a failed push surfaces through the cron's own exit codes, not this). It resolves `BACKUP_DIR` only — no `APP_ENV`/`AGE_RECIPIENT` validation — so it works while a missing key is what is being diagnosed.
- The publish rename is fsynced (`syncPath(BACKUP_DIR)`) before success is reported: a crash cannot silently drop the unit, and a sync failure fails the run instead of claiming a durable backup. Retention pruning failures are logged and do not fail the completed unit.
- The compose `backup` service (profile `backup`, image `ghcr.io/panosmagic32/sick-fansubs-backup:${AUX_TAG:-latest}`, uid 1000:1000, `sick-data` + `./backups` bind) is the VPS execution path — the cron body (`deploy/daily-backup.sh`) and the deploy preflight (`deploy/deploy.sh`) both stop the app and run it. `AGE_RECIPIENT` comes from the VPS `backup.env` via an optional `env_file`; the runbook is [`docs/ops/backup.md`](../../docs/ops/backup.md).
- The deadline passed to the database backup variant (`BackupForDaily` or `BackupForPreMigration`) is wall-clock (`time.Now()`); the injected `now` only pins the folder name and manifest timestamp.
- The package never logs inside library packages — it logs from the command (the database/media conventions).

## Authoritative docs

- [The SQLite foundation rules (backup and restore)](../../docs/patterns/go/sqlite.md)
