# Generics

## Purpose

Fix when a type parameter earns its place, and make the standard library's generic helpers the first
answer. The rules below cover type parameters and those helpers; the concrete container and
pagination shapes are written where they land.

## Rules

1. **Write the concrete version first.** Add a type parameter only when a second real instantiation
   appears whose body is identical and only the types differ. One instantiation is not a reason.
2. **Never build a framework, a DSL, or an error-handling abstraction out of type parameters.** The
   constraint becomes the surface every caller must learn, and the abstraction hides more than it
   shares.
3. **Model shared behavior with an interface, or with a function value passed in, before reaching for
   a type parameter.** Take the varying part as an argument: a method can become a function value,
   not the reverse.
4. **Prefer the standard library's generic helpers to a hand-rolled loop or a new type parameter.**
   `slices` and `maps` (since Go 1.21 — Go 1.21 release notes, New slices package and New maps
   package), their iterator helpers (since Go 1.23 — Go 1.23 release notes, Iterators), and `cmp`
   (since Go 1.21 — Go 1.21 release notes, New cmp package) own the common algorithms. Do not park a
   local copy in a `util` or `helper` package.
5. **Prefer the language builtins to their hand-written forms.** `min`, `max`, and `clear` (since
   Go 1.21 — Go 1.21 release notes, Changes to the language: three new built-ins), and range over an
   integer (since Go 1.22 — Go 1.22 release notes, Changes to the language).
6. **Keep the constraint the smallest one that compiles.** A named constraint with a comment beats
   `any` plus a type switch. Add a type-set term only for an operation the body performs.
7. **Keep type parameters on the type or the function.** A method cannot declare its own; it uses
   the receiver's.
8. **Name a type parameter by its role, not by its first caller.** `T` for one element type, `E` for
   the element of an explicit container parameter, `S` for that container's type, `Key` and `Val` for a
   map's key and value, `Row` and `Item` for a stored row and its wire projection. Keep the names
   short.
9. **Let type inference supply the type arguments.** Write them at a call site only when inference
   fails; never reshape the signature around an inference gap.
10. **Instantiate concrete types in tests, and give a generic helper a production caller.** A helper
    whose only instantiation is a test is production code with no production use, so delete it or
    inline the concrete version.

## Pattern

One body, several concrete entry points:

```go
// readLegacyRecords decodes an export file as T, either the array or the JSON-lines shape.
func readLegacyRecords[T any](path string) ([]T, error) { ... }

func ReadLegacyProjectsFile(path string) ([]LegacyProject, error) {
	return readLegacyRecords[LegacyProject](path)
}
```

The generic worker stays unexported and each exported function names its record shape, so a reader
sees the type from the name and the decoding logic exists once.

## Examples

- `internal/migration/reader.go` holds `readLegacyRecords[T any]`, instantiated three times through
  exported wrappers: blog posts, projects, users. One body, three element types, no type switch.
- `internal/handler/collection.go` holds the collection mechanism: `collection[T]` is the one envelope,
  and `writeKeysetPage[Row, Item]` receives a `keysetPage` whose `read`, `item`, and `next` function
  values carry what differs per endpoint — the row type and its wire projection being the two
  genuinely varying types ([collections.md](collections.md)).
- The standard-library helpers are already the house answer, and no local generic package exists.
  `slices.Contains` (`internal/handler/list_params.go`, `internal/handler/push.go`),
  `slices.Backward` to walk a log file backwards (`internal/logging/read.go`), `slices.Sort` and
  `slices.SortFunc` with `cmp.Compare` (`cmd/backup/backup.go`), `maps.Equal`
  (`internal/handler/push_test.go`).
- Builtins do the small jobs: `min` and `max` clamp a retry-after to one second, size a push record,
  and bound a log page (`internal/handler/response.go`, `internal/push/push.go`,
  `internal/logging/read.go`); range over an integer drives repeated assertions
  (`internal/handler/health_test.go`, `internal/database/migrate_test.go`).
- `writeJSON(w, r, status int, v any)` (`internal/handler/response.go`) hands an arbitrary payload to
  `encoding/json`. That payload is heterogeneous by design; a type parameter would rename `v` and
  add no static check.

## Gotchas

- **A type parameter cannot carry per-type behavior.** When two call sites need different logic —
  different fields, different rules — write two functions. A constraint stretched to cover both ends
  up as a type switch behind an interface.
- **A type parameter is not a speed-up over an interface.** The implementation handles a
  type-parameter value much like an interface value, so convert an interface only when the body
  truly ignores the element type.
- **Reflection still wins where nothing else fits.** Values that carry no methods, with behavior that
  differs per type, go through reflection, as `encoding/json` does.
- **A package-level `min` or `max` function shadows the builtin.** Delete the helper; the builtin
  takes the same calls.
- **A type switch does not turn into a constraint by adding `[T any]`.** If a unifying interface
  exists, model it instead.

## Pointers

- Index: [../../README.md](../../README.md)
- The conventions every pattern doc follows: [../docs-conventions.md](../docs-conventions.md)
- The typed-wrapper rule for the migration reader:
  [../../../internal/migration/AGENTS.md](../../../internal/migration/AGENTS.md)
- The collection envelope and its page builder: [collections.md](collections.md)
