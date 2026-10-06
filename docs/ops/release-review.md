# Release Review

## Purpose

The whole-application review that runs before every stable cutover: the agent lenses that run it,
the research-first and batch protocols, the ripple-ascending pass order, the findings model, and
the owner gates that close it.

This is a process review, not a diff review. Every change already passes its own adversarial review;
this one targets cross-cutting drift that accumulated between slices, plus quality axes the slice
reviews never covered — accessibility, SEO, infrastructure, and ripple effects. Legacy parity is not
a review axis.

## Rules

1. **Whole-app scope, no fixed point.** Each batch reviews its subtree against the checklists below,
   the pattern docs (`docs/patterns/`), the wire contract (`docs/api/openapi.yaml`), and the scope's
   feature doc. Per-slice findings stay with the slice review; this review never re-litigates them.
2. **Legacy parity is not an axis.** The new application is its own entity. Legacy material is
   evidence about product behavior, never a checklist.
3. **Research first.** Every batch opens with a context brief built from primary sources: official
   language and platform documentation, the pattern docs, the feature docs, and the wire contract.
   The brief selects the batch tooling. Briefs live in the local, gitignored scratch directory
   (`.scratch/review/`), never committed.
4. **The default batch is one feature end-to-end** (routes, handler, service, store, web). Split a
   feature into package-level batches only when it exceeds one context window.
5. **Ripple ascending.** A pass whose fixes could invalidate another pass's review runs after it, so
   passes run 0 Inventory, then 4 Platform, 2 QA, 3 AppSec, 1 Staff. The same rule orders batches
   inside a pass: smallest ripple first. The owner may adjust the batch order at pass start.
6. **Three review lines per batch.** Standards: the documented repo rules, with the Fowler smell
   baseline as a fallback — documented rules override the baseline, and a smell is a judgment call,
   never a hard violation. Spec: accepted behavior against the wire contract and feature docs. Lens:
   the checklist of the lens the batch header names.
7. **Findings carry a severity** — `blocker`, `major`, `minor`, or `nit` (table below). A `blocker`
   is fixed immediately on discovery; it never waits for the pass gate.
8. **Owner gates.** A pass closes by owner ruling on its written summary; the ruling needs no
   signature line. The review is done when every pass is closed, no `blocker` or `major` finding
   is open (each closed or parked with a recorded owner ruling), and `make check` is green.
9. **Pause only at batch boundaries.** Never stop inside a batch. Resume from the latest pass
   summary; the next batch starts only after the previous batch's findings and rulings are
   written.
10. **Recurrence.** The review runs before every stable cutover, and on demand when the owner calls
    it. There is no fixed calendar.
11. **The review workspace is local scratch, never committed.** The functionality inventory,
    context briefs, findings, and rulings live in a gitignored scratch directory
    (`.scratch/review/`); nothing there is a durable home. This procedure does not duplicate the
    inventory.
12. **Lenses are agent roles, not people.** One agent wears the batch's lens; the owner pairs on the
    VPS-adjacent platform batches, where VPS access is needed.
13. **Fixes ride normal tags.** There is no special review release. Hard findings are fixed before
    their batch closes, regression tests are written first, and every fix leaves `make check` green.
14. **Review lines are static.** A reviewer reads and reports; it does not run build, test, lint, or
    format commands unless the owner asks. An agent tasked to implement or fix code runs the gates
    its change needs (rule 13).

## Pattern

### Lenses

Roles are **review lenses for the agent**, not humans. Each batch header names the lens.

| Lens                     | Pass | Reviews for                                              |
| ------------------------ | ---- | -------------------------------------------------------- |
| Product                  | 0    | Inventory, scope, Greek copy, nice-to-have gating        |
| Principal/Staff engineer | 1    | Architecture, code quality, ripple analysis, types       |
| QA engineer              | 2    | Semantics, accessibility, SEO, tokens, edge cases, tests |
| AppSec                   | 3    | Authorization, sessions, CSRF, validation, secrets       |
| DevOps engineer          | 4    | Docker, CI/CD, deploy scripts, build pipeline            |
| SRE                      | 4    | Runtime config, SQLite safeguards, backup, sweeps        |

### Passes

Passes run ripple-ascending — smallest ripple first, refactors last.

1. **Pass 0 — Inventory (Product lens).** Compile the functionality list — shipped, in-flight, and
   nice-to-have — from the feature docs and the wire contract, and have the owner validate it. No
   code. The validated inventory in the review workspace is the scope baseline every later pass
   reviews against. Inventory records; it never authorizes new work.
2. **Pass 4 — Platform (DevOps and SRE lenses).** Infrastructure fixes, zero code ripple.
3. **Pass 2 — Frontend and behavior (QA lens).** Small UI and code fixes.
4. **Pass 3 — Authorization and application security (AppSec lens).** Middleware and API changes,
   medium ripple.
5. **Pass 1 — Architecture and code quality (Principal/Staff lens).** Refactors ripple everywhere,
   so they run last.

### Batch lifecycle

1. **Research.** Write the context brief: relevant pattern and feature docs, primary sources, and
   the tooling choice for this batch.
2. **Review.** Run the three review lines against the subtree.
3. **Rule.** Put every finding to the owner: fix now, park with a recorded ruling, or accept.
4. **Fix and validate.** Fix hard findings before the batch closes, tests first; run `make check`.
5. **Stop.** Write the batch's findings and rulings in the review workspace and update the pass
   summary. The batch boundary is the only legal pause point.

### Findings model

| Severity  | Meaning                                                                           |
| --------- | --------------------------------------------------------------------------------- |
| `blocker` | Cutover-blocking: security hole, data loss, or broken contract; fix on discovery. |
| `major`   | Clear defect or contract drift; fixed before done, or parked by owner ruling.     |
| `minor`   | Improvement worth recording; batched or deferred by owner ruling.                 |
| `nit`     | Cosmetic; recorded, never blocking.                                               |

### Definition of done

- Every pass closed; zero open `blocker`/`major` findings; `make check` green.
- The root `AGENTS.md` and the latest pass summary describe the current state.
- The review workspace holds the current inventory, findings, and rulings.

### Platform checklist (Pass 4)

The owner pairs on VPS-adjacent batches: the agent writes the checklist, the owner runs it over SSH,
and the findings come back for review together.

- **4.1 Docker images and Compose.** Every `Dockerfile*` (app, migrate, backup, restore,
  rebuildsearch, importdata, importmedia, importavatars, caddy), `compose.yaml`, `.dockerignore`,
  non-root uid/gid, pinned base images, secrets through `/run/secrets`, no baked credentials, layer
  hygiene.
- **4.2 CI/CD.** The GitHub Actions workflow, the tag-deploy policy ([deploy.md](deploy.md)),
  build/test parity with local `make check`, what the workflow copies to the VPS, secret handling
  in CI.
- **4.3 Deploy scripts and runbook.** `deploy/*.sh` (`deploy.sh`, `backup-now.sh`, `down.sh`,
  `daily-backup.sh`, `mega-put.sh`), the shipped runbooks `docs/ops/backup.md` and
  `docs/ops/restore.md`, `sh -n` and bash pitfalls, fail-closed
  paths, the pre-deploy backup window.
- **4.4 VPS runtime config.** Caddyfile (TLS, `trusted_proxies`, `/media` and API proxying), cron
  (03:00 backup, timezone), Compose secret files, uid/permissions on volumes, log rotation.
- **4.5 SQLite runtime and SQL safeguards.** DSN and pragmas, WAL, pool shape, query plans on hot
  paths (lists, FTS5 search), index coverage, transaction boundaries, NULL-scan guards, sweep tasks
  (sessions, media, audit retention), readiness; the rules live in
  [sqlite.md](../patterns/go/sqlite.md).
- **4.6 Backup and restore.** `cmd/backup` (age encryption, keep-10 retention, `-replace-today`),
  `cmd/restore` (staged, fail-closed, manifest validation), media tarball verification, restore
  drill evidence; the procedures live in [backup.md](backup.md) and [restore.md](restore.md).

### QA checklist (Pass 2)

The research step chooses tooling per batch. The intended defaults are axe-core for accessibility
and a manual Lighthouse run for SEO and performance; these are batch choices, not fixed
dependencies, and the review adds no Go dependencies.

- **2.1 Core and shell.** App bootstrap, router, protected-content flash, click interception,
  popstate, header breakpoints, footer, loading/empty/error/retry states.
- **2.2 Shared layer.** API client (cancellation, problem-type mapping), UI primitives, Greek
  catalog completeness (no scattered literals), the form directive.
- **2.3 Auth features.** Sign-in, register, account/profile, avatar upload, forced-change flow,
  session state machine.
- **2.4 Blog feature.** List, detail, downloads rendering, comments UI, favorite hearts, staff
  forms.
- **2.5 Projects feature.** List, detail, slug display, comments UI, favorites, staff forms.
- **2.6 Search feature.** Type filter, sort control (date default), date-window inputs, URL-synced
  query/cursor, empty and error states.
- **2.7 Notifications feature.** Feed page, badge event, read/read-all, per-item dismissal
  (`DELETE /api/v1/notifications/{id}`), `clear-read`, `delete-all`, role-weighted visibility.
- **2.8 About page.** Copy, community links.
- **2.9 Cross-cutting QA.** HTML semantics (landmarks, labels, headings), accessibility (keyboard,
  focus, ARIA, WCAG 2.2 target/keyboard rules, axe results, rendered re-checks: about-row tint
  contrast in both themes, the `::before` marker's screen-reader announcement, Safari list
  semantics), SEO (meta, headings, canonical URLs, Lighthouse; the SPA's canonical-URL/OpenGraph
  policy stays an owner decision), design tokens (the shared colour variables, typography),
  responsive widths, and a test sweep across both suites: coverage (a Go `-coverprofile` per
  package plus the web tests; report only, no threshold until numbers exist) and test value —
  redundancy, vacuous pins, and edge-case gaps.

### AppSec checklist (Pass 3)

- **3.1 Authentication flows.** Sign-in, register, password-change, and reset contracts; legacy
  bcrypt 72-byte verifier handling; session expiry; revocation through the auth version; reset
  tokens (single use, hashed, short TTL).
- **3.2 Session, CSRF, and origin chain.** Cookie flags, the opaque session shape, chain order
  (the rule is [route-chains.md](../patterns/go/route-chains.md) rules 3–4: origin before the path
  limits, then session, then the forced-change gate, then CSRF), trusted-origin enforcement.
- **3.3 Authorization hierarchy.** Role floors per endpoint, peer protection, the last-active
  super-admin guard, the draft-visibility gate, masked 404s that never disclose existence.
- **3.4 Staff operations.** User-admin operations, destructive operations, the agreed audit-event
  list, no-op 200 semantics.
- **3.5 Input validation.** Strict bodies (unknown fields rejected), ID/cursor/ETag parsing,
  `If-Match` preconditions, body hard limits (auth 4 KiB, content writes 512 KiB, media 10 MB).
- **3.6 Media pipeline.** Magic-byte detection, hostile inputs (decompression bombs, polyglots),
  path traversal, storage layout, orphan sweep.
- **3.7 Comments and hearts.** The plain-text contract, the depth-1 rule, cascade deletes,
  counter-drift guards, moderation audit events.
- **3.8 Secrets and outbound.** Application-side secret wiring, SMTP credentials, VAPID keys, and
  error paths that never leak secret material.

### Staff checklist (Pass 1)

- **1.1 `internal/database` and `internal/store`.** Schema against the feature docs and the wire
  contract, transactions, NULL-scan read-pairs, SQL quality, auth-version revocation.
- **1.2 `internal/handler`, `internal/routes`, `internal/middleware`.** Wire contracts against the
  OpenAPI document, chain order, problem types, DTO shapes, 422 violation codes.
- **1.3 `internal/auth`, `internal/identity`, `internal/problem`.** Business rules, session and user
  shapes, expiry logic, problem types and 422 shapes.
- **1.4 `internal/media`, `internal/search`, `internal/slug`, `internal/migration`.** The pure-Go
  pipeline, FTS5 query construction, Greek normalization, transliteration, import transforms.
- **1.5 `internal/audit` and `internal/config`.** Event schema, environment variables and defaults,
  the config loader and its secrets.
- **1.6 `cmd/*`.** `api` (startup/shutdown, sweeps, frontend serving), `migrate`, `backup`,
  `restore`, `importdata`, `importavatars`, `importmedia`, `fetchmedia`, `rebuildsearch`,
  `resetpassword`, `sweepmedia`, `docslint`.
- **1.7 `web/src` architecture.** Lit patterns, feature-folder discipline, state locality, API
  transport separation, TypeScript strictness, memory (leaks, listener cleanup, happy-dom restore
  pairing).
- **1.8 Cross-cutting ripple.** For each proposed change, trace every shipped behavior it would
  cascade into, in code and in data. Go conventions. Modern-feature simplification is a judgment
  call only; fashion never justifies churn.

### Skills

| Step                              | Skill                                                    |
| --------------------------------- | -------------------------------------------------------- |
| Research                          | `research`                                               |
| Review — both axes                | `code-review`                                            |
| Review — architecture vocabulary  | `codebase-design`                                        |
| Review — hard findings            | `diagnosing-bugs`                                        |
| Rulings                           | `grilling`; `domain-modeling` for records or terminology |
| Fixes                             | `tdd`                                                    |
| Writing or editing this procedure | `writing-for-agents`                                     |

## Examples

A batch header names the pass, the lens, and the batch:

```markdown
## Pass 4 — Platform (DevOps + SRE) — batch 4.5, SQLite runtime and SQL safeguards
```

The recorded shape of a finding, as written in the review workspace:

```text
4.5-003 (major) — WAL checkpoint starvation under the sweep cadence
Evidence: <what was observed, where>
Ruling: fix in batch — <the chosen change>; regression test <name>
```

A legal pause, at a batch boundary only:

```text
Pause after batch 4.3. Pass summary: release review, pass 4, next batch 4.4.
```

## Gotchas

- **Per-slice bugs are out of scope.** Each slice's own adversarial review already handled them;
  re-opening one here burns a batch and hides the cross-cutting drift this review exists to find.
- **Blockers never wait for a gate.** A blocker found while starting a batch is fixed then, before
  the batch continues.
- **The ripple order is not a preference.** Running the architecture pass before QA and AppSec
  invalidates their reviews and doubles the work.
- **Never half-pause inside a batch.** A pass summary written mid-batch describes a state nobody
  can resume from; findings and rulings are complete only at the batch boundary.
- **Nothing durable lives in scratch.** Anything the review must keep — a rule, a test, a fixture —
  lands in code or a durable doc, because the workspace is local and never committed.
- **Inventory is not a backlog.** Pass 0 records what exists and gates nice-to-have items; it never
  turns into a work list.
- **Tooling is per batch.** The axe-core and Lighthouse defaults come from each batch's research
  step, not from a standing dependency decision.

## Pointers

- Index: [../README.md](../README.md)
- SQLite rules the platform checkpoint reviews against:
  [../patterns/go/sqlite.md](../patterns/go/sqlite.md)
- Wire contract: [../api/openapi.yaml](../api/openapi.yaml)
- The other operations: [deploy.md](deploy.md), [backup.md](backup.md), [restore.md](restore.md),
  [cutover.md](cutover.md)
- Package navigation: [../../AGENTS.md](../../AGENTS.md); middleware chain order:
  [../../internal/middleware/AGENTS.md](../../internal/middleware/AGENTS.md)
