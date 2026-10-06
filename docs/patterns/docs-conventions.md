# Documentation Conventions

## Purpose

One agreed structure for every document, so a human or an agent finds the same kind of fact in the
same place, and so drift fails a check instead of a review.

## Rules

1. **One source of truth per fact.** A fact is written once. Every other document and every code
   comment links to it instead of restating it.
2. **Five kinds of document.** A pattern doc (`docs/patterns/**`) fixes the one agreed way to do
   something. A feature doc (`AGENTS.md` beside the code) describes one scope. An operations doc
   (`docs/ops/**`) describes a procedure that runs against a real environment. The architecture doc
   (`docs/architecture.md`) maps the running system: its context, containers, flows, and topology.
   `docs/api/openapi.yaml` is the wire contract. Nothing else is built.
3. **A scope has a doc.** A scope is a Go package (`internal/<pkg>`, `cmd/<tool>`), or a web scope
   (`web/src/core`, `web/src/shared`, and each `web/src/features/<feature>`). Every scope has an
   `AGENTS.md`. Adding a scope means adding its doc in the same commit — `make docs-lint` enforces it.
4. **Every pattern, operations, and architecture doc is reachable from the index.** `docs/README.md`
   links each one — `make docs-lint` enforces it.
5. **Doc skeleton.** Every pattern and operations doc uses these sections, in this order, omitting one
   only when it would be empty: `Purpose`, `Rules`, `Pattern`, `Examples`, `Gotchas`, `Pointers`. The
   architecture doc uses `Purpose`, `Context`, `Containers`, `Request flow`, `Data flows`,
   `Deploy topology`, `Rejected by design`, `Pointers`. A feature doc (`AGENTS.md`) opens with a
   one-paragraph scope, then uses the navigation sections it needs, in this order: `Open this file when`, `Placement` (where the scope's code belongs and what
   it must not absorb; omit when empty), `Folder-local conventions` (the contracts and holds a caller
   or reviewer must know), `Gotchas`, `Authoritative docs`.
6. **File names are kebab-case** and describe the content. No numbers, no dates, no owner names.
7. **Links are relative and must resolve.** Never link to a decision record or a planning document;
   link to the pattern or feature doc that owns the fact. `make docs-lint` enforces link integrity.
8. **Write for the reader who has the code open.** Short sentences, one idea each, active voice,
   imperative rules. Say _what_ and _why_; leave history out. History lives in git.
9. **Code comments state intent.** A comment explains a constraint, a non-obvious mechanism, or a
   decision the reader cannot see from the code. It never restates the statement below it, never
   narrates review history, and never cites a decision number. Exported symbols carry doc comments;
   the rules are in [go/comments.md](go/comments.md).
10. **English for docs and code; Greek only for user-facing copy** in the message catalog
    (`web/src/shared/catalog/el.ts`). Server-rendered email is the one recorded exception: its Greek
    copy ships from `internal/mail` — the catalog cannot serve it — and
    [`internal/mail/AGENTS.md`](../../internal/mail/AGENTS.md) is its home.
11. **Follow the toolchain `go.mod` declares; never bake a version into a doc.** Pattern docs describe
    the language and standard library of the current pin — read `go` in `go.mod` (and `go version`)
    instead of assuming one, and fetch the official docs for that version when a rule depends on API
    behavior. A rule that depends on when a feature arrived names the introducing release and cites that
    release notes section — for example "since Go 1.25, `sync.WaitGroup.Go` subsumes the `Add`/`Done`
    pair (Go 1.25 release notes, _Minor changes to the library → sync_)". That is a historical fact,
    not a pin. An idiom superseded by the standard library is written in its current form (for example
    `slices`/`maps` over hand-rolled helpers, `any` over `interface{}`, `log/slog` over `log`,
    `min`/`max` over manual comparisons). The standard-library answers, the replaced-idiom list, and
    the refresh procedure live in [go/toolchain.md](go/toolchain.md). **Dependency versions are never
    baked into a doc either.** They live in `go.mod`/`go.sum` and `web/package.json`/`web/bun.lock`;
    a doc that depends on one points at its home. The only versions a doc names are a language or
    toolchain version (Go read from `go.mod`, Bun read from the `oven/bun` tag in `Dockerfile`) and an
    introducing release where a rule depends on when a feature arrived.
12. **A doc that no longer describes live behavior is fixed or deleted in the commit that changes the
    behavior** — never left to rot, never archived in-tree.
13. **Decision references are extinct.** The numbered decision records are retired; git history is
    their archive, and no living file cites one. `make docs-lint` fails on any counted file that
    carries a reference. The one recorded exemption is `internal/database/migrations/**` — the
    applied migration files are checksummed history and cannot be edited
    ([go/sqlite.md](go/sqlite.md), rule 8).

## Pattern

A pattern doc states rules as imperatives, then shows the shape once:

```markdown
## Rules

1. Every collection endpoint answers with `{items, pageInfo}`.

## Pattern

func writeKeysetPage[Row, Item any](w http.ResponseWriter, r *http.Request, op string, page keysetPage[Row, Item])
```

A feature doc records what the scope owns in one paragraph, then the contracts a caller or a reviewer
must know, then gotchas that cost someone time.

## Examples

- Pattern doc: this file. The skeleton it documents is the one it uses.
- Feature doc worth copying: `internal/store/AGENTS.md` — scope, then the invariants a reviewer must
  check, then the recorded holds.
- Rationale that belongs in a pattern doc rather than a comment: "handlers log once, at the boundary"
  (the rule), not the story of the bug that revealed it (git history).

## Gotchas

- **A shipped runbook may restate the contract it operates.** An operations doc the deploy workflow
  ships to a machine without the repository — or a one-time owner runbook beside the scripts it
  ships with, under `deploy/` — may state the operator-visible facts it needs; the scope's feature
  doc links to it instead of keeping a second independent description.
- **Renaming or moving a doc requires updating `docs/README.md` in the same commit**, or the guard
  fails.

## Pointers

- Index: [../README.md](../README.md)
- The guard: `cmd/docslint`, wired as `make docs-lint`
