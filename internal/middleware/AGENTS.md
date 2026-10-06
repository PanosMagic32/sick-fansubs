# internal/middleware — Agent Navigation

Cross-scope HTTP middleware: request IDs, session resolution, CSRF, trusted origin, rate limiting, context helpers. No business policy.

## Open this file when

- changing request context, session/CSRF/origin enforcement, rate limits, or middleware ordering.

## Folder-local conventions

- `RequestID` is the ONE place a request ID is minted and the one place a request-scoped logger is built: it mints through `internal/id` — `id.New`, with `id.Fallback` on an entropy failure (logged once; the degraded value keeps the 32-character shape) — attaches `logging.From(ctx).With("requestId", id)` and the ID to the request context, and sets the `X-Request-ID` header. A nested chain reuses the ID already in the context, so one request keeps one identity. Every chain starts with it, including the health routes — a handler that skipped it writes no ID (the handler package mints none). See [`docs/patterns/go/logging.md`](../../docs/patterns/go/logging.md) and [`docs/patterns/go/ids.md`](../../docs/patterns/go/ids.md).
- The chains are composed in `internal/routes/chains.go` — change the order there, not here; the rules are in [`docs/patterns/go/route-chains.md`](../../docs/patterns/go/route-chains.md).
- Ambiguity policy: duplicate session cookies = unauthenticated; duplicate `X-CSRF-Token` / `Origin` = rejected; a duplicate `Referer` is rejected when it is the fallback (no `Origin`). `SessionCookies` / `SessionDigestValue` exist so nobody falls back to `r.Cookie`.
- Never log the sign-in rate-limit key — the identifier may be an email address. (The password-change limiter's key is the user ID, which is logged — permitted.)
- `ClientAddr(r)` (`clientaddr.go`) is the ONE client-address resolver: the left-most `X-Forwarded-For` entry when it parses as a bare IP, else the peer address with its port stripped (bracketed IPv6 included). A malformed or zone-bearing forwarded entry never becomes a bucket key or an audit row. The rate limiter and every audit emitter use it, so a bucket and a ledger row cannot disagree. It deliberately adds no Go-side allowlist — the app publishes no host port and Caddy parses the header against the static Cloudflare ranges (`trusted_proxies_strict`), which is the whole trust argument; the same reasoning is documented on the function and in the Caddyfile.
- Context keys are unexported types; only this package's getters/setters touch them.
- Forced-change gate: `force_password_change.go` rejects a flagged session with 403 `/problems/auth/password-change-required`. The exemption is STRUCTURAL — the gate sits only in `routes.authenticatedReadsChain` and `routes.authenticatedChain`; the whole auth subtree chain (`routes.authChain`) never includes it, which is what makes its exempt paths exempt. It runs after Session, before CSRF, so a flagged user always sees the actionable problem; an unauthenticated request passes through, and the handlers own their 401s.

## Authoritative docs

- [`docs/patterns/go/route-chains.md`](../../docs/patterns/go/route-chains.md)
