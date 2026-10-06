# Restore

## Purpose

How a backup unit becomes the live database again: the fail-closed staged restore, the quarterly
drill that proves the whole loss-of-VPS path, and the failure semantics when a step of either run
fails. The unit's shape and the backup triggers are [backup.md](backup.md)'s.

## Rules

1. **The unit is complete or the command refuses.** `cmd/restore` takes a plaintext unit — the
   `.db` artifact with its manifest sidecar and media tarball in the same directory — and rejects
   a missing or invalid manifest, a missing tarball, a checksum mismatch, or a filename mismatch
   **before** touching the live database. A DB-only pre-migrate artifact is the migration
   command's rollback path, not this one's; the full staged workflow is
   [sqlite.md](../patterns/go/sqlite.md)'s.
2. **The app is stopped for the whole run.** Restore never overwrites in place: it stages,
   validates, and activates a new file behind the restore marker — readiness reports `not_ready`
   and the application refuses to start while the marker exists. The operator-visible outcomes:
   sessions never survive, post-backup account-security changes refuse activation unless the
   acknowledge-loss override is passed, and the media half extracts before the marker is removed.
   The full workflow is [sqlite.md](../patterns/go/sqlite.md)'s (rule 12).
3. **On the VPS the restore is a one-off container against the stopped stack.** The runner mounts
   the data volume read-write and the unit read-only:

   ```sh
   docker compose stop app
   docker run --rm \
     -v sick-fansubs_sick-data:/app/data \
     -v "$PWD/restore-unit":/unit:ro \
     -e DATA_DIR=/app/data \
     ghcr.io/panosmagic32/sick-fansubs-restore:latest -backup /unit/sick-fansubs.db
   ```

4. **After it exits 0: start, smoke, then clean.** `docker compose up -d`, check readiness and one
   sign-in, and remove the retained `.quarantine-<millis>` rollback set once the restored stack is
   verified. Start the tag paired with the restore image you ran — activation applies that image's
   migration set. After a staged restore that recovers a failed deploy, that is the NEW tag
   (re-run the deploy), never the previous tag a rollback restored.
5. **A media extraction failure after activation keeps the activated database and the marker.**
   The app stays fail-closed and prints a re-run instruction; fix the cause and re-run the same
   command. Extraction is idempotent and the run resumes.
6. **The quarterly drill exercises the full loss-of-VPS path into a scratch volume.** The live
   stack keeps serving throughout; the drill can never touch live data. A real restore over the
   live volume uses the same container with the app stopped (rule 3).

## Pattern

The drill, end to end:

```text
VPS: fetch the newest unit from Mega ─▶ own machine: decrypt with the private key
     (decryption never happens on the VPS) ─▶ VPS: upload the plaintext unit
     ─▶ scratch volume + the one-off restore container ─▶ smoke with the real app
     image against the scratch volume ─▶ delete every plaintext copy
```

## Examples

The drill, command by command:

```sh
# 1. VPS: fetch the newest unit from Mega (or reuse the local ciphertext —
#    the same bytes; the download counts against the account quota).
#    mega-get nests the folder name.
cd ~/sick-fansubs
mega-get /sick-fansubs-backups/<newest-folder> drill-enc/
find drill-enc -name '*.age'

# 2. Own machine: download the three .age files, then decrypt:
mkdir -p sick-drill && cd sick-drill
scp -P <ssh-port> 'sick-deploy@<vps-host>:~/sick-fansubs/drill-enc/<newest-folder>/*.age' .
age -d -i sick-backups.key.txt -o sick-fansubs.db sick-fansubs.db.age
age -d -i sick-backups.key.txt -o sick-fansubs.media.tar sick-fansubs.media.tar.age
age -d -i sick-backups.key.txt -o sick-fansubs.db.media-manifest.json \
  sick-fansubs.db.media-manifest.json.age

# 3. VPS: upload the plaintext unit into ~/sick-fansubs/restore-unit/
ssh -p <ssh-port> sick-deploy@<vps-host> 'mkdir -p ~/sick-fansubs/restore-unit'
scp -P <ssh-port> sick-fansubs.db sick-fansubs.media.tar \
  sick-fansubs.db.media-manifest.json sick-deploy@<vps-host>:~/sick-fansubs/restore-unit/

# 4. VPS: scratch volume + the one-off restore container (the app stays up).
#    An aborted drill can leave the scratch volume AND a smoke container
#    behind — clear both first so the next run starts fresh (the create
#    below is idempotent).
docker rm -f restore-drill-smoke 2>/dev/null || true
docker volume rm -f sick-fansubs-restore-drill || true
docker volume create sick-fansubs-restore-drill
docker run --rm \
  -v sick-fansubs-restore-drill:/app/data \
  -v "$HOME/sick-fansubs/restore-unit":/unit:ro \
  -e DATA_DIR=/app/data \
  ghcr.io/panosmagic32/sick-fansubs-restore:latest -backup /unit/sick-fansubs.db

# 5. Smoke: the real app image against the scratch volume — the strict opener
#    and readiness are what prove the restored file. Run through a compose
#    one-off: it reuses the app service's file secrets (mounted uid 1000,
#    mode 0400), SMTP env, and non-root user. The scratch volume mounts at
#    its OWN path with DATA_DIR pointed at it, so the one-off never depends
#    on same-target mount-override semantics and the live volume stays
#    unused. A plain docker run cannot mount the .env secrets readable by
#    uid 1000 for the sick-deploy operator. Like every compose command, it
#    needs CLOUDFLARE_API_TOKEN in .env (the parse-time guard).
docker compose run --rm -d --name restore-drill-smoke \
  -e DATA_DIR=/app/drill-data \
  -v sick-fansubs-restore-drill:/app/drill-data \
  -p 127.0.0.1:8081:8080 \
  app
for i in $(seq 1 30); do
  curl -fsS http://127.0.0.1:8081/health/ready && break
  sleep 2
done
curl -fsS http://127.0.0.1:8081/api/v1/blog-posts   # → the restored content
docker stop restore-drill-smoke

# 6. Cleanup — the plaintext unit holds user data, remove every copy
docker volume rm sick-fansubs-restore-drill
rm -r ~/sick-fansubs/restore-unit ~/sick-fansubs/drill-enc
ls -la ~/sick-fansubs | grep -E 'restore-unit|drill-enc' || echo 'VPS cleanup clean'
#    …and on your own machine: rm -r ~/sick-drill
```

## Gotchas

- **Run every VPS-side drill step as `sick-deploy`, and check the cleanup output.** Plaintext
  files uploaded or owned by another user cannot be removed by `sick-deploy`'s `rm -r`; the
  failure scrolls by easily and a plaintext unit (user data) can sit on the VPS indefinitely.
  The cleanup ends with the verification line above — anything but the clean echo is a failure.
- **`post-activation step failed … re-run`** means media extraction failed after database
  activation: the marker is retained and the app refuses to start. Fix the cause and re-run the
  same command; extraction is idempotent.
- **A cron backup can fire mid-restore.** It fails closed on the restore marker, and its restart
  attempt is refused while the marker exists (the app cannot start), so expect one failed cron and
  no data risk. Prefer restores outside the 03:00 host-time window.
- **A pre-migrate DB-only artifact is rejected here on purpose.** Its rollback path is the
  migration command, which owns the narrow carve-out for a backup it created moments earlier.
- **Sessions never survive a restore.** Every session is deleted and verified zero before
  activation, so a restored database cannot resurrect a login.
- **A crashed restore resumes only through its own activation record.** A marker written by an
  older build (without the attempt identifier) cannot be resumed by this one; the command says so
  and stops for manual recovery rather than guessing at the on-disk state. The same fail-closed
  rule applies when a crashed migration rollback left `.broken-<millis>` remnants with no live
  database: every opener refuses to create an empty one until the live file is recovered from the
  remnant (or the retained backup) by hand.
- **The artifact is read, never opened in place.** Validation runs on a private staged copy, so a
  read-only unit mount is safe and the unit's directory never grows WAL sidecars.
- **The private age key never reaches the VPS.** Decryption happens on your own machine during the
  drill; the VPS only ever sees ciphertext (for backups) and a transient plaintext unit (for the
  restore itself).

## Pointers

- Index: [../README.md](../README.md)
- Backup lifecycle and units: [backup.md](backup.md)
- Deploy rollback: [deploy.md](deploy.md); production cutover: [cutover.md](cutover.md)
- Staged restore rules: [../patterns/go/sqlite.md](../patterns/go/sqlite.md)
- The restore command: [../../cmd/restore/AGENTS.md](../../cmd/restore/AGENTS.md)
- The migration command's rollback carve-out: [../../cmd/migrate/AGENTS.md](../../cmd/migrate/AGENTS.md)
