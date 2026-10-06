# Go Structure and Naming

## Purpose

One agreed shape for packages, files, names, and statements, so any reader — human or agent — finds
code where they expect it and writes it the way the tree already reads. The rules come from the
official Go sources named in [toolchain.md](toolchain.md); a rule marked _house rule_ is this
repository's own convention, chosen where the sources are silent.

## Rules

1. **Two top-level trees.** `cmd/<program>/` holds programs, one directory each, in `package main`;
   `internal/<package>/` holds every shared package. Nothing is importable from outside the module.
2. **One package per directory**, every file in it declares the same package name, and the name is the
   directory's base name in lower case letters and digits, unbroken: `tabwriter`, never `tabWriter`
   or `tab_writer`.
3. **Name a package for what it owns**, not for a pattern or a bucket. `internal/identity` holds the
   user, session, role, and account-status values; `internal/auth` holds sign-in and session behavior;
   `internal/store` holds persistence. Never create `util`, `utility`, `common`, `helper`, `misc`,
   `api`, `types`, `interfaces`, `model`, or `testhelper` — the official naming rules reject those
   names, and a reviewer treats one as a blocker.
4. **A package grows by splitting files, not by splitting hairs**: when a package outgrows one file,
   add a file named for its subject (`blog_admin_store.go`, `clientlabel.go`), and keep the primary
   type near the top of the file that declares it (house rule: the sources set no file or field
   order). A production file that passes 500 lines is split by subject —
   `TestProductionFileSizeBounded` in `internal/codestyle` fails the check, and a `_test.go` file is
   exempt.
5. **Export switch is the first letter.** Do not export a name that only the package uses.
6. **Drop the package name from exported identifiers** (the official sources call this
   _repetition_): `auth.Service`, not `auth.AuthService`; `identity.User`, not `identity.IdentityUser`;
   `handler.Auth`, not `handler.AuthHandler`. Name a constructor `New` when the package's subject is
   the type it returns, and otherwise repeat only the type: `handler.NewAuth`.
7. **No `Get` prefix** on accessors — `Owner()`, `SetOwner()`. `Get` is kept only where the concept
   really is an HTTP GET.
8. **Initialisms keep one case**: `ID`, `URL`, `HTTP`, `API`, `DB`, `CSRF`, `JSON`, `SQL`. So `userID`,
   `SessionUser.CSRF`, `appURL`, never `userId` or `Url`. Millisecond fields carry the suite's `MS`
   suffix (`NowMS`, `CreatedAtMS`) — not `Ms`, `Millis`, or `_ms` (house rule).
9. **Constants are MixedCaps after their role**, never SCREAMING_CASE: `maxOpenConns`,
   `StatusSuspended`, not `MAX_OPEN_CONNS`.
10. **Write `any`, never `interface{}`.**
11. **Variable names omit the type word**: `users`, not `userSlice`; `userCount`, not `numUsers`. Scale
    length with scope: `r`, `w`, and `db` in a short function; a descriptive name for a value that lives
    long or crosses files.
12. **Receivers are short, uniform, and never `this`/`self`/`me`**: `func (s *Service)`,
    `func (w *Writer)`, `func (db *sql.DB)`.
13. **Default to pointer receivers.** Use a value receiver only for a small unchanging value, never mix
    receiver kinds within one type, and never copy a struct whose methods need the pointer.
14. **Signatures stay on one line.** Name result parameters only when two results share a type or a
    deferred closure writes one, and do not use naked returns outside a tiny function.
15. **Define an interface where it is consumed, not where it is implemented.** Accept interfaces,
    return concrete types, and add an interface only when a second implementation, a real seam, or a
    meaningful decoupling exists — never for a mock alone, and never exported only for tests.
16. **Import order**: standard library, then third-party modules, then this module's packages, in
    separate groups. Never `import .`. `import _` belongs to a `main` package or a test that needs the
    side effect; a library package must not carry one.
17. **Handle errors first and return**, so the normal path stays unindented. Keep `if`, `for`, and
    `switch` headers on one line.
18. **Declare empty slices as `var t []string`**, not `t := []string{}`, and never build an API where
    nil and empty mean different things.
19. **Struct literals name their fields** for any type declared outside the current package. Omit
    zero-valued fields when the result still reads clearly, and build a value with one composite
    literal instead of assigning field by field.
20. **A `WriteString` argument is a literal or a value, never a concatenation.** A concatenated
    argument is flagged by the editor and hides the fragment's shape behind Go's `+` seams. Write a
    literal fragment as its own call and interpolate values with `fmt.Fprintf`:

    ```go
    // No: sb.WriteString(" ORDER BY " + titleExpr + " ASC LIMIT ?")
    fmt.Fprintf(&sb, " ORDER BY %s ASC LIMIT ?", titleExpr)
    ```

21. **No initialization side effects.** `init` may register something that cannot be a declaration
    (a collation, a driver); it never loads configuration, opens a database, or starts a goroutine.
22. **A new scope ships with its doc.** A new package directory needs its `AGENTS.md` in the same
    commit — `make docs-lint` fails without it.
23. **gofmt is the arbiter** of formatting. There is no line-length limit: wrap a line when its meaning
    is clearer wrapped, indent the continuation with a tab, and rearrange the code when gofmt's output
    reads badly rather than working around the formatter.

## Pattern

A package whose subject is one thing exposes the thing, not its package name:

```go
package auth

// Service holds the sign-in, registration, password, and session rules.
type Service struct { /* ... */ }

func New(db *sql.DB, sender mail.Sender, base string) *Service
```

Callers read `auth.New(db, sender, base)` and `*auth.Service`; the wire, the store, and the handler
layer never define a second name for the same value.

## Examples

- `internal/audit` — one subject, one file pair, exported `Writer`/`Record`, no package-name prefix.
- `internal/search` — `FunctionFold` and `FilterTokens` name what they return; no `util` package wraps
  them.
- `internal/store/metrics_store.go` — the `sinceMS` parameter carries the `MS` suffix, and
  `internal/handler/favorites.go` uses `userID`, so the initialism rules read the same in both layers.
- `internal/auth/clientlabel.go` — one mechanism (user-agent classification) in one file named for the
  subject, with its bound pinned in the same file.

## Gotchas

- **A rename touches four things at once**: the directory, the package clause, every import path, and
  every markdown link to the package. `make check` catches the code; `make docs-lint` catches the
  links.
- **`go vet` and gofmt are gates, not suggestions.** An unused import or a misformatted file fails
  `make check` before a reviewer sees the change.
- **A banned package name is cheaper to avoid than to argue about.** Renaming later means the same
  mechanical churn plus a doc pass.
- **`WriteString` concatenation is invisible in review** and visible to the editor; the fix is one
  `fmt.Fprintf` call that keeps the fragment in one place.

## Pointers

- Index: [../../README.md](../../README.md)
- Mechanical guards for the checkable subset of these rules:
  [`internal/codestyle`](../../../internal/codestyle/AGENTS.md).
- Comments and doc comments: [comments.md](comments.md)
- Tests: [testing.md](testing.md)
- Type parameters and the standard-library helpers: [generics.md](generics.md)
- The pinned toolchain, the sources, and how they are refreshed: [toolchain.md](toolchain.md)
- Documentation rules for every doc kind: [../docs-conventions.md](../docs-conventions.md)
- Rules of the project as a whole: [`AGENTS.md`](../../../AGENTS.md)
