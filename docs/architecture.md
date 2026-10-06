# Architecture

## Purpose

One page that maps the running system: what it is, what runs where, how a request and a write
travel, the deploy topology, and what the project deliberately does not build. Pattern docs own the
rules, feature docs own per-scope contracts, and `docs/ops/` owns the procedures; this doc owns the
shape and links to them.

## Context

Sick-Fansubs is a fansub community website: blog posts and projects with downloads, full-text
search, comments and hearts, favorites and follows, a notifications feed with web push, member
accounts, and a staff dashboard. One public site on one owner-operated VPS; production is
`https://sickfansubs.com`.

The application is a single Go binary with the compiled Lit frontend embedded. It speaks JSON under
`/api/v1`, serves media bytes from its data volume, and keeps state in one SQLite database. Sessions
are opaque server-side rows carried by an `sf_session` cookie — no browser-stored tokens.

## Containers

```text
browser
  │ HTTPS
  ▼
Cloudflare  (proxied apex, Full strict, Bot Fight Mode on)
  │ HTTPS 443 (the origin firewall admits only Cloudflare's ranges)
  ▼
VPS
  ├─ caddy   Compose service — TLS (Cloudflare DNS challenge), sole ingress, :443 → app:8080
  ├─ app     Compose service — Go binary: JSON API, media serving, embedded SPA; the database's ONLY writer
  ├─ migrate one-shot profile — schema migrations, with the app stopped first
  ├─ backup  one-shot profile — age-encrypted unit (database + media tar + manifest)
  └─ host    cron (daily backup, weekly disk cleanup), the Mega push, logrotate
```

- Volumes: `sick-data` (SQLite database, media files, the rotating log sink), `caddy-data`, and
  `caddy-config`; Compose file secrets carry the SMTP API key and the VAPID private key. The
  Compose contract is [ops/deploy.md](ops/deploy.md)'s.
- Browser TLS ends at Cloudflare; Caddy terminates origin TLS and is the only process on 443. The
  `DOCKER-USER` firewall blocks every non-Cloudflare source.
- Log records go to stdout (Docker's bounded json-file driver) and to the rotating file sink the
  staff logs tab reads.

## Request flow

1. Cloudflare terminates browser TLS, applies the zone settings, and proxies to origin 443; Caddy
   terminates origin TLS and reverse-proxies to `app:8080`.
2. The router resolves three families: `/api/v1/*` (the JSON API), `/media/*` (stored media bytes),
   and everything else to the embedded SPA shell. Hashed assets are immutable-cached, HTML
   revalidates, and a missing `/assets/*` file is a 404 — never the shell.
3. Middleware chains are composed in one place (`internal/routes/chains.go`): request ID first, then
   the trusted-origin and session checks (every family except the plain public reads), then the
   forced-password-change gate, then CSRF on unsafe methods. The auth subtree carries path-scoped IP
   rate limits instead of the gate, and handler-level user-keyed buckets guard the abuse-sensitive
   writes (comments, password change, verification sends, the push self-test). Rule home:
   [patterns/go/route-chains.md](patterns/go/route-chains.md).
4. Handlers validate input, call the store, and answer JSON; errors are RFC 9457 problem bodies, and
   every request logs through the request-scoped `slog` logger. The wire contract is
   [api/openapi.yaml](api/openapi.yaml).
5. The store is the only SQLite writer: transactions per write, keyset pagination for lists, FTS5
   with Greek accent folding for search, audit rows for staff actions, and per-viewer rows (or
   dismissals over the audit space) for notifications
   ([internal/store/AGENTS.md](../internal/store/AGENTS.md)). Push fan-out signs with VAPID and
   cleans up dead endpoints.

## Data flows

- **Legacy import** (cutover only): a fresh volume receives users → content → search index → media
  → avatars → favorites from the frozen legacy export, each leg printing a reconciliation report.
  The runbook is [../deploy/data-import.md](../deploy/data-import.md).
- **Media**: every processed image is stored under `media/images/<id[:2]>/<id>.<ext>`; the database
  stores storage-relative references and the serving path is public under `/media/images/`
  ([internal/media/AGENTS.md](../internal/media/AGENTS.md)).
- **Backup**: the daily unit is created by the same code path production uses, age-encrypted,
  retained as the newest ten locally, and pushed off the VPS by the host script; the deploy path
  takes a pre-deploy unit ([ops/backup.md](ops/backup.md)).
- **Restore**: a fail-closed staged workflow — quarantine, activate, extract media, verify — and the
  application refuses to serve while a restore marker exists ([ops/restore.md](ops/restore.md)).

## Deploy topology

- Version tags drive `.github/workflows/deploy.yml`: the test job runs the `make check` suite plus a
  build and a frozen-lockfile install; the build job pushes five images to ghcr (app, migrate,
  backup, restore, caddy — the auxiliary images version-paired with the app through `AUX_TAG`,
  restore riding `:latest` as the break-glass path); the deploy job — gated by the
  `deploy-approval` environment — ships the compose file, the Caddyfile, and the scripts over SSH
  and runs `deploy/deploy.sh` ([ops/deploy.md](ops/deploy.md)).
- `deploy.sh` takes the preflight backup, stops the app, applies migrations, brings the stack up,
  and health-gates it, keeping the previous TAG and Caddyfile as the rollback target. It never rolls
  back `compose.yaml`, and a failure that has already moved the schema stops for a data decision
  ([ops/deploy.md](ops/deploy.md)).
- The public probe runs from the VPS, because Cloudflare challenges the workflow runner's egress
  IPs. Provisioning and rebuilds: [../deploy/bring-up.md](../deploy/bring-up.md); the production
  cutover and the repository swap: [ops/cutover.md](ops/cutover.md),
  [../deploy/repo-swap.md](../deploy/repo-swap.md).

## Rejected by design

Settled no-gos, recorded so they are not re-proposed: anonymous analytics and view/download
counters; search typo-tolerance, suggestions, relevance ranking, or a separate search service;
off-VPS audit-event shipping; storage dashboards and trend reports; interactive API tooling;
frontend tooling experiments; further media-pipeline enhancements; further backup/DR enhancements;
moving the Caddy token into a file secret; maskable PWA icons; direct messages and a home chat
button.

Architecture-level prohibitions — no MongoDB at runtime, no JWT or refresh-token flow, no separate
frontend artifact, no SolidJS or HTMX — live in the root [`AGENTS.md`](../AGENTS.md) `Do Not` list.

## Pointers

- Index: [README.md](README.md)
- Wire contract: [api/openapi.yaml](api/openapi.yaml)
- Go rules: [patterns/go/](patterns/go/); web rules: [patterns/web/](patterns/web/)
- Operations: [ops/deploy.md](ops/deploy.md), [ops/backup.md](ops/backup.md),
  [ops/restore.md](ops/restore.md), [ops/cutover.md](ops/cutover.md)
- Per-scope contracts: the `AGENTS.md` beside the code, mapped in the root
  [`AGENTS.md`](../AGENTS.md)
