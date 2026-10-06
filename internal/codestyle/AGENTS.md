# internal/codestyle

The repository's mechanical code-shape guards: one test per guarded rule that a
pattern doc states and a machine can check. The package holds no buildable code —
every file is a `_test.go` — so nothing in the module can import it, and the
guards run inside the ordinary `go test ./internal/...` of `make check`.

## Open this file when

- adding or changing a mechanical guard, or the doc citation a guard carries.
- deciding whether a pattern-doc rule can be machine-checked at all.

## Folder-local conventions

- The guards cover a subset of the docs they name, not every rule in them:
  `docs/patterns/go/structure.md` rules 20, 10, 3, 16 and 4 (a production file that passes 500 lines is
  split by subject; a `_test.go` file is exempt), the `sort` package and
  `errors.As` idioms of `docs/patterns/go/toolchain.md`, `docs/patterns/go/logging.md`
  rules 2 (a handler logs through the request-scoped logger, never the package-level
  `slog` functions or the process default), 4 (an attribute key is never one of the
  record's own keys) and 5 (`writeInternalError` is the only 500 path for JSON
  endpoints; a literal or named 500 handed to a response writer, or a named
  status assigned for a later write, is flagged, a
  plain-text `http.Error` 500 stays allowed only in the media asset path, and a
  status used in a comparison or a return writes nothing), and `docs/patterns/go/errors.md` rules 3
  (an error is tested with `errors.Is`, never `==` or `!=` — `nil` stays allowed)
  and 8 (the `encoding/json` unknown-field message is the tree's ONE decode-error
  text match, confined to `internal/handler/errors.go` beside its pinning test),
  and `docs/patterns/go/route-chains.md` rule 1 (`internal/routes/chains.go` is the
  only buildable file that wraps a handler with middleware; a `_test.go` file stays
  exempt because `internal/handler` cannot import `internal/routes`), and
  `docs/patterns/go/collections.md` rule 1 (`internal/handler/collection.go` is the
  only non-test file that declares or builds the keyset collection envelope; a
  `_test.go` file may decode the wire shape), `docs/patterns/go/ids.md` rules 1
  and 8 (`internal/id` is the tree's only identifier minter; `internal/store`
  imports no media code), `docs/patterns/go/sqlite.md` rules 1 and 3 (the
  registered driver name `sqlite3` appears nowhere; DSN `_pragma` text is built
  only inside `internal/database`), `docs/patterns/go/sql-mapping.md` rule 5
  (the `null*` family is declared only in `internal/store/null.go`), and
  `docs/patterns/go/content-kinds.md` rule 1 (no non-test store file outside
  `internal/store/content_kinds.go` carries a per-content-kind table name or
  kind string as a string literal), and `docs/ops/deploy.md` rule 3 (the
  Caddyfile's `trusted_proxies` ranges equal the ufw `DOCKER-USER` sources in
  `deploy/bring-up.md`, set-for-set and non-vacuous).
- **A guard names its doc.** A failing guard prints the offending file (with
  line and column where the rule has a position) and the pattern doc that owns
  the rule. The doc is the source: a guard is added, changed, or deleted with it.
- **A guard proves itself before it scans.** Each test first checks that a bad
  snippet is flagged and a clean one is not (file-level guards probe their
  parsers and assert floors), then scans `cmd/` and `internal/` or the named
  file; a check that stopped matching fails instead of passing silently. A
  check may read a repository file outside the Go trees when its rule spans
  files; it keys on import paths, not identifiers, so an alias cannot dodge
  one.
- **The walk is anchored to the module root** (`go.mod` above the test's working
  directory), and `TestWalkerFindsTheTree` — the one test here that pins no rule —
  fails when either tree is short or missing a known file, so no guard passes by
  scanning nothing.
- `testutil_test.go` holds the shared harness; `rules_test.go` holds the AST guards
  and file-level guards live in their own subject-named test files.

## Gotchas

- A rule that needs type information — the `wg.Add(1)`/`defer wg.Done()` pair,
  for example — is not decidable by parsing alone; such a rule stays a review
  rule in the pattern doc and gets no guard here.
- A guard may exempt `_test.go` files where a fixture must contain the banned
  shape (the id-minting and DSN guards do): the rule polices production code,
  and the snippets in this package carry banned shapes on purpose.
- The snippets in `rules_test.go` contain banned shapes on purpose. They are
  string fixtures, never declarations, so a grep for `interface{}` or
  `errors.As(` will find them.
- The directory needs no build tags and no `Makefile` target: `make check` runs
  `go vet` and `go test` over `./internal/...`, which covers a test-only
  package.

## Authoritative docs

- Go structure and naming: [`../../docs/patterns/go/structure.md`](../../docs/patterns/go/structure.md)
- Logging mechanism and record shape: [`../../docs/patterns/go/logging.md`](../../docs/patterns/go/logging.md)
- Error vocabularies and boundary mapping: [`../../docs/patterns/go/errors.md`](../../docs/patterns/go/errors.md)
- Identifier minting: [`../../docs/patterns/go/ids.md`](../../docs/patterns/go/ids.md)
- SQLite foundation: [`../../docs/patterns/go/sqlite.md`](../../docs/patterns/go/sqlite.md)
- Value mapping: [`../../docs/patterns/go/sql-mapping.md`](../../docs/patterns/go/sql-mapping.md)
- Content-kind descriptors: [`../../docs/patterns/go/content-kinds.md`](../../docs/patterns/go/content-kinds.md)
- Middleware chains and where they are composed: [`../../docs/patterns/go/route-chains.md`](../../docs/patterns/go/route-chains.md)
- Toolchain and replaced idioms: [`../../docs/patterns/go/toolchain.md`](../../docs/patterns/go/toolchain.md)
- Test conventions: [`../../docs/patterns/go/testing.md`](../../docs/patterns/go/testing.md)
- Documentation structure: [`../../docs/patterns/docs-conventions.md`](../../docs/patterns/docs-conventions.md)
- Index: [`../../docs/README.md`](../../docs/README.md)
