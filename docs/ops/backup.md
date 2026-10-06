# Backup

## Purpose

The backup lifecycle of the production stack: one validated, age-encrypted unit per day, where it
lives, what triggers it, how it leaves the VPS, and what to check when it breaks. The restore side
is [restore.md](restore.md). This file is shipped to the VPS by the deploy workflow, so the
commands below are the same ones the host runs.

All commands run as `sick-deploy` from `~/sick-fansubs` unless noted. The VPS has no repository: it
runs the shipped scripts (`backup-now.sh`, `daily-backup.sh`, `disk-check.sh`, `disk-cleanup.sh`,
`mega-put.sh`, `down.sh`, `lock.sh`) and the Compose `backup` profile. The local equivalents are
`make backup-local` / `make backup-status` (see `make help`).

## Rules

1. **One backup is one date-stamped folder** under `backups/`, named for the Europe/Athens date,
   holding three files: the validated database artifact, the media tarball captured at the same
   point, and the manifest that binds them. Production publishes ciphertext only; with
   `APP_ENV=production` a missing `AGE_RECIPIENT` fails closed before anything is written. The
   artifact's validation and the staged-restore mechanics are
   [sqlite.md](../patterns/go/sqlite.md)'s rules; the manifest contract is
   [internal/media/AGENTS.md](../../internal/media/AGENTS.md)'s.
2. **Retention is ten units locally, pruned after each successful publish; the Mega copy is
   write-only.** Nothing on the VPS can delete anything in Mega — the remote retention (also ten)
   is a manual owner task. The local unit size dominates the VPS disk, so image and log space is
   reclaimed separately by the weekly `disk-cleanup.sh`.
3. **The capture is a single-owner window.** The app is stopped, the backup runs, and on failure
   a restart is attempted, its outcome reported, and the trigger exits non-zero. On success
   `backup-now.sh` leaves the app stopped and the caller decides the next step (migrate, `up -d`,
   or `down`). One script holds that stop/backup/restart sequence so the three triggers cannot
   drift, and a host lock (`flock` on `~/sick-fansubs/.lock`) serializes the triggers — a cron run, a
   shutdown, and a deploy can never overlap their stop/migrate/up steps. `backup-now.sh` runs
   `disk-check.sh` first: a critically full host fails the trigger before the stop window.
4. **Three triggers, one exception.** The daily cron, the shutdown wrapper, and the deploy
   preflight all route through `backup-now.sh`. Cron and shutdown are fail-closed: an existing
   same-day unit is never clobbered. The deploy trigger alone replaces today's unit with a fresh
   pre-migrate one — its rollback point must be fresh, and its history is validated as a contiguous
   prefix because it runs before the deploy's migration. The deploy preflight runs the backup on
   the image paired with the tag being deployed, so a re-run after a schema-advanced rollback
   validates the newer live history instead of failing on an older image's embedded set.
5. **Ciphertext is the access boundary.** The published folder is `0755` and the `.age` files
   `0644`, so any local account — including `sick-deploy` — can read and ship them; the age
   encryption is what protects them. The transient plaintext halves stay owner-only (`0600`) and
   are removed once their ciphertext is fsynced. The ancestor directories stay traversable.
6. **The off-VPS leg is a write-only push.** `mega-put.sh` uploads the newest folder to
   `/sick-fansubs-backups/` on the dedicated backup-only Mega account. A failed push leaves the
   local unit published and exits non-zero; the recovery point is the time since the last
   successful push. On success the script stamps `.last-push` in `~/sick-fansubs` with the UTC time —
   that stamp is the readable form of the RPO (the `backups/` directory is container-owned, the
   operator's home is what the script can write).
7. **Status is a glance, not a monitor.** `make backup-status` (local) or
   `docker compose --profile backup run --rm backup -status` (VPS) prints the newest published
   folder and its age; a missing `BACKUP_DIR` says so instead of reading as "no backups yet". Check
   it weekly, together with `cat ~/sick-fansubs/.last-push`,
   `mega-ls /sick-fansubs-backups/`, `df -h /`, `docker system df`,
   `docker compose logs caddy | grep -iE 'renew|certificate'`,
   `grep 'retention prune failed' ~/sick-fansubs/backup-cron.log`, and
   `docker compose logs app --since 24h | grep 'database WAL'` — `-status` sees the local disk
   only, so the stamp and the remote listing prove the off-site leg, the disk lines prove the next
   unit has room, the Caddy lines prove renewal is not quietly failing, the prune grep proves
   retention is keeping up, and the WAL lines show the hourly `walBytes` samples: compare
   consecutive values, and treat a `database WAL at high-water size` warning as "confirm no
   long-running reader pins it" (the file stays large until a truncating checkpoint).
8. **Shutdown goes through the wrapper.** `sh down.sh` backs up and then takes the stack down; a
   raw `docker compose down` does NOT back up. A failed backup leaves the stack in place (the
   restart outcome is reported) and exits non-zero — you want to know backups are broken before
   tearing anything down. Volumes survive the shutdown: `down` never runs with `-v`.

## Pattern

The unit and the daily flow:

```text
backups/2026-09-02/
  sick-fansubs.db.age                       # validated DB artifact (history + quick_check + FK check)
  sick-fansubs.media.tar.age                # media tarball from the same capture point
  sick-fansubs.db.media-manifest.json.age   # binds the two: sha256 + filenames + timestamp

cron ─▶ daily-backup.sh ─▶ backup-now.sh (disk-check ─▶ stop app ─▶ backup) ─▶ up -d ─▶ mega-put.sh
        on success backup-now leaves the app STOPPED for its caller; a failure
        restarts it and aborts, and only a successful backup reaches the push
```

## Examples

One-time bring-up, once per VPS lifetime:

1. **age keypair** — on your own machine, never the VPS: `age-keygen -o sick-backups.key.txt`, and
   store the private key with your other secrets. Losing it makes every backup unreadable.
2. **`backup.env`** — on the VPS, `printf 'AGE_RECIPIENT=age1...\n' > backup.env && chmod 600
backup.env` (the public key). The Compose `backup` service reads it as an optional `env_file`;
   without it the stack still starts but every backup fails closed, and deploys block on the
   pre-deploy backup — on purpose.
3. **`backups/` directory** — once, as root: `sudo mkdir -p ~sick-deploy/sick-fansubs/backups &&
sudo chown 1000:1000 ~sick-deploy/sick-fansubs/backups` (the container runs uid 1000). Leave the
   ancestor directories (`~/sick-fansubs`, `~/sick-fansubs/backups`) world-traversable — `0755` is the
   default and `chmod 700` on either breaks the host-side scripts.
4. **MEGAcmd + login** — install MEGAcmd, then once, interactively as `sick-deploy`:
   `mega-login you@backup-account.example 'password'` (the bare command only prints usage). Use the
   dedicated backup-only account, and export its Recovery Key with your secrets. Confirm the push
   shape: `mega-ls /sick-fansubs-backups/` answers, and `mega-put <folder> <remote>` uploads the
   folder's contents recursively. Validate the push by hand with `sh mega-put.sh` once the first
   backup unit exists (the script exits non-zero while `backups/` is empty), and make sure
   `mega-put` is on the cron `PATH` (see the cron gotcha). The login session persists for cron.
5. **Cron** — as `sick-deploy`, two lines (the failure tail re-emits the log to cron's output;
   set `MAILTO` to a monitored address where the host's mail works, otherwise the weekly check is
   the backstop):

   ```cron
   0 3 * * * cd /home/sick-deploy/sick-fansubs && sh daily-backup.sh >> backup-cron.log 2>&1 || { tail -n 30 backup-cron.log >&2; exit 1; }
   30 3 * * 0 cd /home/sick-deploy/sick-fansubs && sh disk-cleanup.sh >> disk-cron.log 2>&1 || { tail -n 30 disk-cron.log >&2; exit 1; }
   ```

   The daily time is host-local (the host stays on Europe/Berlin, so it fires at 04:00
   Europe/Athens); the unit's folder date is computed in Europe/Athens inside the binary. Install
   the logrotate drop-in once so both logs stay bounded, and re-copy it after any change to the
   shipped file:
   `sudo cp ~/sick-fansubs/logrotate.sick-fansubs /etc/logrotate.d/sick-fansubs`. Verify it once:
   `sudo logrotate -d /etc/logrotate.d/sick-fansubs`, then `sudo logrotate -f
/etc/logrotate.d/sick-fansubs` and confirm the rotated names appear.

Rebuild a wiped VPS: re-run [deploy/bring-up.md](../../deploy/bring-up.md), repeat this
section's steps 1–5, then restore the newest backup per [restore.md](restore.md) — a fresh volume
restores directly, because a missing live database has no security state to reconcile. Verify the
rebuild after the first new backup unit: `-status` shows it, `sh mega-put.sh` pushes it, and the
quarterly drill in [restore.md](restore.md) passes end to end.

## Gotchas

- **The deploy-day collision is by design.** Cron publishes today's unit at 03:00; a deploy later
  that day replaces it (`backup-now.sh replace` → `-replace-today -pre-migrate`). If an old deploy
  script ever runs, its preflight fails closed: remove the day's folder —
  `sudo rm -rf /home/sick-deploy/sick-fansubs/backups/YYYY-MM-DD` — and re-run the failed workflow
  jobs (no new tag needed). Any other pre-deploy backup failure (image pull, `backup.env`,
  `backups/` permissions) is fixed the same way: repair the backup, re-run the deploy. A deploy
  that crosses 03:00 inverts the order: the cron waits on the host lock, then fails closed because
  the deploy already published today's unit — push it when the deploy finishes with
  `sh mega-put.sh`.
- **A crash during a deploy-day replace leaves today's unit as `backups/YYYY-MM-DD.previous`.**
  `mega-put.sh` refuses to push while that folder exists — it would silently ship an older unit.
  Resolve by state: if `backups/YYYY-MM-DD/` is missing, move the remnant back
  (`mv backups/YYYY-MM-DD.previous backups/YYYY-MM-DD`); if the date folder exists, the fresh unit
  already published and the remnant is stale — remove it
  (`sudo rm -rf backups/YYYY-MM-DD.previous`). A `YYYY-MM-DD.staging` folder is a crashed run's
  leftover: the next backup clears it itself, so no action is needed unless you want the space
  back (`sudo rm -rf backups/YYYY-MM-DD.staging`).
- **A backup that cannot fsync its directory fails after publishing.** `sync backup directory after
publish` means the unit was renamed but the directory could not be made crash-durable; fix the
  disk (see `df -h`/`dmesg`) and recover by state: the unit is already in place, so a plain re-run
  refuses (`backup folder … already exists`) — remove the date folder
  (`sudo rm -rf backups/YYYY-MM-DD`) or run `sh backup-now.sh replace`. The trigger reports failure
  on purpose — a rename that may not survive a crash is not a backup. A `retention prune failed`
  line is the opposite case: the new unit is complete and published; remove the old folders by hand
  (the warning names the current folder count).
- **Cron runs with a minimal `PATH`.** The example crontab line works when MEGAcmd's `mega-put`
  sits in `/usr/bin` (the Debian package's location); otherwise prepend a `PATH=/…:/usr/bin:/bin`
  assignment to the line. `mega-put.sh` fails with a clear message when the command is missing.
- **A pre-migrate unit carries a prefix history,** because the deploy backup runs before the
  deploy's migration. A daily or shutdown unit carries the exact history. The restore path accepts
  both; the validation rules are [sqlite.md](../patterns/go/sqlite.md)'s.
- **`mega-login` takes the credentials as arguments** — the bare command only prints usage.
- **`AGE_RECIPIENT is required in APP_ENV=production`** means `backup.env` is missing or empty on
  the VPS; it also blocks deploys until fixed.
- **`backup folder … already exists`** means today's unit is already there: a re-run of the cron
  (resolve by hand) or a double cron.
- **A failed push is not a lost backup.** The local unit stays published; fix the Mega session or
  quota and re-run `sh mega-put.sh`.
- **Disk headroom is checked, capped, and reclaimed.** `disk-check.sh` warns under 5 GiB free or
  85% used and fails the trigger under 2 GiB or 95%; it runs at the top of every backup trigger.
  The app and Caddy run with `json-file` log caps (`compose.yaml`), the cron logs rotate through
  the logrotate drop-in, and the weekly `disk-cleanup.sh` prunes exited Compose one-offs, dangling
  images, the build cache, and release tags outside the newest three per repository (the running
  pair always survives). The backups themselves stay at ten units by design — if space runs short,
  prune images first.
- **Plaintext copies hold user data.** Whenever a plaintext unit is created for a drill or a
  restore, delete every copy — on the VPS and on your own machine — once the run is done.

## Pointers

- Index: [../README.md](../README.md)
- Staged restore and the quarterly drill: [restore.md](restore.md)
- Deploy sequence and rollback: [deploy.md](deploy.md); production cutover: [cutover.md](cutover.md)
- SQLite backup/restore rules: [../patterns/go/sqlite.md](../patterns/go/sqlite.md)
- The backup command: [../../cmd/backup/AGENTS.md](../../cmd/backup/AGENTS.md)
- Media manifest contract: [../../internal/media/AGENTS.md](../../internal/media/AGENTS.md)
- VPS provisioning: [../../deploy/bring-up.md](../../deploy/bring-up.md)
