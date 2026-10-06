# Go Comments

## Purpose

One agreed way to write a comment, so a reader finds the same kind of fact in the same place and
`go doc` renders something useful. The tree still carries comments written before these rules; fix
the ones in the code you touch.

## Rules

1. Write comments in English. User-facing copy stays in the message catalog.
2. Write a doc comment as a complete sentence that names the symbol it documents; an article ("A",
   "An", "The") may come before the name. Keep it as short as the context allows — no longer than
   the reader needs, no shorter than they need. A package comment follows rule 5 and may carry
   rule 11's structure.
3. Move rationale out of a short comment into the pattern or feature doc that owns it, and name
   that doc by its repository-relative path instead of restating it.
4. Give exported top-level names doc comments, and give unexported types and functions whose
   behavior is not obvious one too, in the same style, so exporting the name later needs no rewrite.
5. Open a library package comment with `Package <name>`; open a `main` package comment with
   `Command <name>`, `Binary <name>`, or `The <name> command`. Keep one per package, in one file,
   directly above the `package` clause.
6. Attach a doc comment directly above the declaration, with no blank line between.
7. Say what a function returns or does for its caller, not how it works. Write "reports whether" for
   a boolean, name result parameters only when the comment needs them, and name the special cases
   the caller must handle, such as empty input.
8. Say what an instance of a type represents, and say so when the zero value is not usable. Document
   every exported field in the type comment or in a per-field comment; a short field comment may sit
   at the end of its line.
9. Give a grouped `const` or `var` block one group comment plus short end-of-line comments; give a
   lone declaration a full sentence.
10. Mark a superseded declaration with a paragraph that starts `Deprecated:` and names its
    replacement, and mark an actionable remark with `TODO(uid):` or `BUG(uid):`.
11. Use the doc-comment syntax for structure: `#` for a heading on an unindented line, `-` and `1.`
    for lists, an indented span for a code block, `[Name]` or `[pkg.Name]` for a doc link, and
    `[Text]: URL` targets at the end of the comment. Indent a span only for preformatted text.
12. Write no banner or divider comment; a heading inside a doc comment uses the `#` syntax.
13. State a constraint, an invariant, or a non-obvious mechanism in a comment inside a body.
14. Never restate the statement below the comment, narrate review history, or cite a numbered
    decision record.
15. Use `//` line comments; reserve `/* */` for disabling a block of code. Keep a directive comment
    such as `//go:embed` at the end of a doc comment, where rendered docs hide it.
16. Wrap prose at 100 columns — this repository's chosen width, since the official sources set no fixed
    limit — and keep one width inside a file.
17. Run gofmt. It owns comment layout: it aligns end-of-line comments, normalizes doc-comment
    structure, and preserves prose without rewrapping. `make go-fmt` writes the result;
    `make go-fmt-check` inside `make check` fails a file that is not formatted.

## Pattern

A package comment carries the structure; the declarations stay short.

```go
// Package media owns the media storage layout and the image pipeline.
// The full contract lives in internal/media/AGENTS.md.
//
// # Layout
//
//	<data-directory>/media/images/<2-hex>/<id>.<ext>
package media

// Store resolves one processed image to a served path.
type Store struct {
	// DataDir is the absolute data directory. It must exist and be writable.
	DataDir string
}
```

## Examples

Real comments from the tree, shortened.

One-line doc comments (`internal/store/audit_store.go`, `internal/handler/users_admin_status.go`):

```go
// Write persists one audit record.
// UserSuspend returns the handler for POST /api/v1/users/{id}/suspend.
```

A package comment with a heading and a list (`internal/config/config.go`):

```go
// Package config provides typed, validated runtime configuration for the
// Sick-Fansubs application. It reads from environment variables, applies
// sensible defaults for development, and fails fast on invalid input.
//
// # Design principles
//
//   - Single source of truth: all env-var reading happens here.
```

A contract too long for a short comment moves to the owning doc; the comment keeps the sentence and
the pointer (`internal/database/backup.go`):

```go
// MigrationStatus reports the applied/embedded migration counts without
// applying anything; it refuses a present restore marker
// (docs/patterns/go/sqlite.md; internal/database/AGENTS.md).
```

An in-body comment explains the call below it (`internal/store/session_store.go`):

```go
	// Disambiguate "user not found" from "version changed". The lookup is
	// outside the transaction — a benign TOCTOU window that cannot produce a
	// wrong outcome.
```

The deprecation paragraph and the note markers have no instance in the tree yet; write them in this
form:

```go
// Deprecated: use Parse; ParseLegacy drops unknown fields silently.

// TODO(uid): drop the shim when the legacy importer is gone.
// BUG(uid): a zero limit reaches the store as "unbounded", not "no rows".
```

## Gotchas

- **A detached comment is not a doc comment.** A blank line between the comment and the declaration
  turns it into a floating comment; `go doc` shows nothing and the name reads as undocumented.
- **Indentation is meaningful.** A leading indent in a doc comment makes a code block, the classic
  rendering bug for a wrapped prose line.
- **Rationale is the usual violation.** A paragraph that argues for a design choice belongs in the
  doc that owns the rule, not in the comment.
- **Banners read as structure but carry none.** Replace one with a `#` heading in the doc comment of
  the declaration that introduces the section, or delete it. The same goes for a comment that
  restates the statement below it, and for a package comment pasted into several files.

## Pointers

- Index: [../../README.md](../../README.md)
- Documentation structure and skeletons: [../docs-conventions.md](../docs-conventions.md)
- The format targets and their checks: [Makefile](../../../Makefile)
- The scope's own `AGENTS.md` owns the rationale a comment points at when it outgrows a short
  comment.
