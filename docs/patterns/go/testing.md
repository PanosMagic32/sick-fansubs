# Go Testing

## Purpose

One agreed shape for a Go test in this repository: its name, its table, its failure messages, its
fixtures, and the database it runs against. What a scope must cover is the feature doc's business.

## Rules

1. Put a test in a `_test.go` file beside the code it tests. Use `package foo` when the test needs
   unexported symbols, and `package foo_test` for a test that exercises only the exported API.
2. Name tests `TestXxx`, benchmarks `BenchmarkXxx`, and examples `ExampleXxx`. Underscores appear only
   in these names and in `_test.go` file names.
3. Keep the fixtures a package shares in one file beside the tests — the openers, the seeders, and the
   small builders — under the house name `testutil_test.go`. The temp data-dir helper, owned per
   package, lives in that file under the name `testDataDir`; the one shared test-support package is
   `internal/store/storetest` (rule 4).
4. Seed a user row through `internal/store/storetest`, never with a hand-written `INSERT` in a test.
   Widen `UserSpec` when a test needs a column the fixture does not carry.
5. Write a table only where the cases share one shape and one comparison. Behavior that differs gets
   its own test function.
6. Give the row struct explicit field names and fill only the fields the case uses. Put a longer
   explanation in its own field and print it in the failure message, not in the subtest name.
7. Give every row a `name` and run the row with `t.Run(tt.name, ...)`. Never identify a row by index,
   and never label it `case #%d`.
8. Keep a subtest name short and identifier-like, so it still reads as one idea after the runner
   replaces its spaces with underscores. A name carrying a slash starts a nested subtest.
9. Use a subtest for a case's own setup, cleanup, or parallelism, and let no case depend on another
   having run first.
10. Compare stable, semantic results. Never assert on serialized bytes or on a formatted string that a
    package we do not own produces.
11. Keep comparisons on the standard library. Prefer checking the fields that matter; where no field
    is irrelevant, compare the whole value in one shot, clearing any noisy field. `reflect.DeepEqual`
    serves that comparison, and `github.com/google/go-cmp` is not a dependency.
12. Print got before want, and name the function under test plus the input:
    `YourFunc(%q) = %q, want %q`. The `expected ... got` order is forbidden.
13. Use `%q` for a string and `%+v` for a small struct; for a large value, print the differing part
    with a `-want +got` legend, not two full dumps. A setup failure needs less detail.
14. Prefer `t.Error`, so one run reports every failed check; in a loop with no subtests follow it with
    `continue`. Use `t.Fatal` only where a later check would be meaningless, and inside `t.Run`.
15. Test an error by presence, by `errors.Is`/`errors.AsType`, or by a property such as the offending
    parameter name in the message. Never compare the exact string of a package we do not own.
16. Begin a helper that can report a failure with `t.Helper()`. Helpers return values; they do not
    build an assertion layer that reports on the caller's behalf. A vocabulary-pin helper that
    compares two copies of one list may report through `t` (the audit pin's `assertVocabularyCopy`
    precedent).
17. Pass `*testing.T` after any context parameter in a helper, and take the test's own context from
    `t.Context()` — since Go 1.24 it returns a context that is canceled before the cleanup functions
    run (Go 1.24 release notes, Minor changes to the library → testing). The repo-wide migration
    status: new tests and touched suites take `t.Context()`; the remaining `context.Background()`
    sites in older suites are pre-existing debt that migrates on touch, not a convention.
18. Use the standard `testing` package only. No third-party test framework and no assertion library.
19. Open a real, fresh, temporary SQLite database through the package's helper, with the real
    migrations: `t.TempDir()`, the `internal/database` opener, and a `t.Cleanup` that closes the
    pool. Never mock it.
20. Reach no host outside the machine. Drive a handler with `httptest.NewRequest` and
    `httptest.NewRecorder`, never a bound port; a loopback stub covers the service the code calls.
21. Test time-dependent code on a fake clock, never a sleep: `testing/synctest.Test` runs a function
    in a bubble where time is virtual and the clock moves once every goroutine there is blocked
    (Go 1.25 release notes, New testing/synctest package); `synctest.Sleep` combines the sleep with
    the bubble's wait (Go 1.27 release notes, Minor changes to the library → testing/synctest).

## Pattern

A package's shared-fixture file (house name `testutil_test.go`) holds its opener; each test builds
its table, names every row, and prints got before want.

```go
// openStoreDB returns a fresh temporary SQLite database with every migration
// applied, closed when the test ends.
func openStoreDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := testDataDir(t)

	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}
```

```go
func TestFold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"case and accent fold", "Άμλετ", "αμλετ"},
		{"empty stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Fold(tt.in); got != tt.want {
				t.Errorf("Fold(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

## Examples

- Opener: `internal/store/testutil_test.go` — the shape the house fixture file carries: `t.Helper()`,
  `t.TempDir()`, real migrations, `t.Cleanup`.
- Named rows: `internal/config/config_test.go` — a `name` per row, run with `t.Run(tt.name, ...)`.
- Got before want: `internal/search/collate_test.go` — `t.Errorf("Fold(%q) = %q, want %q", ...)`.
- All failures at once: `internal/database/db_test.go` — the loop uses `t.Errorf`, not `t.Fatal`.
- Whole value: `internal/handler/staff_metrics_test.go` — clear the varying field, then compare the
  bodies with `reflect.DeepEqual`.
- Black-box: `internal/handler/health_test.go` — `package handler_test` with an `httptest` recorder.
- Seeding: `internal/store/storetest/AGENTS.md` — the shared user-row fixture and its contract.

## Gotchas

- `reflect.DeepEqual` also compares unexported fields and treats a nil slice or map as different from
  an empty one: clear the field that varies per request or per run before comparing the rest.
- A loop without `t.Run` has no case-scoped `t.Fatal`. After a failed `t.Error` the row must
  `continue`, or the next check runs against the state the failure left behind.
- A helper in `testutil_test.go` is compiled only into that package's test binary, so no other
  package can import it: shared fixtures belong in `internal/store/storetest`, past the guard test.
- A subtest name is not a failure message. The runner escapes it, prints its spaces as underscores,
  and reads a slash as a nesting boundary, so the identifying detail belongs in the `t.Errorf` text.

## Pointers

- Index: [../../README.md](../../README.md)
- Doc skeleton, link rules, and the decision-citation check: [../docs-conventions.md](../docs-conventions.md)
- The release-notes source list and the toolchain baseline: [toolchain.md](toolchain.md)
- The shared user-row fixture and its one raw-insert exception:
  [internal/store/storetest](../../../internal/store/storetest/AGENTS.md)
- Test, vet, and check targets: `make help`, defined in the [Makefile](../../../Makefile)
