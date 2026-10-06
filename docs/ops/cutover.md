# Cutover

## Purpose

How the beta stack becomes production: the maintenance window, the data freeze and import, the
edge swap, the rollback boundaries, and what closes the legacy system out. Deploy, backup, and
restore mechanics live in their own docs.

## Rules

1. **Cutover promotes the running stack; it does not change the deploy path.** The beta and
   production are the same Compose stack and the same tag workflow ([deploy.md](deploy.md)). The
   repository swap ([repo-swap.md](../../deploy/repo-swap.md)) precedes the maintenance window;
   the tag push below is the deploy. What changes is the edge: Caddy becomes the sole ingress for
   `sickfansubs.com` on 443 and the legacy
   nginx retires. The flip's file-by-file change list is part of this rule:
   - Sequence, inside the maintenance window — stop the legacy nginx first (it holds 80/443);
     reinstall the `DOCKER-USER` blocks for 443 from the edited
     [bring-up.md](../../deploy/bring-up.md) §4 (below), so direct origin 443 is gated the moment
     the flipped stack binds it; point the Cloudflare apex at the origin (proxied, Full strict) and
     remove the `v2` Origin Rule and the `v2` DNS record; set the repository variable
     `PUBLIC_BASE_URL` to the apex (the deploy job's probe reads it and would otherwise fail after
     a healthy deploy); then push the flip tag. That order gives the probe a working public URL
     the moment the flipped stack is up.
   - `Caddyfile` — the site address becomes `sickfansubs.com` (plus the `www` redirect once the DNS
     facts are collected), and the site block gains
     `header Strict-Transport-Security "max-age=31536000"` (no preload; `includeSubDomains` only
     once every subdomain is HTTPS).
   - `compose.yaml` — the Caddy port mapping becomes `443:443` (replacing `8443:443`); port 80
     stays unpublished; `PUBLIC_BASE_URL` and `TRUSTED_ORIGIN` become the apex; the banner
     comment's port sentence follows.
   - The VPS firewall — edit [bring-up.md](../../deploy/bring-up.md) §4 to the 443 gate (block
     comments and every `--ctorigdstport`) and sweep the whole runbook clean of the retired edge:
     every 8443 / `v2`-record / legacy-ingress mention and the executed rename migration
     (Prerequisites, certificate copy, acceptance checks, legacy-container notes), then reinstall
     the blocks on the VPS from it, so a post-flip rebuild cannot reinstall the retired edge and
     non-Cloudflare clients cannot reach the origin on 443.
   - Rollback boundary — the deploy script restores the previous TAG and Caddyfile, never
     `compose.yaml`: the flip tag deploy runs the flipped compose file on every path, failure and
     rollback included, and a failed flip is reverted by hand (compose file, firewall blocks,
     Cloudflare incl. the `PUBLIC_BASE_URL` repository variable, nginx) with the maintenance
     window still open.
   - Verification — readiness, a password-reset link's host, a real sign-in, the HSTS header, the
     probe URL, and non-Cloudflare 443 refused.
2. **Announce the maintenance window and the one-time logout.** Sessions do not survive the
   cutover, so the announcement names the logout; the cutover GitHub Release carries the
   hand-written notes (what changed from legacy). Per-beta releases are skipped.
3. **Freeze legacy writes for the import window, and snapshot the source first.** Take the final
   legacy export and record its identity; from then on Atlas is read-only and the target SQLite
   database is the only writer.
4. **Import into a fresh production volume with the runbook's legs, in its dependency order.**
   Every leg prints a reconciliation report: compare the counts with the rehearsal and stop on any
   unexplained difference. The commands, the order, and the expected numbers are
   [deploy/data-import.md](../../deploy/data-import.md)'s.
5. **Back up the target database before it serves writes.** The unit that protects the cutover is
   created by the same backup path production will use ([backup.md](backup.md)), and a restore is
   drill-tested before the window ([restore.md](restore.md)).
6. **Deploy the target artifact and smoke it.** Readiness (`/health/ready`), one public read, and a
   real sign-in with a production password — the sign-in proves the imported credentials.
7. **Keep legacy and Atlas unchanged for the agreed rollback window.** Returning to legacy is safe
   only while no target-only writes would be lost. Once users write to SQLite, the rollback paths
   are the staged restore of the pre-cutover backup or fix-forward — a redeploy of the old stack is
   not one of them once migration history has advanced ([deploy.md](deploy.md) rule 7,
   [restore.md](restore.md)).
8. **When the window closes, remove legacy and MinIO.** Media objects are migrated once by the
   media leg; there is no two-phase URL window and no running MinIO dependency afterwards.
9. **Reconcile sessions and accounts deliberately.** Legacy sessions are excluded by design and a
   post-cutover sign-in is part of the smoke; the restore path reconciles post-backup
   account-security changes, so neither side quietly keeps access.
10. **Cutover readiness is demonstrated, not assumed.** Every in-scope capability has accepted
    target tests; the media-reference conformance query (Examples) returns zero rows against the
    database that will serve production; the full rehearsal reconciled production-shaped data;
    backup and restore, and the applicable rollback path — including an interrupted deploy and the
    restore-after-failed-deploy TAG chain — have been rehearsed on the VPS; and the data-rollback
    boundaries are explicit before the window opens.
11. **No beta-era name survives the flip.** Every deployed name is `sick-fansubs`; the cutover
    window carries no rename step, and the rule 10 readiness gate confirms the live directory,
    Compose project, volumes, and crontab all carry the new name.
12. **The flip retires the working-state scratch tree.** The flip deletes the whole `.scratch/`
    tree and `.gitignore` gains `.scratch/`, so a later local rebuild cannot republish it; the
    root `AGENTS.md` and `README.md` drop their scratch pointers in the same pass, and no living
    doc keeps a pointer into the deleted tree (the review workspace's rules are
    [release-review.md](release-review.md) rule 11).

## Pattern

```text
announce (logout) ─▶ freeze legacy writes ─▶ final export + identity
   ─▶ fresh volume: migrate ─▶ import (users → content → search → media → avatars → favorites)
   ─▶ reconcile (stop on unexplained mismatch) ─▶ backup the target ─▶ deploy + smoke
   ─▶ serve, monitor ─▶ rollback window open (legacy + Atlas untouched)
   ─▶ window closes: remove legacy + MinIO, close the record
```

## Examples

- The pre-cutover media-reference conformance query (rule 10), run against the database that will
  serve production — one row per stored `media/…` reference outside the canonical
  `media/images/<id[:2]>/<id>.<ext>` shape, expected zero (legacy absolute URLs are not storage
  references and are not returned):

  ```sql
  WITH refs(source, id, reference) AS (
    SELECT 'blog_posts', id, thumbnail_url FROM blog_posts
    UNION ALL
    SELECT 'projects', id, thumbnail_url FROM projects
    UNION ALL
    SELECT 'users', id, avatar_url FROM users
  )
  SELECT source, id, reference FROM refs
  WHERE reference LIKE 'media/%'
    AND NOT (
      length(reference) = 52
      AND substr(reference, 1, 13) = 'media/images/'
      AND substr(reference, 14, 2) = substr(reference, 17, 2)
      AND substr(reference, 14, 2) NOT GLOB '*[^0-9a-f]*'
      AND substr(reference, 17, 32) NOT GLOB '*[^0-9a-f]*'
      AND substr(reference, 49) IN ('.jpg', '.png')
    );
  ```

  A returned row is a reference the write gate now refuses: the sweep and the delete guard resolve
  the canonical path while the row points elsewhere. The file itself sits at the canonical path —
  every processed image is stored there — so the repair is a one-time rewrite of the stored
  reference to that path before the window opens.

- Provisioning or rebuilding the VPS: [deploy/bring-up.md](../../deploy/bring-up.md).
- Deploy and rollback of the artifact: [deploy.md](deploy.md).

## Gotchas

- **The warm-up and readiness order matters.** The application refuses to serve while a restore
  marker exists; finish any restore fully before the promoted stack is expected to answer.
- **Imports are not a repair path, and media parity is not perfection.** The content legs are not
  idempotent into the same volume — a clean redo is a fresh volume (the import runbook's rule) —
  and known-dead legacy thumbnails stay dead; their rows keep the legacy URL until re-uploaded
  through the admin UI.
- **The rollback window is a data decision, not a button.** Whoever closes it must confirm that no
  target-only writes are worth preserving — after that point the only honest paths are restore or
  fix-forward.

## Pointers

- Index: [../README.md](../README.md)
- Deploy and rollback: [deploy.md](deploy.md)
- Backup units: [backup.md](backup.md); staged restore: [restore.md](restore.md)
- Data import runbook: [../../deploy/data-import.md](../../deploy/data-import.md)
- Repository swap: [../../deploy/repo-swap.md](../../deploy/repo-swap.md)
- VPS provisioning: [../../deploy/bring-up.md](../../deploy/bring-up.md)
- Migration mechanics: [../patterns/go/sqlite.md](../patterns/go/sqlite.md)
