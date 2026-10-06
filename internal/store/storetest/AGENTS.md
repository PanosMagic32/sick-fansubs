# internal/store/storetest

Test support for seeding rows that other packages' tests need. The store
deliberately exposes no test-only creation path: registration goes through
`store.CreateUserAndSession`, which expects a password hash and also mints a
session, so a test that just needs a user row seeds one here.

## Open this file when

- a test needs a user row, a `UserSpec` field, or the `PasswordPlaceholder` verifier.
- touching the fixture's insert/read-back shape or the no-production-import guard.

## Folder-local conventions

- **Never imported by production code.** That is the whole point of the package,
  and `guard_test.go` fails `make check` (the test run, not `go build`) if a
  non-test file imports it. That test also fails when it cannot find the module
  root or sees no Go files at all, so it can never pass by walking the wrong
  tree.
- `UserSpec` carries the row's fields. `ID` and `Username` are required; every
  other zero value falls back to the fixture's usual shape (role `user`, status
  `active`, `auth_version` 1, `PasswordPlaceholder` as the verifier, no avatar,
  no forced password change, timestamps 1000, the lowercased username as the
  canonical form, and the lowercased username plus `@example.com` as the address
  — the same address production stores at registration). A zero `CreatedAtMS`
  selects the default; the schema requires a positive instant, just as it
  requires a positive `auth_version`, so neither zero is a legal row and both
  defaults are safe. Only override a field when the test pins a value that
  differs from these.
- `InsertUser(t, db, spec)` writes one row and fails the test on error. It takes
  `testing.TB`, so a benchmark or fuzz target can use it too.
- `PasswordPlaceholder` is the verifier the fixture stores when `PasswordHash`
  is empty. A test that asserts on the stored password compares against that
  constant, never a literal.

## Gotchas

- It writes the `users` row directly, so a **new NOT NULL column without a
  default** breaks it at the INSERT, and a column it does not read back escapes
  the comparison — the row tests pin the eleven columns they read. When you add
  or remove one, update the spec, the read-back list, and the row tests in the
  same commit.
- Every test that needs a user row uses this fixture; a hand-written
  `INSERT INTO users` in a test is drift waiting to happen. Sites that need a
  column the spec does not carry are a signal to widen the spec, not to bypass
  it.
- **The one recorded raw-insert exception:** `internal/database/restore_test.go`
  seeds a user with raw SQL because that database has only migration 1 applied,
  so the columns the fixture writes (`avatar_url`, `must_change_password`) do not
  exist yet. Any other raw site is a bug.

## Authoritative docs

- The store's production user creation path:
  [`CreateUserAndSession`](../session_store.go).
- Store conventions: [`../AGENTS.md`](../AGENTS.md).
