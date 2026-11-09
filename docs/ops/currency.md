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
   nothing merges on the bot's word. Action pins follow [deploy.md](deploy.md) rule 9; every job
   names a versioned runner label rather than a floating label, and a runner-label bump belongs in
   the manual currency pass.
2. **The web tree stays Bun-only.** The `npm` Dependabot ecosystem is the only one that reads
   `web/package.json`; it may be enabled only as a watched trial — the first update PR must edit
   `bun.lock` alone, and any other artifact it produces (`package-lock.json`, node_modules state,
   anything npm-shaped) reverts the entry immediately. The npm entry is deferred until a watched
   trial lands; while it is out, web drift is reported by the weekly currency workflow
   (`bun outdated`) and bumped by hand.
3. **A weekly report records drift and vulnerabilities.**
   [`.github/workflows/currency.yml`](../../.github/workflows/currency.yml) runs `go list -m -u
all`, `make deps-vuln`, `bun outdated`, and `bun audit` every Monday. A security finding is
   triaged immediately; ordinary drift queues for the next release. The Go vulnerability scan, web
   drift, and web audit steps fail the run when they report a problem; the Go module listing is
   informational. The red run is the signal, not a merge gate. Every report step runs after an
   earlier failure, so one red signal never hides the others.
4. **The VPS tooling is checked on the same cadence.** On the VPS: `docker version`, `docker
compose version`, `apt list --upgradable`, and a look at each Dockerfile base image's upstream
   tag. Patch-level host upgrades are routine; a Docker Engine or Compose major bump is a planned
   change with a deploy rehearsal.
5. **Base-image bumps are deliberate.** Bumping the Caddy pin invalidates the xcaddy build layer
   and forces a rebuild (Dockerfile.caddy); bumping the Go, Bun, or Alpine pins changes the
   artifact and must ride a tag deploy. Bump pins together with the pattern docs that name them
   (the root `AGENTS.md` rule 17 refresh procedure) in one commit.
6. **`govulncheck` and the web audit live here.** `govulncheck` runs in the weekly workflow through
   `make deps-vuln`, resolved from the tool entry in `go.mod`; `make deps-report` is the local
   drift list (Go modules + `bun outdated`) and runs no vulnerability scan. The pinned Bun's
   `bun audit` surface is confirmed and runs in the same weekly workflow; `bun outdated` remains
   the drift signal.

## Pattern

```text
Dependabot (weekly) ─▶ reviewable PR ─▶ ci.yml + make check ─▶ merge
currency.yml (weekly) ─▶ drift + vuln report ─▶ triage ─▶ bump by review
VPS check (same cadence) ─▶ docker/compose/apt/base images ─▶ planned bump
```

## Examples

- Local drift report: `make deps-report`.
- Go vulnerability scan: `make deps-vuln`.
- Web drift and advisories: `cd web && bun outdated`, then `cd web && bun audit`.
- VPS: `docker version && docker compose version && apt list --upgradable`.

## Gotchas

- **Dependabot and Bun.** Verify the first `npm`-ecosystem PR leaves `bun.lock` as the only changed
  artifact before trusting the entry; a lockfile format mismatch can make the bot open useless or
  wrong PRs.
- **`govulncheck` needs network** (the vuln database); it runs in CI and at the owner's machine,
  never in the sandboxed review environment. Its version policy is
  [toolchain.md](../patterns/go/toolchain.md)'s.
- **Host upgrades on the VPS can restart Docker.** Run `apt` upgrades in a quiet window; the stack
  restarts via its `unless-stopped` policy, but the deploy lock is not held by `apt`.
- **`bun outdated` exits non-zero when it reports drift** — the workflow and `make deps-report`
  surface that exit instead of masking it. A red currency run is expected while drift waits for
  the next release.

## Pointers

- Index: [../README.md](../README.md)
- Go toolchain procedure: [../patterns/go/toolchain.md](../patterns/go/toolchain.md)
- Web toolchain procedure: [../patterns/web/toolchain.md](../patterns/web/toolchain.md)
- Deploy pipeline: [deploy.md](deploy.md)
