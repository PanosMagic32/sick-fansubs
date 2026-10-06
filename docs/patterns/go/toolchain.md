# Go Toolchain and Version Currency

## Purpose

Every pattern doc describes the language and standard library of the toolchain this repository
actually builds with, and the answers stay current when that toolchain moves. This doc owns the
pinned standard-library answers and the procedure that refreshes them.

## Rules

1. **Stay on the newest released toolchain, and read the pin from one place.** The `go` line in
   `go.mod` tracks the newest released Go: when a newer release lands, bump it and run the refresh
   procedure below in the same commit. That line is the language version and the minimum the build
   accepts; the container images and the CI workflow pin the same release in their own files, and one
   bump updates all of them together. Read the pin from `go.mod` — never bake a version into a doc,
   a comment, or a Makefile, and never assume the one installed on a laptop. No code here is written
   against an older release, and no superseded idiom is kept as current advice.
2. **A rule that depends on when a feature arrived names the release**, together with its
   release-notes section, because that is a historical fact and a reviewer can check it:
   "since Go 1.25, `sync.WaitGroup.Go` replaces the `Add`/`Done` pair (Go 1.25 release notes,
   _Minor changes to the library → sync_)".
3. **Say the current form, not the superseded one.** When the standard library subsumes an idiom,
   the doc, the code, and the examples use the standard form; the old form appears only in the
   replaced-idioms list below, labelled with its replacement.
4. **Refresh when the pin moves, in the same commit.** The procedure:
   1. Fetch the new release notes into a local workspace snapshot under `data/.research/` (gitignored,
      one file per covered version; the official URLs below stay the durable source).
   2. Read the language changes, the library changes, the `go` command changes, and the vet changes;
      note every item that touches code, tests, or the build.
   3. Apply the items that are directives: a new `go fix` modernizer, a new vet check, a changed
      default.
   4. Bump the pin everywhere it is declared: `go` in `go.mod`, the `golang:` image tags in the
      Dockerfiles, and the Go version in the CI workflows.
   5. Update the tables below and any rule a change makes stale. A rule that describes the old
      behavior is a bug in the doc.
   6. Grep the pattern docs for the language version and for any idiom the release deprecates.
5. **A rule that depends on the `go` directive or a build tag says so**, because the toolchain
   checks per file: `stdversion` (a `go vet` analyzer since Go 1.23, run by `go test` by default since
   Go 1.27) fails a build that uses a standard-library symbol newer than the file's effective
   version, and `loopclosure` (Go 1.22) flags a captured loop variable.
6. **Sources are the official ones.** Release notes (`https://go.dev/doc/go1.NN`), Effective Go, the
   Go wiki's Code Review Comments, Google's Go Style Decisions, the module-layout guide, the Go blog
   articles, and the package documentation for each standard-library package named here. Fetch the
   release notes for the version `go.mod` declares; a version claim with no source is a review
   finding.
7. **Never add a dependency for something the toolchain already ships**, and never keep a dependency
   that the toolchain subsumed. The exception is a package with no standard-library equivalent
   (bcrypt, the age file format, the SQLite driver) — that exception is recorded where it is used.
8. **Dependencies are not pinned in prose.** A dependency version lives in `go.mod`/`go.sum` (and
   `web/package.json`/`web/bun.lock` on the web side); no doc, comment, or Makefile states one. When a
   bump changes behavior, the doc that owns the behavior states the new behavior without the version.

## Pattern

The answer we write, grouped by the release that introduced it:

| Standard-library answer                                              | Since | Write it instead of                                                         |
| -------------------------------------------------------------------- | ----- | --------------------------------------------------------------------------- |
| `any`                                                                | 1.18  | `interface{}`                                                               |
| `errors.Is` / `%w` wrapping                                          | 1.13  | sentinel string comparison, `err == ErrX`                                   |
| `errors.Join`, several `%w` verbs in one `fmt.Errorf`                | 1.20  | concatenating error text                                                    |
| `slices` (`Contains`, `Equal`, `Sort`, `SortFunc`), `maps`, `cmp`    | 1.21  | hand-rolled loops, `sort.Slice`, `sort.Strings`, `sort.Sort`                |
| `slices` iterator helpers (`All`, `Backward`, `Values`, `Collect`)   | 1.23  | building an intermediate slice to iterate it                                |
| `min`, `max`, `clear` builtins                                       | 1.21  | manual comparisons, `for i := range m { delete(m, i) }`                     |
| per-iteration loop variables                                         | 1.22  | `x := x` aliasing before a goroutine                                        |
| `for range n`                                                        | 1.22  | `for i := 0; i < n; i++` when the index is only a counter                   |
| method-aware `ServeMux` patterns (`"POST /api/v1/x"`, `{id}`, `{$}`) | 1.22  | method checks inside the handler, hand-rolled path splitting                |
| range-over-func and `iter.Seq`                                       | 1.23  | push-style callbacks — only where a real need exists                        |
| `log/slog`                                                           | 1.21  | `log`, `log.Printf`, unstructured messages                                  |
| `omitzero` (JSON)                                                    | 1.24  | `omitempty` on a struct, a time, or an integer where zero is meaningful     |
| `os.Root`                                                            | 1.24  | `os.Open` plus a hand-written "is it inside this directory" check           |
| `testing.B.Loop`                                                     | 1.24  | `for i := 0; i < b.N; i++`                                                  |
| `sync.WaitGroup.Go`                                                  | 1.25  | the `wg.Add(1)` / `defer wg.Done()` pair                                    |
| `testing/synctest`                                                   | 1.25  | `time.Sleep` to let concurrent work finish (`synctest.Sleep` since Go 1.27) |
| container-aware `GOMAXPROCS`                                         | 1.25  | a manual `GOMAXPROCS` setting                                               |
| `errors.AsType[*T](err)`                                             | 1.26  | `errors.As(err, &target)`                                                   |
| `slog.NewMultiHandler`                                               | 1.26  | a hand-written multi-handler                                                |
| `go fix` modernizers                                                 | 1.26  | hand-written idiom churn — `make go-fix` runs before every commit           |
| `encoding/json` backed by `encoding/json/v2`                         | 1.27  | asserting an exact decode-error string: the text may differ from v1         |
| standard `uuid` package                                              | 1.27  | pulling a UUID module for new identifiers — this tree mints its own ids     |
| `strings.CutLast`, `bytes.CutLast`                                   | 1.27  | `LastIndex` plus manual slicing                                             |
| `stdversion` vet check inside `go test`                              | 1.27  | relying on review to notice a too-new symbol                                |

Replaced idioms, in the form a reader may still meet in old code or in older writing:

| Old form                                                     | Replacement                                                                                                                         |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| `interface{}`                                                | `any`                                                                                                                               |
| hand-rolled contains / equality / sort helpers               | `slices`, `maps`                                                                                                                    |
| `sort.Slice`, `sort.Strings`, `sort.Sort(sort.Reverse(...))` | `slices.SortFunc` with `cmp.Compare`                                                                                                |
| `log` / `log.Printf`                                         | `log/slog` with a level and key-value pairs                                                                                         |
| `x := x` before a closure or goroutine                       | nothing — the loop variable is per iteration                                                                                        |
| `wg.Add(1)` plus `defer wg.Done()`                           | `wg.Go(func() { ... })`                                                                                                             |
| a hand-written `slog` multi-handler                          | `slog.NewMultiHandler`                                                                                                              |
| `errors.As(err, &target)`                                    | `errors.AsType[*T](err)`                                                                                                            |
| `time.Sleep` in a concurrency test                           | `testing/synctest` with a fake clock                                                                                                |
| `os.Open` plus manual directory confinement                  | `os.Root`                                                                                                                           |
| `tools.go` blank-import files for build tools                | the `tool` directive in `go.mod` (the repo's own commands live in `cmd/`; the workflow linter is the one tool-directive dependency) |
| `golang.org/x/crypto/{sha3,pbkdf2,hkdf}`                     | the standard `crypto/{sha3,pbkdf2,hkdf}` packages                                                                                   |
| `github.com/google/uuid` for new identifiers                 | this tree's own id minter                                                                                                           |
| `expected …, got …` in a failure message                     | `got, want` (see [testing.md](testing.md))                                                                                          |

## Examples

- `internal/handler/response.go`, `internal/media/process.go`, `internal/push/push.go` — the `min` and
  `max` builtins, with no manual comparison.
- `internal/logging/read.go` — `slices.Backward` for the newest-first reader.
- `internal/database/backup.go` — `errors.Join` for the sweep's accumulated failures.
- `internal/handler/errors.go` — `errors.AsType[*auth.FieldError](err)`, the current form for a
  concrete target.
- `internal/routes/blog.go`, `internal/routes/comments.go` — the method-aware pattern table the
  router is built from.

## Gotchas

- **`go fix` does not catch everything.** The modernizers only match canonical shapes, so a leftover
  can survive every run; measured on this tree, `go fix -diff ./...` reports nothing while replaced
  idioms remain. Read the list, do not trust the tool alone.
- **A vet check can fail a test run after a toolchain bump.** `stdversion` turns a too-new symbol
  into a `go test` (vet) failure rather than a review note.
- **JSON error text is not a contract.** The decoder's message may change between toolchains; test the
  error kind, the field, or a property of the message, never its exact wording.
- **An opt-out is a decision, not a default.** `GOEXPERIMENT=nojsonv2` or `GODEBUG=tracebacklabels=0`
  may only be set with a recorded reason.
- **A new release note can make an existing pattern doc wrong.** The refresh step includes re-reading
  the docs that name a standard-library package.

## Pointers

- Index: [../../README.md](../../README.md)
- Mechanical guards for the replaced idioms:
  [`internal/codestyle`](../../../internal/codestyle/AGENTS.md).
- Package and naming rules: [structure.md](structure.md)
- Comment rules: [comments.md](comments.md)
- Test rules: [testing.md](testing.md)
- Type parameters and the generic helpers: [generics.md](generics.md)
- Local workspace release-note snapshots, created by the refresh step: `data/.research/` —
  gitignored, one file per covered Go version
- Official release notes: `https://go.dev/doc/go1.NN`, with `NN` read from `go.mod`
- Official guides: `https://go.dev/doc/effective_go`, `https://go.dev/wiki/CodeReviewComments`,
  `https://google.github.io/styleguide/go/decisions`, `https://go.dev/doc/modules/layout`
