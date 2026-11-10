# Deploy

## Purpose

How the application reaches the production VPS: the runtime topology, the artifact boundary, the
tag-driven pipeline, the deploy sequence and its rollback rules, and the secrets the stack needs.
Backup and restore have their own docs.

## Rules

1. **Deployment runs from a version tag through GitHub Actions over SSH.** One workflow builds,
   pushes, and deploys; there is no separate staging path. Every semantic-version tag —
   prerelease or not — deploys to the current stack, so a tag is created deliberately. The workflow
   is `.github/workflows/deploy.yml`; the tag names the application artifact.
2. **The artifact is one application image with the Lit build embedded.** Vite builds the frontend
   before Go compilation and `//go:embed` serves it; Caddy never deploys `web/dist` separately. The
   tag also builds the auxiliary images: `migrate`, `backup`, `restore`, `rebuildsearch`, and
   `caddy` (the custom Caddy image carries the Cloudflare DNS module). Auxiliary images are
   published under both `:latest` and the version tag; the compose services pin `migrate`, `backup`,
   and `caddy` through `AUX_TAG` (`${AUX_TAG:-latest}` in `compose.yaml`, written beside `TAG` and
   reset on rollback by the deploy script). The restore and rebuildsearch one-offs ride their
   runbook's tag. The application image is
   always the tag. It also carries `/app/resetpassword`, the break-glass maintenance entrypoint;
   run it with `--entrypoint /app/resetpassword` while the app is stopped
   (`cmd/resetpassword/AGENTS.md`). A missing or stale frontend build fails packaging instead of
   embedding the wrong files.
3. **Caddy is the only public ingress; the application has no published port.** Caddy terminates
   TLS (Cloudflare DNS challenge with a scoped token) and reverse-proxies to `app:8080`. It
   overwrites forwarding headers and parses `X-Forwarded-For` with `trusted_proxies` +
   `trusted_proxies_strict`, so Go trusts client identity only from the edge. The range list is
   kept set-equal to the `DOCKER-USER` sources in
   [bring-up.md](../../deploy/bring-up.md) §4 (`internal/codestyle` guards the pair). Caddy runs as
   uid/gid 1000 with every capability dropped except `NET_BIND_SERVICE` — kept in the bounding set
   so a rollback to a pre-capability-removal image can exec — and `no-new-privileges`; the image
   removes the binary's file capability, and the container pins the unprivileged-port floor to 0 so
   the bind does not depend on that capability. The port mapping is `443:443`. The
   access log drops the `token` query key, `Referer`, `Cookie`, `X-Csrf-Token`, `Authorization`,
   and `Set-Cookie` — the password-reset link carries a credential, and session/CSRF values must
   not reach container logs.
4. **The stack is one Compose project with one application data volume (plus Caddy's state
   volumes) and two one-shot profiles.** `app` holds
   `sick-data:/app/data`; `caddy` holds `caddy-data`/`caddy-config`; `./backups` is a host bind the
   cron and Mega scripts read. A Caddy volume created before the non-root switch needs a one-time
   root one-off immediately before the first non-root start: from `~/sick-fansubs`, stop Caddy, run
   `docker compose run --rm --no-deps --user root --cap-add CHOWN --cap-add DAC_READ_SEARCH --entrypoint chown caddy -R 1000:1000 /data /config`,
   then start the deploy; its `up -d` starts the non-root Caddy. A fresh volume inherits the image's
   `/data/caddy` and `/config/caddy` state directories, which the image creates as uid 1000, mode 0755.
   The deploy preflight refuses to run while a path in `/data/caddy` or `/config/caddy` is not
   owned by uid 1000, and prints the repair. It inspects the existing caddy container's volumes
   (running or stopped), or the volumes derived from the compose project name after a shutdown; a
   host with both state volumes absent skips it. Ownership is the preflight test; the loopback TLS
   probe (rule 6) is the runtime test for what Caddy can read. When the guard is bypassed (for
   example a manual `docker compose up -d`), a skipped chown shows in the Caddy log as
   `permission denied` on a certificate cache entry and on `last_clean.json`, then as a TLS
   `internal error` at the origin and HTTP 525 through Cloudflare; the container still reads `Up`,
   so container state does not reveal it.

   `migrate` and `backup` run only through their profiles
   (`docker compose --profile migrate run --rm migrate`). Migrations are an explicit maintenance
   step, never run by the app process, and the app only performs the strict read-only compatibility
   check at startup — the mechanics are [sqlite.md](../patterns/go/sqlite.md)'s. A routine deploy
   preserves the volume's sessions and push rows; only a staged restore deletes sessions and rolls
   push rows back to the backup instant.

5. **Configuration is environment variables; secrets are Compose file secrets.** Non-secret config
   (ports, `DATA_DIR`, `APP_ENV`, `PUBLIC_BASE_URL`, `TRUSTED_ORIGIN`, SMTP relay settings) rides
   `environment`; `SMTP_API_KEY` and `VAPID_PRIVATE_KEY` mount from `/run/secrets` for uid 1000
   only, sourced at `docker compose up` time from the owner-only VPS `.env`. `CLOUDFLARE_API_TOKEN`
   is Caddy's. Production fails closed when a required secret is missing. Rotation is an `.env`
   edit plus `docker compose up -d`; for the Cloudflare token, recreate Caddy with the new value
   before revoking the old one, or renewal fails quietly at the next renewal. The variable
   inventory lives in
   [internal/config/AGENTS.md](../../internal/config/AGENTS.md).
6. **The deploy has two phases, and a failed phase never leaves a half-switched stack.** The two
   phases serialize on a host lock with the other backup/deploy triggers (see the lock gotcha).
   1. _Preflight_ — pull the new app image and the version-paired auxiliary images (plus the
      restore image, `:latest`), refuse when Caddy's state directories are not owned by uid 1000
      (rule 4), stop the app (the single-writer window), and take the pre-deploy
      backup (the deploy trigger may replace today's unit; see [backup.md](backup.md)). The backup
      runs on the tag being deployed's paired image, so a re-run after a schema-advanced rollback
      validates the newer live history. A refusal before the stop leaves the running stack
      untouched; any later failure restarts the old stack and aborts with `.env` untouched.
   2. _Deploy_ — run compose on the new TAG/AUX_TAG pair (exported into the shell environment;
      `.env` is committed only after the health gate, so an interrupted deploy keeps the previous
      pair as its rollback target), swap the staged `Caddyfile.new` in (keeping
      `Caddyfile.previous`), run the migrate profile, `up -d`, and wait for the health check: the
      app reports healthy through `/health/ready` **and** the caddy container is Up on two
      consecutive samples **and** a loopback TLS request for the site name answers `200` (one
      failure it catches is rule 4's). The job then fetches the public
      `PUBLIC_BASE_URL/health/ready` through Cloudflare — DNS, the edge, TLS, the origin, and the
      app in one check — issued on the VPS
      over SSH: Cloudflare's free Bot Fight Mode challenges the GitHub runner's shared egress IPs
      and WAF skip rules cannot bypass it, so the probe client is the deploy host's own address
      and the zone keeps the setting on. The Cloudflare SSL mode (Full strict) stays a recorded
      console prerequisite the probe cannot see.

7. **Rollback restores the previous tag and the previous Caddyfile, and stops for a human when the
   schema moved.** An older application artifact rejects newer migration history, so the image swap
   is only a rollback when the failed deploy applied no migration. If the restored stack does not
   become healthy, the schema history most likely advanced: the script prints the STOP and the
   operator decides — staged restore of the pre-deploy backup ([restore.md](restore.md)) or
   fix-forward. The STOP names three causes — a migration applied by the failed deploy, a
   configuration fault, or an edge that fails the loopback TLS probe — and the operator reads the
   compose error and the probe line the rollback printed before choosing. It never loops silently.
   After a staged restore the stack runs the NEW tag (re-run
   the deploy): activation applies the restore image's migration set, so the previous tag would
   refuse the restored history. The choice: with no migration applied and a healthy edge, the image
   rollback is the whole recovery; an unreadable Caddy state survives the rollback and is repaired
   by rule 4; with the schema advanced, a staged restore trades away every write since
   the pre-deploy backup while fix-forward keeps them — decide by whether those writes are worth
   preserving.
8. **Health and smoke.** `/health/live` reports process liveness; `/health/ready` carries the
   strict readiness contract ([sqlite.md](../patterns/go/sqlite.md) rule 10) and answers `not_ready`
   while a restore marker exists. After a deploy: readiness, one public read, the deployed version
   and the applied migration version, then watch the logs for the agreed interval.
9. **CI holds least privilege.** Third-party actions are pinned to commit SHAs with the release in
   a trailing comment; checkouts set
   `persist-credentials: false`; permissions are `contents: read`, with `packages: write` on the
   build job alone (the one job that pushes images); deploy
   credentials are repository secrets and variables used only by the deploy job (`PUBLIC_BASE_URL`
   feeds the public-edge probe); the SSH connection
   pins the host key and fails on password prompts. A `deploy-approval` GitHub environment with
   required reviewers — created in repository settings — gates the `deploy` job. Job timeouts are
   hang-brakes around the deploy concurrency group, not cost policy; the deploy ceiling sits above
   the expected worst case (the script prints per-phase elapsed times, and the ceiling is revisited
   once real timings are recorded). Never echo `.env`, connection strings, keys, user records,
   or backup contents into logs or job output.
10. **The stack is production; there is no second environment.** Local development is
    `make dev-local` (Go plus Vite, no Docker, disposable SQLite); it never uses production
    secrets. VPS provisioning and the from-scratch rebuild path live in
    [deploy/bring-up.md](../../deploy/bring-up.md) and [backup.md](backup.md).
11. **Every deployed name is `sick-fansubs`.** The VPS directory (`~/sick-fansubs`), the Compose
    project (`name:` in [compose.yaml](../../compose.yaml)) and its named volumes, the deploy
    script, the lock file, the runbooks, and the cron line carry the product name.

## Pattern

The pipeline and the on-VPS sequence:

```text
tag push ─▶ test job (Go + web) ─▶ build job (6 images → ghcr) ─▶ deploy job
                                                                      │
                                          scp: compose.yaml, Caddyfile.new,
                                               scripts, backup and restore runbooks
                                          ssh: sh deploy.sh  (NEW_TAG=…)
                                                                      │
        preflight: pull ─▶ state check ─▶ stop app ─▶ backup ─────────┤
        deploy:    run new pair ─▶ swap Caddyfile ─▶ migrate ─▶ up -d ─▶ healthy?
                                                                      │
                              no ─▶ rollback (previous TAG + Caddyfile) ─▶ healthy?
                                                                      │
                                                    no ─▶ STOP: repair edge, staged restore, or fix-forward
```

## Examples

- Pipeline and artifact builds: [`.github/workflows/deploy.yml`](../../.github/workflows/deploy.yml).
- On-VPS sequence: [`deploy/deploy.sh`](../../deploy/deploy.sh).
- Topology: [`compose.yaml`](../../compose.yaml); edge config: [`Caddyfile`](../../Caddyfile).
- Local commands (dev, migrate, backups): `make help`.

## Gotchas

- **`docker compose pull` honors the old `TAG`, and `docker compose run` pulls only missing
  images.** The preflight pulls auxiliary images directly by name for that reason; keep that shape
  when editing the script.
- **A forged `X-Forwarded-For` must not change the resolved client.** `internal/middleware`
  deliberately keeps no Go-side allowlist because the app publishes no port and Caddy replaces the
  header with the address it resolved through `trusted_proxies` (`header_up X-Forwarded-For
{http.request.client_ip}`); a client-supplied value is discarded. Re-probe after any edge change,
  asserting the resolved address is the true client (not the Cloudflare edge).
- **`.env` names only a healthy deploy.** The deploy phase exports the new pair to compose and
  commits it to `.env` after the health gate, so a failed or interrupted deploy leaves the previous
  pair in place — "re-run failed jobs" still computes the right rollback target. Rollback restores
  the previous pair.
- **All backup/deploy triggers serialize on `~/sick-fansubs/.lock`.** `flock` (util-linux) is a VPS
  dependency; a deploy waits for a running cron backup and vice versa, and a killed process
  releases the lock. The scripts fail closed when `flock` is missing. A long-lived daemon started
  inside a trigger can inherit the lock descriptor and keep the lock after the script exits;
  `lslocks | grep sick-fansubs` names the holder — see the MEGAcmd gotcha in [backup.md](backup.md).
- **`SICK_LOCK_HELD` is a reserved environment variable.** The lock helper exports it so a nested
  `backup-now.sh` does not re-lock; never export it by hand — a trigger launched with it set skips
  the mutual exclusion. The advisory lock file is owned by `sick-deploy`, and a signal during the
  lock wait exits before any state change.
- **An interrupted deploy recovers from the job log.** `deploy.sh` traps HUP/INT/TERM for a
  best-effort rollback (a stack restart before the deploy phase, a rollback after it began) and
  prints per-phase elapsed times. SIGKILL — the job timeout — and a VPS reboot cannot be trapped:
  read the log, then re-run the failed job; the re-run recomputes the previous pair from `.env`
  and completes or rolls back. Run `docker compose up -d` first only when no migration may have
  applied — after a possible migration the previous tag refuses the newer history, so go straight
  to the re-run (its preflight backup runs on the new tag's paired image and covers that history).
- **The app's graceful shutdown needs more than Docker's default stop grace.** The service sets
  `stop_grace_period: 45s` so `docker compose stop` (backup and deploy preflight) and `up -d`'s
  recreation let the 30 s HTTP drain, the cleanup join, and the explicit pool close finish instead
  of being SIGKILLed at the 10 s default.
- **A missing `SMTP_API_KEY` fails both the deploy and its rollback at Compose level** — a
  compose-level refusal is configuration, not schema drift. Fix `.env`, `up -d`, re-run.
- **A missing `CLOUDFLARE_API_TOKEN` refuses every compose command at parse time.** The compose
  `:?` guard fails deploys, backups, shutdowns, and the drill's smoke one-off alike — the token
  must be present in `.env` for any compose command, not just for Caddy to start.
- **Deploy-day backup collision:** the cron may already have published today's unit; only the
  deploy trigger replaces it. See [backup.md](backup.md).
- **Every backup trigger checks disk headroom first.** `disk-check.sh` gates each trigger; the
  thresholds and the weekly `disk-cleanup.sh` sweep are [backup.md](backup.md)'s.
- **The public-edge probe needs the `PUBLIC_BASE_URL` repository variable.** It holds the site's
  base URL (the `sickfansubs.com` apex); when unset the deploy job fails after the VPS side
  already deployed. The probe itself runs on the VPS over SSH (the deploy key and pinned host key
  are its transport): the zone's Bot Fight Mode
  challenges the runner's shared egress IPs and no WAF rule can skip it, so Bot Fight Mode
  stays on and the probe client is the deploy host's address.
- **Repository variables are read when the job is created, not when it runs.** A deploy job
  parked at the `deploy-approval` gate keeps the `vars.*` values from its creation: after a
  variable edit, re-run the job for the new value to apply — the live failure is an SSH
  `Permission denied (publickey)` against the previous `DEPLOY_USER`.
- **The workflow ships the scripts with every deploy.** Never hand-edit a script on the VPS — the
  next deploy overwrites it, and the versioned copy in `deploy/` is the contract.
- **CI and `make check` are deliberately asymmetric.** The workflow test jobs run `go build ./...`
  and a frozen-lockfile `bun install` beyond `make check`; the local gate omits the build (vet
  type-checks the same packages). `docs-lint` runs in both, so the one guard no deploy may skip is
  covered. The PR/push suite (`ci.yml`) and the tag gate (`deploy.yml`'s test job) mirror each
  other — a suite member added to one belongs in the other and in `make check`.
- **The Docker build cache shows its verdict in the build log.** A `Dockerfile*` or base-image change
  pays a cold build; read the build step's log at that tag to confirm the gha cache still restores
  (the caddy image is the canary — minutes cold, seconds cached).
- **No CPU/memory limits are set.** The stack runs uncapped deliberately until the VPS baseline is
  measured (`free -m`, `docker stats --no-stream`); the cap decision (~2–3× observed peak) is
  recorded with that measurement, not guessed.
- **The deploy trigger is deliberately broad; a workflow step enforces the tag scheme.** GitHub's
  tag-filter `[]` classes admit only alphanumerics and `a-z`/`A-Z`/`0-9` ranges — a literal dash or
  dot inside one invalidates the whole workflow file, and GitHub silently drops an invalid file
  (no listing, no runs). `deploy.yml` therefore triggers on `[0-9]*`; its test job's first step
  accepts only `<major>.<minor>.<patch>[-<prerelease>]` (alnum, dash, dot), which also keeps shell
  metacharacters out of the deploy step's `NEW_TAG='…'` interpolation.
- **New ghcr packages default to private.** Set each package's visibility before the first
  anonymous pull; the six image names are `sick-fansubs`, `sick-fansubs-migrate`,
  `sick-fansubs-backup`, `sick-fansubs-restore`, `sick-fansubs-rebuildsearch`, and
  `sick-fansubs-caddy`.
- **Caddy carries no Compose healthcheck.** The deploy gate proves the edge instead: two `Up`
  samples and a loopback TLS request for the site name. A hand-run `docker compose up -d` gets
  neither, so run the loopback probe after a manual start.
- **The Caddyfile is not validated before the migrate profile runs.** The deploy swaps the staged
  file in, migrates, then `up -d`; Caddy rejects a bad configuration at that start, and the
  rollback restores the previous Caddyfile and image pair. The file is small and reviewed, and the
  rollback keeps a bad configuration from staying in front of the stack — an accepted hold.
- **`compose.yaml` and the Dockerfiles are exercised, not linted.** CI lints the workflows and the
  shell scripts, and the build job compiles every image; Compose parses its file at deploy time.
  A syntax error therefore fails the build or the deploy (with rollback), not a pre-merge lint —
  an accepted hold.

## Pointers

- Index: [../README.md](../README.md)
- Backup lifecycle: [backup.md](backup.md); staged restore: [restore.md](restore.md)
- Dependency currency: [currency.md](currency.md)
- SQLite foundation rules: [../patterns/go/sqlite.md](../patterns/go/sqlite.md)
- Environment variables: [../../internal/config/AGENTS.md](../../internal/config/AGENTS.md)
- App runtime, health endpoints, embedded frontend: [../../cmd/api/AGENTS.md](../../cmd/api/AGENTS.md)
- VPS provisioning runbook: [../../deploy/bring-up.md](../../deploy/bring-up.md)
