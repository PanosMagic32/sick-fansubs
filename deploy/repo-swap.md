# Repository Swap (owner-executed)

## Purpose

Replace the public `PanosMagic32/sick-fansubs` repository with one parentless release commit of this
application, recreate the GitHub settings its workflows need, and retire the development repository
after a verified flip. One-time procedure: it runs once, for the 2.0.0 cutover.

## Rules

1. **One parentless commit, no retained history.** The public repository's `main` becomes a single
   commit whose tree is this repository's `main`; the accepted loss is that old commit links die
   (issues and pull requests remain).
2. **A branch push never deploys.** `deploy.yml` triggers on version tags only, so the swap and the
   settings work happen outside the maintenance window; pushing the `2.0.0` tag is the deploy
   ([cutover.md](../docs/ops/cutover.md) rule 1).
3. **The ghcr access grant is part of the swap.** Each of the five `sick-fansubs*` packages must
   grant the new repository Actions write access, or the build job dies
   `denied: permission_denied: write_package`.
4. **The approval gate exists before the first deploy.** Create the `deploy-approval` environment
   with a required reviewer before pushing the tag: a referenced-but-absent environment is created
   unprotected, and the deploy would not wait.
5. **Retire the old repository only after a verified flip.** Development continues in the new
   repository from then on.

## The flip edits

Commit these in this repository, with `make check` green, immediately before the swap:

- `Caddyfile` — the site address becomes `sickfansubs.com`, the `www` redirect joins once the DNS
  facts are collected, and the site block gains
  `header Strict-Transport-Security "max-age=31536000"` (no preload; `includeSubDomains` only once
  every subdomain is HTTPS).
- `compose.yaml` — the banner comment follows the new edge; the Caddy port mapping becomes
  `443:443`; `PUBLIC_BASE_URL` and `TRUSTED_ORIGIN` become `https://sickfansubs.com`.
- `.github/workflows/deploy.yml` — the `deploy` job gains `environment: deploy-approval`.
- `.gitignore` — gains `.scratch/`; the `.scratch/` tree is deleted.
- `AGENTS.md` — drops the `.scratch/SESSION-STATE.md` starting read and the working-state section.

## Pattern

```sh
# 1. Build the parentless release commit from the flip commit's tree.
#    Run once; re-running commit-tree mints a different commit.
release=$(git commit-tree 'main^{tree}' -m 'Sick-Fansubs 2.0.0')
git branch -f release "$release"
git tag 2.0.0 "$release"      # pushed later, inside the window

# 2. Replace the public main, then prune every stale ref the repository still carries.
git push --force git@github.com:PanosMagic32/sick-fansubs.git release:main
git ls-remote git@github.com:PanosMagic32/sick-fansubs.git
# delete every ref the listing still shows besides refs/heads/main, one per command:
git push git@github.com:PanosMagic32/sick-fansubs.git --delete <ref> ...
```

## Settings in the new repository

- Secrets: `DEPLOY_HOST`, `DEPLOY_KEY`, and `DEPLOY_HOST_KEY`.The host-key pin lives in secrets so the origin address stays out of the
  world-readable variables.
- Variables: `DEPLOY_PORT`, `DEPLOY_USER`, and `PUBLIC_BASE_URL=https://sickfansubs.com`.
- Environments: `deploy-approval` with the owner as required reviewer.
- Package access, one grant per package — repository `PanosMagic32/sick-fansubs`, role Write, for
  `sick-fansubs`, `sick-fansubs-migrate`, `sick-fansubs-backup`, `sick-fansubs-restore`, and
  `sick-fansubs-caddy`. The packages stay public: the VPS pulls anonymously.

## After the verified flip

```sh
git remote set-url origin git@github.com:PanosMagic32/sick-fansubs.git
```

Then delete `PanosMagic32/sick-v2` on GitHub, and close the cutover record.

## Gotchas

- **A forced `main` leaves old branches, tags, and Releases in place.** Prune the refs from the
  `git ls-remote` listing, and delete or retarget the stale Releases in the UI.
- **The tag is the deploy.** Push `2.0.0` only inside the maintenance window, after the edge work
  and the `PUBLIC_BASE_URL` variable are in place, and approve the gated run.
- **An interrupted force-push is safe to re-run** — the push is idempotent; re-list the refs after
  it.

## Pointers

- Cutover sequence: [../docs/ops/cutover.md](../docs/ops/cutover.md)
- Deploy pipeline, secrets, and rollback: [../docs/ops/deploy.md](../docs/ops/deploy.md)
