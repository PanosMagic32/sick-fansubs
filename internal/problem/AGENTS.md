# internal/problem — Agent Navigation

RFC 9457 Problem Details writing.

## Open this file when

- adding problem types, changing problem bodies, or touching 422 violation shapes.

## Folder-local conventions

- New problem types must be mirrored in `docs/api/openapi.yaml`.
- `WriteInternal` is the shared masked 500 writer: it records one Error through the request-scoped logger and emits `/problems/internal-error`. `handler.writeInternalError` wraps it, and the middleware session gate calls it directly.
- `type` values are stable relative URIs under `/problems/...`; the authoritative request ID comes from the middleware, never the inbound header. A failed body write is recorded through the request-scoped logger (`logging.From`).

## Authoritative docs

- [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)
