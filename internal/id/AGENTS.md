# internal/id

Identifier minting for the whole application. `New` produces every stored identifier from 16
`crypto/rand` bytes as 32 lowercase hex characters; `Fallback` is the never-fail degraded value the
request-id path uses when the entropy source fails. The package imports nothing from the module —
it is a leaf, so every layer may depend on it. The pattern rules live in
[`docs/patterns/go/ids.md`](../../docs/patterns/go/ids.md); `internal/codestyle` guards the
single-home rule (`TestIDMintingConfined`) and the store's independence from media
(`TestStoreDoesNotImportMedia`).

## Open this file when

- minting an identifier, or changing the `New`/`Fallback` shapes.
- touching the store's id path or the request-id fallback.

## Folder-local conventions

- `New() (string, error)` returns exactly 32 lowercase hex characters; the error wraps the entropy failure, and callers add their own operation context (`store: <op> id: %w`) or answer the masked 500.
- `Fallback() string` never fails and is always 32 characters: the `ffffffffffff` marker prefix, 12 hex characters of the wall-clock millisecond, and 8 hex characters of a per-process counter. `middleware.RequestID` is its only caller; the value is a correlation handle, never a secret and never stored.
- `internal/store` mints through this package and imports no media code (`TestStoreDoesNotImportMedia`).

## Gotchas

- **The fallback's uniqueness is per process.** The counter makes calls distinct within one running
  binary; a multi-process deployment would need its own discriminator (the application is a single
  writer process today).
- **Do not grow imports here.** Any module import turns the leaf into a dependency and invites a
  cycle with the packages that mint ids.
- **The counter wraps at 2^32 and the millisecond at year ~10889.** Both are far beyond the life of
  a degraded path, and the shape (32 hex characters) is the contract that matters.

## Authoritative docs

- Identifier rules: [`docs/patterns/go/ids.md`](../../docs/patterns/go/ids.md)
- SQLite foundation: [`docs/patterns/go/sqlite.md`](../../docs/patterns/go/sqlite.md)
- The request-id consumer: [`internal/middleware/AGENTS.md`](../middleware/AGENTS.md)
- Mechanical guards: [`internal/codestyle/AGENTS.md`](../codestyle/AGENTS.md)
- Index: [`docs/README.md`](../../docs/README.md)
