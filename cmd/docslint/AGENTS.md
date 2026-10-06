# cmd/docslint

The documentation guard. It enforces four rules from `docs/patterns/docs-conventions.md`: every code
scope has an `AGENTS.md` feature doc, every pattern, operations, and architecture doc is linked
from `docs/README.md`, relative markdown links resolve, and no living file cites a retired decision
record. It runs as
`make docs-lint` inside `make check`.

## Open this file when

- changing a check, its skip rules, or the decision-citation exemption.
- touching the Makefile wiring or the exit-code contract of `make docs-lint`.

## Folder-local conventions

- `cmd/docslint` checks the tree rooted at the current directory and exits non-zero on any
  problem, printing one line per problem. Exit codes: 0 clean, 1 problems found, 2 the check could not
  run (unreadable file, unexpected argument).
- `-root <dir>` checks another tree; the tests call the check functions directly and through `run`.
- **The citation pattern covers all four forms** (`decision NNNN`, `decision/NNNN`, `decision-NNNN`,
  and a `decisions/NNNN-…` file path) in prose or a comment, across Go, web, docs, Makefiles, and
  Dockerfiles. Any match outside the exemption fails the run. The counted universe is the
  house-prose and configuration kinds (`citationExtensions` plus `citationNames` and
  `Dockerfile*`); vendored asset kinds (`.txt` licenses, `.svg` images) are out of it by design —
  they hold third-party content, not house text.
- **One path is exempt by ruling: `internal/database/migrations/**`.** Applied migration files are
  checksummed history — editing one breaks every deployed database — so a decision reference in their
  comments stays. The exemption is stated in `docs/patterns/go/sqlite.md` rule 8 and
  `internal/database/AGENTS.md`.
- Skips by design: the local/build directories (`.git`, `node_modules`, `data`, `dist`); the local `.scratch` workspace is skipped.
  Citation counting skips this command's `_test.go` files only, because a fixture must contain the
  forms the tool matches.
- Markdown inside fenced code blocks and inside inline code spans is ignored, so a Go example such as
  `g[T](arg)` is not mistaken for a link.

## Gotchas

- A link resolves against the directory of the file that contains it; a moved doc must update every
  inbound relative link.
- The index check requires a real link: a prose mention of a doc's path does not satisfy it.

## Authoritative docs

- Rules and skeleton: `docs/patterns/docs-conventions.md`
- Makefile target: `docs-lint`
