# Route Chains

## Purpose

One mechanism for middleware: where a chain is composed, which shape a route picks, and the order
every shape shares. Each shape is a plain `func(http.Handler) http.Handler` composition over a
handler, built at registration time. A rule marked _house rule_ is this repository's own convention.

## Rules

1. **One file composes chains.** `internal/routes/chains.go` is the only file that uses a wrapping
   middleware; every other file names a constructor from it. The `internal/codestyle` guard
   `TestMiddlewareChainConfined` fails a buildable file that uses one anywhere else; it derives the
   wrapper set from the middleware package's own signatures — a written-out or named
   `func(http.Handler) http.Handler` — so a new wrapper is policed the moment it lands, and it
   catches a call, a function value, and a parenthesized selector alike. _House rule._
2. **Every chain starts with `RequestID`.** It mints the request ID and the request-scoped logger
   ([logging.md](logging.md) rule 1) and sets the `X-Request-ID` header; a route outside a chain
   answers without an ID, and its handlers log through the process default ([logging.md](logging.md)
   rule 2). Health probes apply the chain like any other route.
3. **Choose the narrowest shape that satisfies the route.** The five shapes, and the order each one
   executes in:

   | Constructor               | Execution order                                                                            | Used by                                                                      |
   | ------------------------- | ------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------- |
   | `publicChain`             | RequestID                                                                                  | public reads, health probes, media serving, `notFound()`                     |
   | `optionalSessionChain`    | RequestID → TrustedOrigin → Session                                                        | a public read that personalizes when a session is present (comments threads) |
   | `authenticatedReadsChain` | RequestID → TrustedOrigin → Session → ForcePasswordChange                                  | an authenticated read (notifications and push reads)                         |
   | `authenticatedChain`      | RequestID → TrustedOrigin → Session → ForcePasswordChange → CSRF                           | every write, and every authenticated read on a subtree that also has writes  |
   | `authChain`               | RequestID → TrustedOrigin → path-scoped IP limits → Session → CSRF (no forced-change gate) | the auth subtree                                                             |

4. **The order inside a chain is fixed where it is observable.** `TrustedOrigin` runs before a rate
   limit, so a foreign-origin request never spends quota. The forced-change gate sits after
   `Session` and before `CSRF`, so a flagged session always sees the actionable password-change
   problem. `CSRF` validates unsafe methods only. _House rule_ for the origin-before-limits slot.
5. **Wrapping order is reversed from execution order.** `h = A(h); h = B(h)` executes B before A;
   each constructor lists its wraps in reverse and names the execution order in its doc comment.
6. **Pick the chain per method, not per path.** One path may carry a public GET and an
   authenticated write; register each method with its own chain instead of wrapping the path once.
7. **Register a namespace's fallbacks through `notFound()`.** It applies the public chain, so an
   unmatched path still answers with the `X-Request-ID` header and the problem's `requestId`.
8. **Construct a limiter once, at registration.** A path-scoped IP limiter joins `authChain` as
   `pathLimit` data; a user-keyed bucket is injected into the handler that spends it, which owns the
   429 record ([errors.md](errors.md) rule 12). Each `pathLimit` matches one exact path, so the
   entries are independent and their relative order is unobservable.
9. **The forced-change exemption is structural.** Only `authenticatedReadsChain` and
   `authenticatedChain` carry the gate; `authChain` does not, which is what makes the auth
   subtree's exempt paths exempt.

## Pattern

```go
// internal/routes/chains.go — authenticatedChain, wraps listed in reverse of
// execution order (RequestID → TrustedOrigin → Session → ForcePasswordChange →
// CSRF).
func authenticatedChain(h http.Handler, db *sql.DB, secure bool, trustedOrigin string) http.Handler {
	h = middleware.CSRF()(h)
	h = middleware.ForcePasswordChange()(h)
	h = middleware.Session(db, secure)(h)
	h = middleware.TrustedOrigin(trustedOrigin)(h)
	return middleware.RequestID()(h)
}

// A registration site names the chain; it never composes one.
mux.Handle("GET /api/v1/blog-posts", publicChain(handler.BlogList(db, publicBaseURL)))
mux.Handle("PUT /api/v1/blog-posts/{id}", authenticatedChain(update, db, secure, trustedOrigin))
```

## Examples

- `internal/routes/chains.go` — the five constructors and the `pathLimit` pair.
- `internal/routes/blog.go` — one path, two chains: the public GET beside the authenticated write.
- `internal/routes/auth.go` — the path-scoped IP limits handed to `authChain` as data.
- `internal/routes/users.go` — one chain over a subtree mux, with the fallbacks registered inside it.
- `internal/codestyle/rules_test.go` — `TestMiddlewareChainConfined`, plus its positive control that
  the tree still shows the real composition in the only file allowed to hold it.

## Gotchas

- **A method-aware pattern without a method-less twin answers 405 in text/plain.** Inside the API
  subtree the fallbacks are the twin: the top-level `/api/v1/` pattern covers the whole subtree, and
  each stripped sub-mux (`auth.go`, `users.go`) registers its own `/`. A mux assembled without the
  top-level fallback — the routes tests build one — 405s where a twin is missing. The unversioned
  health paths register no twin: a wrong method there answers the mux's text/plain 405, outside the
  JSON-only API rule.
- **`http.StripPrefix` rewrites the path before the chain runs.** A path-scoped limit matches the
  stripped path (`/sign-in`, not `/api/v1/auth/sign-in`).
- **A path-scoped limit matches its path exactly.** Moving a route without moving its `pathLimit`
  leaves the bucket unwired and the route unlimited — the two are changed together.
- **The reads chains exist for the contract, not for behavior.** CSRF never gates a safe method, so
  an `authenticatedChain` on a GET passes through untouched; a read still uses
  `authenticatedReadsChain` where the endpoint's contract keeps the reads free of CSRF.
- **Tests may compose middleware directly.** A middleware's own behavior test wraps it by hand, and
  a handler test that needs a chain composes one — `internal/handler` cannot import
  `internal/routes` (import cycle). The guard checks buildable files only.
- **A nested chain reuses the request ID.** `RequestID` mints once per request: a fallback
  registered through `notFound()` inside a subtree mux that is already wrapped by a chain (the
  auth and users namespaces) reuses the ID the context carries, so the header, the problem body,
  and every log record share one identity.

## Pointers

- Index: [../../README.md](../../README.md)
- Logging, and the request ID a chain mints: [logging.md](logging.md)
- Errors, including the 429 record a limiter owns: [errors.md](errors.md)
- Package structure and naming: [structure.md](structure.md)
- Tests and their failure messages: [testing.md](testing.md)
- The route feature doc, per namespace: [`internal/routes/AGENTS.md`](../../../internal/routes/AGENTS.md)
- The middleware package: [`internal/middleware/AGENTS.md`](../../../internal/middleware/AGENTS.md)
- Mechanical guards for the checkable subset of these rules: [`internal/codestyle`](../../../internal/codestyle/AGENTS.md)
