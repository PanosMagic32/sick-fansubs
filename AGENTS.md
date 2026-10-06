# AGENTS.md — Agent Entry Point

Read this file before working on Sick-Fansubs.

## Project Direction

Sick-Fansubs is a clean rewrite of the existing Nx/Angular/NestJS fansub community application.

**Decided target:**

- Go backend, standard library first, with minimal dependencies.
- SQLite as the target database, using the pinned pure-Go `modernc.org/sqlite` driver and the [SQLite foundation](docs/patterns/go/sqlite.md) rules.
- Lit + Vite as the target frontend.
- Opaque SQLite-backed browser sessions; no target JSON Web Token/refresh-token flow.
- Deployment to a self-managed VPS operated over SSH.
- Preservation of core product capabilities and production data.
- Lit production build embedded in Go; Caddy provides TLS/reverse proxy in the Docker topology.
- All repository, CI/CD, runtime, and deployment naming is `sick-fansubs`; the retired beta-era names must not reappear.

This is **not** a wire-compatible replacement. Legacy routes, payloads, cookies, session design, internal architecture, and frontend details are evidence to evaluate, not contracts to copy automatically. Intentional improvements are allowed when they are recorded and reviewed. Existing sessions do not need to survive cutover; a one-time logout is acceptable.

## Mandatory First Read

Read these in order before doing project work:

1. [`docs/README.md`](docs/README.md) — the documentation index: pattern docs, feature docs, operations and architecture docs, the wire contract.
2. [`docs/patterns/docs-conventions.md`](docs/patterns/docs-conventions.md) — how docs, comments, and links are written and guarded.

Then read the documents relevant to the task.

## Requirement Vocabulary

**Legacy observation** — evidence from the existing system; migration evidence, not a target contract. **Decided target** — an accepted direction implementation must follow. **Open decision** — unresolved; do not implement a choice silently. **Deferred future work** — intentionally outside the current delivery scope.

## Authority Order

When documents disagree, use this order:

1. The user's latest explicit instruction.
2. Pattern docs — [`docs/patterns/`](docs/patterns/) — the one agreed way to do something.
3. Co-located feature docs (`AGENTS.md` beside the code), the operations docs (`docs/ops/`), and the architecture doc ([`docs/architecture.md`](docs/architecture.md)).
4. Legacy observations and historical reports.

Historical evidence cannot override a decided target. The retired planning documents and numbered
records live in git history, never in the tree.

## Non-Negotiable Working Rules

1. **Teaching mode and incremental review remain active.** Work in the smallest useful vertical slice and ensure the owner can explain it.
2. **Preserve capabilities and data, not accidental interfaces.** Use the legacy application to discover behavior, edge cases, and migration requirements.
3. **Do not settle an open choice silently.** Ask the owner, then record the ruling in the pattern or feature doc it belongs to — a choice with no doc and no owner ruling is not settled.
4. **Use SQLite for the target application.** MongoDB Atlas is the safe migration source, not the target runtime database. The SQLite rules — driver, DSN/pragmas, WAL, pool, migrations, UTC-millisecond time, identifiers, backup — live in [`docs/patterns/go/sqlite.md`](docs/patterns/go/sqlite.md), with [`sql-mapping.md`](docs/patterns/go/sql-mapping.md) for values and [`ids.md`](docs/patterns/go/ids.md) for identifiers.
5. **Use Lit + Vite for the target frontend.** Existing SolidJS experiments and HTMX are not the target.
6. **Prefer simple Go.** Use the standard library first. Add dependencies only with a concrete reason.
7. **Use boundaries where they add clarity.** Handler, domain-value, auth, and store separation is useful when responsibilities differ; do not create ceremonial layers or interfaces.
8. **Protect production data.** References under `docs/current-data/` may contain sensitive context. Do not modify them, publish them, or copy sensitive values into documentation, fixtures, logs, or prompts.
9. **Greek user interface remains a product requirement.** Keep user-facing copy in a central message catalog rather than scattering literals through components.
10. **Do not describe same-VPS storage as off-site.** Local MinIO or another local volume can be a local backup only. Future off-VPS storage is required, but its provider is not selected.
11. **Commands live in the Makefile.** `make help` lists the run/build/test targets; deploy runs through GH Actions SSH on version tags. Navigation docs point at the Makefile instead of restating commands.
12. **Every agent session — and every spawned sub-agent — chats in the `caveman` skill's style (default level `full`).** Load the `caveman` skill FIRST, before any other skill or tool call, and keep its style for the whole session. The owner does not need to repeat this per prompt, and sub-agents inherit it without being told. Persisted files — code, comments, commits, docs, tickets — stay normal prose.
13. **Run `make go-fix` before every commit.** `go fix` applies toolchain modernizations (for example `slices`/`maps` helpers, `wg.Go`, range-over-int) — review its diff like any other change, keep `make check` green afterwards, and fold the result into the same commit as the work it modernizes.
14. **Adversarial reviewers spawn IN PARALLEL, never one at a time.** Spec, Standards, and every other review lens run side by side with disjoint, read-only scopes — sequential spawning wastes the owner's time. The only ordering constraint is between review passes whose fixes invalidate each other, not between reviewers of one diff.
15. **Every code scope has a feature doc, and the guard stays green.** A scope is a Go package, a command, or a web scope; each carries an `AGENTS.md`, and `make docs-lint` (which runs inside `make check`) enforces scope docs, index reachability, link integrity, and the absolute decision-citation check.
16. **Comments state intent, never history.** A comment explains a constraint or a non-obvious mechanism; it never restates the code, narrates review history, or cites a decision number (see [`docs/patterns/docs-conventions.md`](docs/patterns/docs-conventions.md)).
17. **Stay on the newest released toolchain and the newest official guidance — backend and frontend.** The Go pin in `go.mod` (read it there; never bake a version into a doc) tracks the newest released Go: bump it when a newer release lands, and refresh the pattern docs in the same commit. A superseded idiom is never kept as current advice, and new code is never written against an older release. Docs describe the pinned toolchain and name an introducing release only as the historical fact of when a feature arrived; they never bake a version in. The same rule governs the web tree — newest released Lit/Vite/TypeScript and the newest official guidance — and its pattern docs state it there when the web slice lands. The Go side lives in [`docs/patterns/go/toolchain.md`](docs/patterns/go/toolchain.md).
18. **Reviews are read-only; reviewers do not run the gates.** A review pass or adversarial reviewer reads and reports — it runs `make check` or any build, test, lint, or format command only when the owner asks. An agent tasked with implementing or fixing code runs the gates its change needs.
19. **Go and Bun dependency updates are owner-gated.** Never run a dependency-update command on your own initiative — `go get -u ./...`, `go get <module>@latest`, `go mod tidy` as an update pass, `bun update`, `bun add <package>@latest`, or an equivalent — against `go.mod`/`go.sum` or `package.json`/`bun.lock`. Whenever work is initiated (a session, a pass, a batch, or a slice), ask the owner once whether to run the update commands first, naming the command and the manifest it would touch; run them only on the owner's word. A bump is validated by `make check` and is never logged in docs (rule 17).

## Document Map by Task

The durable homes are [`docs/patterns/`](docs/patterns/) (cross-cutting rules), the feature docs
(`AGENTS.md` beside the code), [`docs/ops/`](docs/ops/) (procedures that run against a real
environment), the system map ([`docs/architecture.md`](docs/architecture.md)), and
[`docs/api/openapi.yaml`](docs/api/openapi.yaml) (the wire contract). Git history
holds the retired planning documents and decision records.

| Task                                                      | Read                                                                                                                   |
| --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Go rules: structure, errors, logging, SQL                 | [`docs/patterns/go/`](docs/patterns/go/)                                                                               |
| Web rules: components, CSS, testing                       | [`docs/patterns/web/`](docs/patterns/web/)                                                                             |
| API and data requirements                                 | [`docs/api/openapi.yaml`](docs/api/openapi.yaml)                                                                       |
| System shape: context, containers, flows, deploy topology | [`docs/architecture.md`](docs/architecture.md)                                                                         |
| Deployment, backup, restore, cutover                      | [`docs/ops/`](docs/ops/)                                                                                               |
| Legacy behavior and data reconciliation                   | [`internal/migration/AGENTS.md`](internal/migration/AGENTS.md), [`cmd/importdata/AGENTS.md`](cmd/importdata/AGENTS.md) |
| A scope's contracts                                       | its `AGENTS.md` — see Package Navigation below                                                                         |

## Package Navigation

Each folder's `AGENTS.md` is the feature doc for that scope: what it owns, its contracts, its
gotchas, and pointers. Pattern-level rules live in [`docs/patterns/`](docs/patterns/).

| When working on                                                                                   | Open first                                                                             |
| ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Startup/shutdown, the DB open path, expired-session cleanup, serving the frontend                 | [`cmd/api/AGENTS.md`](cmd/api/AGENTS.md)                                               |
| Schema migration runs, their Makefile/container wiring                                            | [`cmd/migrate/AGENTS.md`](cmd/migrate/AGENTS.md)                                       |
| Daily backup unit creation, age encryption, retention (`make backup-local`)                       | [`cmd/backup/AGENTS.md`](cmd/backup/AGENTS.md)                                         |
| Legacy data import + reconciliation report                                                        | [`cmd/importdata/AGENTS.md`](cmd/importdata/AGENTS.md)                                 |
| Legacy thumbnail HTTP fetch, its reason codes (`make fetch-media-local`)                          | [`cmd/fetchmedia/AGENTS.md`](cmd/fetchmedia/AGENTS.md)                                 |
| Media import + reference rewrite + media reconciliation report                                    | [`cmd/importmedia/AGENTS.md`](cmd/importmedia/AGENTS.md)                               |
| Legacy avatar import + reconciliation (`make import-avatars-local`)                               | [`cmd/importavatars/AGENTS.md`](cmd/importavatars/AGENTS.md)                           |
| FTS5 search-index rebuild/backfill (`make rebuild-search-local`)                                  | [`cmd/rebuildsearch/AGENTS.md`](cmd/rebuildsearch/AGENTS.md)                           |
| Media orphan sweep, manual trigger (`make sweep-media-local`)                                     | [`cmd/sweepmedia/AGENTS.md`](cmd/sweepmedia/AGENTS.md)                                 |
| Operator password reset, break-glass (`make reset-password-local`)                                | [`cmd/resetpassword/AGENTS.md`](cmd/resetpassword/AGENTS.md)                           |
| Older-backup restore, fail-closed staged workflow (`make restore-local`)                          | [`cmd/restore/AGENTS.md`](cmd/restore/AGENTS.md)                                       |
| Docs guard: scope docs, index reachability, link integrity, decision citations (`make docs-lint`) | [`cmd/docslint/AGENTS.md`](cmd/docslint/AGENTS.md)                                     |
| Audit events                                                                                      | [`internal/audit/AGENTS.md`](internal/audit/AGENTS.md)                                 |
| Code-shape guards for the Go pattern docs                                                         | [`internal/codestyle/AGENTS.md`](internal/codestyle/AGENTS.md)                         |
| Env vars and defaults                                                                             | [`internal/config/AGENTS.md`](internal/config/AGENTS.md)                               |
| DSN/pragmas, openers, migration runner, readiness                                                 | [`internal/database/AGENTS.md`](internal/database/AGENTS.md)                           |
| Identifier minting (`id.New`, the request-id fallback)                                            | [`internal/id/AGENTS.md`](internal/id/AGENTS.md)                                       |
| Legacy-record transform, download classification, reconciliation report                           | [`internal/migration/AGENTS.md`](internal/migration/AGENTS.md)                         |
| Wire contracts: statuses, DTOs, problems, cookies                                                 | [`internal/handler/AGENTS.md`](internal/handler/AGENTS.md)                             |
| Log file sink, rotation, and the log tail reader                                                  | [`internal/logging/AGENTS.md`](internal/logging/AGENTS.md)                             |
| SMTP relay + server-side email copy                                                               | [`internal/mail/AGENTS.md`](internal/mail/AGENTS.md)                                   |
| Session/CSRF/origin/rate-limit enforcement or chain order                                         | [`internal/middleware/AGENTS.md`](internal/middleware/AGENTS.md)                       |
| Media storage layout, pure-Go processing pipeline, size/dimension limits                          | [`internal/media/AGENTS.md`](internal/media/AGENTS.md)                                 |
| Session/user shapes and expiry                                                                    | [`internal/identity/AGENTS.md`](internal/identity/AGENTS.md)                           |
| Problem types and 422 shapes                                                                      | [`internal/problem/AGENTS.md`](internal/problem/AGENTS.md)                             |
| Push fan-out sender, VAPID keys, payload, retries/dead-endpoint cleanup                           | [`internal/push/AGENTS.md`](internal/push/AGENTS.md)                                   |
| Endpoints and middleware chains                                                                   | [`internal/routes/AGENTS.md`](internal/routes/AGENTS.md)                               |
| Greek accent normalization, FTS5 query construction                                               | [`internal/search/AGENTS.md`](internal/search/AGENTS.md)                               |
| Project slug generation + validation, Greek→latin transliteration                                 | [`internal/slug/AGENTS.md`](internal/slug/AGENTS.md)                                   |
| Sign-in/password/session business rules                                                           | [`internal/auth/AGENTS.md`](internal/auth/AGENTS.md)                                   |
| Persistence, transactions, auth_version revocation                                                | [`internal/store/AGENTS.md`](internal/store/AGENTS.md)                                 |
| Test-support user-row seeding for tests in every package                                          | [`internal/store/storetest/AGENTS.md`](internal/store/storetest/AGENTS.md)             |
| App bootstrap, route table, global/in-shadow styles                                               | [`web/src/core/AGENTS.md`](web/src/core/AGENTS.md)                                     |
| Click interception, popstate, header breakpoints, footer                                          | [`web/src/core/shell/AGENTS.md`](web/src/core/shell/AGENTS.md)                         |
| Session state machine, auth pages, form directive                                                 | [`web/src/features/auth/AGENTS.md`](web/src/features/auth/AGENTS.md)                   |
| About page (Η ομάδα) copy and community links                                                     | [`web/src/features/about/AGENTS.md`](web/src/features/about/AGENTS.md)                 |
| Blog wire contracts, home list page, blog detail page                                             | [`web/src/features/blog/AGENTS.md`](web/src/features/blog/AGENTS.md)                   |
| Projects wire contracts, list/detail pages                                                        | [`web/src/features/projects/AGENTS.md`](web/src/features/projects/AGENTS.md)           |
| Search page, type filter, URL-synced query/cursor state                                           | [`web/src/features/search/AGENTS.md`](web/src/features/search/AGENTS.md)               |
| Inline comments thread, hearts, deep-link flow                                                    | [`web/src/features/comments/AGENTS.md`](web/src/features/comments/AGENTS.md)           |
| Notifications feed: wire calls, `/notifications` page, badge event                                | [`web/src/features/notifications/AGENTS.md`](web/src/features/notifications/AGENTS.md) |
| Staff dashboard: users/metrics/logs, content forms, media upload                                  | [`web/src/features/admin/AGENTS.md`](web/src/features/admin/AGENTS.md)                 |
| API transport, shared UI primitives, Greek catalog                                                | [`web/src/shared/AGENTS.md`](web/src/shared/AGENTS.md)                                 |

## Before Implementing a Slice

1. Classify every input as a legacy observation, decided target, open choice, or deferred future work.
2. Resolve only the choices that block this slice, with the owner; the ruling lands in the pattern or feature doc it belongs to.
3. Agree the definition of ready, acceptance criteria, and tests.
4. Implement and review one small vertical slice.
5. Update the pattern doc and the feature doc that the slice changed.

## Do Not

- Treat NestJS response shapes, cookie names, token claims, throttling numbers, or Angular routes as mandatory merely because they exist.
- Reintroduce MongoDB as a target runtime option without a superseding owner ruling recorded in a pattern or feature doc.
- Open modernc SQLite with driver name `sqlite3`, accept an arbitrary runtime DSN, copy only the live main database file, or run schema migrations from ordinary request handlers.
- Replace Lit with SolidJS, Angular, or HTMX without a superseding owner ruling recorded in a pattern or feature doc.
- Require old sessions to remain valid after cutover.
- Reintroduce access/refresh JSON Web Tokens or browser-stored bearer credentials without a superseding owner ruling recorded in a pattern or feature doc.
- Compare cookie headers character by character; test the selected contract semantically.
- Deploy the Lit build as a separate production artifact; the accepted topology embeds it in the Go application inside a Docker container while Caddy remains the TLS/reverse-proxy boundary.
- Read retired material as current policy — recover it from git history for evidence only.
- Cite a retired decision number in a new doc or a new code comment, or link to a retired planning document.
- Reintroduce a retired beta-era name in the repository, images, runtime paths, or docs.
- Leave `make docs-lint` red.
