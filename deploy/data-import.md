# Production Data Import (owner-executed)

> Cutover and rehearsal runbook: import the frozen legacy export into a fresh
> production volume.
>
> **SSH alias:** the owner reaches the VPS through an SSH alias. Replace
> `<user>@<vps-ip>` with the alias name and drop `-P 32122` throughout.
> The files below must land in the **sick-deploy** user's home
> (`/home/sick-deploy/sick-fansubs/`).
>
> **Alias-user gotcha:** the owner's scp alias logs in as a DIFFERENT user
> than the ssh alias. After the scp, check `whoami` on the VPS and, if the 5
> files are not in `/home/sick-deploy/sick-fansubs/`, move them across (from
> the personal shell, which has sudo):
>
> ```sh
> sudo mv ~/sick-fansubs/sick-fansubs-*.tar.gz ~/sick-fansubs/sick-db.*.json \
>         /home/sick-deploy/sick-fansubs/
> ```
>
> Ownership can stay with the personal user — the files are 644 and are
> deleted after the run. Deletion needs write on the DIRECTORY: run the
> cleanup `rm` as sick-deploy (dir owner) or with sudo, and use `rm -f` (a
> plain `rm` prompts for each write-protected file).

## What this does

Runs `cmd/importdata` (`-kind users|blog-posts|projects|favorites`) and
`cmd/rebuildsearch` as one-off containers against the named volume
`sick-fansubs_sick-data`, with the app stopped (single-writer). Import into a
**fresh volume**: the legs are not idempotent, because existing rows win on
canonical-key collisions (per-record rejections) and a partial re-run leaves
an inconsistent database. The frozen legacy export is read-only during the run.

## Hygiene

The export files hold real bcrypt hashes and emails: owner-only handling,
never committed (`data/` is gitignored), `chmod 600`, deleted from the VPS
after the run. The printed reconciliation reports are sanitized (counts,
IDs, concise reasons — never emails or hashes) and are safe to share.

## 1. Build + ship the images (LOCAL machine)

```sh
make docker-build-importdata
make docker-build-rebuildsearch
docker save sick-fansubs-importdata:latest | gzip > sick-fansubs-importdata.tar.gz
docker save sick-fansubs-rebuildsearch:latest | gzip > sick-fansubs-rebuildsearch.tar.gz
scp sick-fansubs-importdata.tar.gz sick-fansubs-rebuildsearch.tar.gz \
    data/legacy-app-data/sick-db.users.json \
    data/legacy-app-data/sick-db.blogposts.json \
    data/legacy-app-data/sick-db.projects.json \
    <alias>:~/sick-fansubs/
```

## 2. Load + import (VPS, as sick-deploy)

```sh
cd ~/sick-fansubs
gzip -dc sick-fansubs-importdata.tar.gz | docker load
gzip -dc sick-fansubs-rebuildsearch.tar.gz | docker load
chmod 644 sick-db.*.json        # readable by the uid-1000 containers
docker volume ls | grep sick-fansubs   # confirm: sick-fansubs_sick-data

docker compose stop app         # single-writer

docker run --rm -v sick-fansubs_sick-data:/app/data \
  -v "$PWD/sick-db.users.json":/import.json:ro \
  -e DATA_DIR=/app/data -e IMPORT_FILE=/import.json \
  sick-fansubs-importdata:latest -kind users
# Same shape for -kind blog-posts, then -kind projects (order matters:
# users first — creator/updater references resolve against them).

# The rebuildsearch image declares the uid-1000 app user (the migrate
# pattern); the explicit pin is harmless.
docker run --rm -u 1000:1000 -v sick-fansubs_sick-data:/app/data \
  -e DATA_DIR=/app/data sick-fansubs-rebuildsearch:latest

docker compose up -d
docker compose ps               # app → healthy
```

Each import prints its reconciliation report to stdout — compare against
the expected numbers (users 38 imported / 1 rejected with the
canonical-username reason; blog 333/0; projects 22/0; warnings are
inventory, not failures).

## 3. Verify

- `docker compose ps`: app healthy.
- Browser on `https://sickfansubs.com`: the blog list and projects list
  show the legacy content; a Greek search returns hits; sign in with a
  production password; the footer shows the deployed version.
- **Count check:** walk the public lists and expect blog 333 / projects 22 —
  `grep -o '"title":' | wc -l` per page (no jq on the VPS). A count of 0 on
  either list means that import never landed; the slug-protected projects
  re-run is the safe repair (fails fast on UNIQUE(slug) if the rows are
  already there).
- Thumbnails are fetched/imported by the media follow-up below (external-host thumbnails — postimg/Discord — are the majority); favorites are imported by §4b.

## 4. Media follow-up (thumbnails — owner-executed)

The production thumbnails live on three hosts: 264 `i.postimg.cc`, 37
`cdn.discordapp.com`, and 54 legacy `sickfansubs.com/media/images/...`.
The fetch joins the import over the URL-derived key — dead objects
(deleted Discord attachments, postimg takedowns) are counted as `notFound`,
never fatal, and their rows keep the legacy URL (re-upload through the
admin UI later).

**LOCAL machine** (exports + network access exist here; re-runs are
idempotent):

```sh
make fetch-media-local IMPORT_FILE=data/legacy-app-data/sick-db.blogposts.json KIND=blog-posts
make fetch-media-local IMPORT_FILE=data/legacy-app-data/sick-db.projects.json KIND=projects
# Both report JSON to stdout; the counts tell us the dead-link inventory.
tar czf sick-fansubs-media-source.tar.gz -C media-source .
make docker-build-importmedia
docker save sick-fansubs-importmedia:latest | gzip > sick-fansubs-importmedia.tar.gz
scp sick-fansubs-media-source.tar.gz sick-fansubs-importmedia.tar.gz <alias>:~/sick-fansubs/
```

**VPS** (as sick-deploy, app stopped for the import — single-writer):

```sh
cd ~/sick-fansubs
gzip -dc sick-fansubs-importmedia.tar.gz | docker load
mkdir -p media-source && tar xzf sick-fansubs-media-source.tar.gz -C media-source
chmod -R a+rX media-source       # readable by the uid-1000 container

docker compose stop app

docker run --rm -v sick-fansubs_sick-data:/app/data \
  -v "$PWD/media-source":/media-source:ro \
  -e DATA_DIR=/app/data -e MEDIA_SOURCE_DIR=/media-source \
  sick-fansubs-importmedia:latest

docker compose up -d
docker compose ps               # app → healthy
```

The import report prints processed/rewritten/failed counts — failed rows
= objects the fetch could not download. **The one-off `docker run` exits 1
when ANY object failed** — dead external links are expected, so read the
report, not the exit code. Verify: blog list + detail pages render
thumbnails for every row except the counted dead links. Re-runs are
idempotent (a re-run over a rewritten DB scans zero legacy rows).

**Interruption is safe:** the import commits per object, so a Ctrl+C'd run
leaves valid partial rewrites — a re-run completes the remainder.

**Dead-link recovery:** the 37 Discord-CDN thumbnails 404 (the attachments
were deleted; the legacy site hotlinks the same dead URLs — parity, not a
migration loss). List the rows still carrying an absolute URL after the
import, and re-upload a thumbnail through the admin UI for each:

```sh
# on the VPS, e.g. via the app's volume (or any sqlite shell):
SELECT id, title FROM blog_posts WHERE thumbnail_url NOT LIKE 'media/%';
```

(Remember the header's alias-user scp gotcha: the scp alias may log in as
a different user than the ssh alias — after the scp, check `whoami` and
move the two tarballs into `/home/sick-deploy/sick-fansubs/` if needed.)

## 4a. Avatar follow-up (owner-executed)

The nine production avatars are clean legacy MinIO URLs
(`https://sickfansubs.com/media/images/<ts>-<uuid>.<ext>`) — the same
object-key join as the legacy-host thumbnails. The users import deferred
avatars to NULL, so the join comes from the users EXPORT (not the DB).

**Sequencing:** run this leg right after the users import, before the
sweep's next pass — the sweep scans `users.avatar_url`, so a NULL column
would leave the imported avatar objects orphaned, and the profile endpoint
serves the rewritten absolute URL.

**LOCAL machine** (re-runs idempotent):

```sh
make fetch-media-local IMPORT_FILE=data/legacy-app-data/sick-db.users.json KIND=users
# 9 distinct URLs expected; the report prints the counts.
# The media-source dir already holds the blog/projects objects — the
# avatar keys join the same directory, so ship it together:
tar czf sick-fansubs-media-source.tar.gz -C media-source .
make docker-build-importavatars
docker save sick-fansubs-importavatars:latest | gzip > sick-fansubs-importavatars.tar.gz
scp sick-fansubs-media-source.tar.gz sick-fansubs-importavatars.tar.gz \
    data/legacy-app-data/sick-db.users.json <alias>:~/sick-fansubs/
```

**VPS** (as sick-deploy, app stopped — single-writer):

```sh
cd ~/sick-fansubs
gzip -dc sick-fansubs-importavatars.tar.gz | docker load
mkdir -p media-source && tar xzf sick-fansubs-media-source.tar.gz -C media-source
chmod -R a+rX media-source
chmod 644 sick-db.users.json

docker compose stop app

docker run --rm -v sick-fansubs_sick-data:/app/data \
  -v "$PWD/media-source":/media-source:ro \
  -v "$PWD/sick-db.users.json":/import.json:ro \
  -e DATA_DIR=/app/data -e MEDIA_SOURCE_DIR=/media-source -e IMPORT_FILE=/import.json \
  sick-fansubs-importavatars:latest

docker compose up -d
docker compose ps               # app → healthy
```

Expected report: exportRecords 39, usersWithLegacyAvatars 9, matchedUsers
9, processed 9, failed 0, rowsRewritten 9. **Skip-if-set caveat:** the match
requires `avatar_url IS NULL`, so an account that self-uploaded an avatar
before this import is skipped silently — matched/processed drop by one,
`failed` stays 0, and the end state is still complete. Verify: the account
header chip and the `/account` profile show avatars for the nine accounts;
every other account still falls back to the initial letter. Re-runs are
idempotent (a re-run scans zero rows — `avatar_url IS NULL` is the match).
The one-off `docker run` exits 1 when any object failed — read the report,
not the exit code.

## 4b. Favorites import (owner-executed)

The legacy favorites are ObjectId arrays ON the user documents
(`favoriteBlogPostIds` / `favoriteProjectIds`) — 11 blog favorites across
6 users, 7 project favorites across 2 users (counts only; 0 dangling
references). The import reads the USERS export and resolves each entry
through the PRESERVED user ObjectId and the DERIVED content id, so it MUST
run after users + content, into a fresh volume (see §Rollback).

**VPS** (as sick-deploy, app stopped — single-writer):

```sh
docker compose stop app

docker run --rm -v sick-fansubs_sick-data:/app/data \
  -v "$PWD/sick-db.users.json":/import.json:ro \
  -e DATA_DIR=/app/data -e IMPORT_FILE=/import.json \
  sick-fansubs-importdata:latest -kind favorites

docker compose up -d
docker compose ps               # app → healthy
```

Expected report (`favorites` section): blog `users` 6, `sourceRows` 11,
`imported` 11; projects `users` 2, `sourceRows` 7, `imported` 7;
`userMissing`/`contentMissing`/`alreadyPresent` all 0. Re-runs are
idempotent (existing rows count as `alreadyPresent`). Verify: sign in as a
user with legacy favorites — the detail-page hearts and the account
favorites lists show them, with the newest array entry first (the
`updatedAt`-anchor step-back preserves the legacy order).

## 5. Cleanup

```sh
rm sick-db.*.json sick-fansubs-*.tar.gz media-source -rf
```

## Rollback

Before the stack serves production writes, the clean redo is a fresh volume:
`down`, remove the volume, migrate, then re-run the full leg order. Once
production writes land, that window is closed — the rollback paths are the
staged restore of the pre-cutover backup or fix-forward
([cutover.md](../docs/ops/cutover.md) rule 7).

```sh
docker compose down
docker volume rm sick-fansubs_sick-data
docker compose --profile migrate run --rm migrate
# then the full leg order:
# users → blog-posts → projects (order matters), rebuildsearch, up -d,
# then §4 (media), §4a (avatars), §4b (favorites)
```

Do NOT "re-run the imports" into an existing DB as a fix: the users import
rejects duplicates per-record (safe), but the blog import DERIVES the same
target IDs on every run — the first INSERT hits the primary key and the
command fails fast instead of duplicating, and a partial re-run would leave
an inconsistent database (projects are protected by UNIQUE(slug)). The clean
redo is the fresh volume above.
