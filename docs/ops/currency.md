# Dependency Currency

## Purpose

How the project stays on the newest released dependencies — the Go modules, the web packages, the
container base images, and the VPS's Docker tooling. The pinned versions themselves live in their
files (`go.mod`, `web/bun.lock`, the Dockerfiles, the workflows — root `AGENTS.md` rule 17); this
doc owns the process that notices when they fall behind and the rules for bumping them safely.

## Rules

1. **Dependabot watches the three safe ecosystems and opens reviewable PRs.**
   [`.github/dependabot.yml`](../../.github/dependabot.yml) covers `github-actions` (the workflow
   `uses:` pins), `gomod`, and `docker` (the Dockerfile base-image tags), weekly, with minor/patch
   grouped and majors as individual PRs. Every PR rides the normal gate (`ci.yml`, `make check`) —
   nothing merges on the bot's word.
2. **The web tree stays Bun-only.** The `npm` Dependabot ecosystem is the only one that reads
   `web/package.json`; it may be enabled only as a watched trial — the first update PR must edit
   `bun.lock` alone, and any other artifact it produces (`package-lock.json`, node_modules state,
   anything npm-shaped) reverts the entry immediately. The npm entry is deferred until a watched
   trial lands; while it is out, web drift is reported by the weekly currency workflow
   (`bun outdated`) and bumped by hand.
3. **A weekly report records drift and vulnerabilities.**
   [`.github/workflows/currency.yml`](../../.github/workflows/currency.yml) runs `go list -m -u
all`, `govulncheck ./...`, and `bun outdated` every Monday. A security finding is triaged
   immediately; ordinary drift queues for the next release. The report is a signal, not a merge.
4. **The VPS tooling is checked on the same cadence.** On the VPS: `docker version`, `docker
compose version`, `apt list --upgradable`, and a look at each Dockerfile base image's upstream
   tag. Patch-level host upgrades are routine; a Docker Engine or Compose major bump is a planned
   change with a deploy rehearsal.
5. **Base-image bumps are deliberate.** Bumping the Caddy pin invalidates the xcaddy build layer
   and forces a rebuild (Dockerfile.caddy); bumping the Go, Bun, or Alpine pins changes the
   artifact and must ride a tag deploy. Bump pins together with the pattern docs that name them
   (the root `AGENTS.md` rule 17 refresh procedure) in one commit.
6. **`govulncheck` and the web audit live here.** `govulncheck` runs in the weekly workflow;
   `make deps-report` is the local drift list (Go modules + `bun outdated`) and runs no
   vulnerability scan. The web advisory scan is added once the pinned Bun's audit command surface
   is confirmed — until then `bun outdated` is the web signal.

## Pattern

```text
Dependabot (weekly) ─▶ reviewable PR ─▶ ci.yml + make check ─▶ merge
currency.yml (weekly) ─▶ drift + vuln report ─▶ triage ─▶ bump by review
VPS check (same cadence) ─▶ docker/compose/apt/base images ─▶ planned bump
```

## Examples

- Local drift report: `make deps-report`.
- Go vulnerability scan: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`.
- Web drift: `cd web && bun outdated`.
- VPS: `docker version && docker compose version && apt list --upgradable`.

## Gotchas

- **Dependabot and Bun.** Verify the first `npm`-ecosystem PR leaves `bun.lock` as the only changed
  artifact before trusting the entry; a lockfile format mismatch can make the bot open useless or
  wrong PRs.
- **`govulncheck@latest` needs network** (the vuln database); it runs in CI and at the owner's
  machine, never in the sandboxed review environment.
- **Host upgrades on the VPS can restart Docker.** Run `apt` upgrades in a quiet window; the stack
  restarts via its `unless-stopped` policy, but the deploy lock is not held by `apt`.
- **`bun outdated` exits non-zero when it reports drift** — the workflow wraps it deliberately.

## Pointers

- Index: [../README.md](../README.md)
- Go toolchain procedure: [../patterns/go/toolchain.md](../patterns/go/toolchain.md)
- Web toolchain procedure: [../patterns/web/toolchain.md](../patterns/web/toolchain.md)
- Deploy pipeline: [deploy.md](deploy.md)
