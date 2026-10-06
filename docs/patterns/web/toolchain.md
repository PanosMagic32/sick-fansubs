# Web Toolchain and Version Currency

## Purpose

Every web pattern doc describes the toolchain this repository actually builds with, and the answers
stay current when that toolchain moves. This doc owns the pinned answers of the web stack, the
deliberate divergences from official guidance, and the refresh procedure.

## Rules

1. **Stay on the newest released toolchain.** The frontend pins live in `web/package.json` and
   `web/bun.lock`; the Bun version is the `oven/bun` tag in `Dockerfile`. When a newer release
   lands, bump the pin and run the refresh procedure below in the same commit. Read a pin from its
   manifest — never bake a version into a doc, a comment, or a Makefile, and never assume the one
   installed on a laptop. No web code here is written against an older release, and no superseded
   idiom is kept as current advice.
2. **Build to the latest web standards; there is no obsolete-browser floor.** The output keeps
   `build.target: "esnext"` on purpose — no legacy transpilation, current-engine syntax is expected.
   A platform feature may be adopted when current engines ship it; Baseline status is recorded in
   the docs as information, never as a 30-month gate. Runtime APIs the build cannot transpile
   (`AbortSignal.timeout`, `AbortSignal.any`) are used directly, with no compatibility fallbacks.
3. **A rule that depends on when a feature arrived names the introducing release** where the source
   states one (a Baseline "newly available" date, a changelog entry) — that is a historical fact a
   reviewer can check, not a pin.
4. **Dependencies are not pinned in prose.** A package version lives in `web/package.json` and
   `web/bun.lock` (and `go.mod`/`go.sum` on the Go side); no doc, comment, or Makefile states one.
   When a bump changes behavior, the doc that owns the behavior states the new behavior without the
   version.
5. **Refresh procedure** when a pin moves:
   1. Read the release notes or changelog for each moved package (Lit, Vite, TypeScript, Bun,
      happy-dom) and the TypeScript release notes for the compiler behavior changes.
   2. Note every item that touches code, tests, or the build — a changed default, a new
      deprecation, a renamed option, a default-browser or target change.
   3. Apply the directive items and update the tables below; a rule that describes the old
      behavior is a bug in the doc.
   4. Bump `web/package.json` and refresh `web/bun.lock` (`bun install`), bump the Bun tag in
      `Dockerfile` when Bun moved.
   5. Run `make check` and `make web-build`; both must pass in the same commit.
   6. Grep the docs and `web/src` for a version string that survived — version-provenance comments
      in code (for example "verifiable in the installed package source") state the mechanism, never
      the number.
6. **Vite ignores the `target` in `tsconfig.json`.** `build.target` in `web/vite.config.ts` decides
   the browser output; the tsconfig `target` governs `tsc --noEmit` and Bun only. A change to one
   is not a change to the other.

## Pattern

The recorded divergences from official guidance — each deliberate, each with its reason:

| Divergence                                                            | Official guidance                                                                                                                                  | Our reason                                                                                                                                                                                                                                                                 |
| --------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `?inline` CSS imports wrapped in `unsafeCSS`                          | Lit documents `css` tagged templates and shared style modules; bundler `?inline` is not documented                                                 | The sheets are trusted in-repo files, so the `unsafeCSS` caveat (untrusted CSS can phone home) does not apply; the toolchain composes the sheets and the guards keep them honest                                                                                           |
| `build.target: "esnext"`                                              | Vite's default is `baseline-widely-available`                                                                                                      | The product ships on current engines; no legacy transpilation                                                                                                                                                                                                              |
| `verbatimModuleSyntax` unset                                          | TypeScript's bundler guidance recommends it for bundled projects                                                                                   | With `isolatedModules` and `noEmit` the practical risk is smaller, but it is a known gap: type-only imports stay explicit by convention, and enabling the flag is a refresh item                                                                                           |
| `dom.iterable` in `lib`                                               | TypeScript merged it into `dom` and left the two `dom.*iterable` files empty — the 6.0 announcement says projects "can now simplify to just `dom`" | Harmless redundancy; removing it is a refresh item, not a behavior change                                                                                                                                                                                                  |
| `resolve.alias` hand-synced with tsconfig `paths`                     | Vite honors `paths` through `resolve.tsconfigPaths`                                                                                                | That option is documented as costly and discouraged; three aliases are cheap to keep in step, but the second source of truth is recorded here                                                                                                                              |
| Vite config builds aliases from `URL.pathname` + `decodeURIComponent` | Vite requires absolute alias targets; older docs showed the `fileURLToPath` recipe, current docs do not                                            | No Node imports (Bun is the one runtime), and Vite may evaluate its config outside Bun, so Bun-only APIs (`import.meta.dir`) are unavailable; the resulting paths are POSIX — dev, CI, and the Docker build are Linux, so `fileURLToPath`'s Windows handling is not needed |
| `[loader] ".css" = "text"` in `bunfig.toml`                           | Bun's runtime default for `.css` is an empty object (Bun 1.4 release notes, the `.css` runtime change)                                             | The mapping makes `?inline` sheets inspectable under test; it is runtime-wide, so nothing may depend on a `.css` import's value under `bun run` either                                                                                                                     |
| `module: "ESNext"` + `moduleResolution: "bundler"`                    | TypeScript suggests `preserve` as the closest fit for a bundler and Bun                                                                            | `bundler` is inside the documented recommendation; moving to `preserve` is a refresh item, not a correctness fix                                                                                                                                                           |
| `experimentalDecorators: true` + `useDefineForClassFields: false`     | TypeScript's default changes with the target; modern guidance favors standard decorators                                                           | Lit documents this combination for decorators, and standard decorators need the `accessor` keyword and emit larger output                                                                                                                                                  |
| `showModal()` guarded in components                                   | The `<dialog>` element is the platform's modal                                                                                                     | happy-dom's `showModal()` is a stub that only sets the `open` attribute (no modality), so the guard and the attribute fallback stay as the recorded test-DOM shape                                                                                                         |

Replaced forms a reader may still meet in old code or in older writing:

| Old form                                      | Replacement                                                                  |
| --------------------------------------------- | ---------------------------------------------------------------------------- |
| hand-mixed `rgba()` tints of a token          | `color-mix(in srgb, var(--sf-token) N%, transparent)`                        |
| `var(--sf-x, #fallback)`                      | `var(--sf-x)`, with the token declared in `app-shell.css`                    |
| `!important` to win a specificity fight       | a correct selector, or a `:where()`-level shared base                        |
| a hand-rolled `role="dialog"` modal           | the native `<dialog>` element                                                |
| a `<div role="button">`                       | a native `<button>` (or `<a href>` for navigation)                           |
| `aria-label` on a naming-prohibited element   | visually hidden text (`.sr-only`)                                            |
| a manual `AbortController` chain              | `AbortSignal.any([...])` / `AbortSignal.timeout(ms)`                         |
| JSON round-trip cloning                       | `structuredClone()` — available, unused today                                |
| hand-rolled grouping loops                    | `Object.groupBy`, `Map.groupBy`, the `Set` methods — available, unused today |
| `Promise` executor boilerplate for a deferred | `Promise.withResolvers()` — available, unused today                          |

## Examples

- `web/vite.config.ts` — the deliberate `esnext` target and the hand-synced aliases.
- `web/tsconfig.json` — the decorator pair, the strict family, and the `paths` the aliases mirror.
- `web/bunfig.toml` — the runtime-wide `.css` text loader.
- `web/src/shared/api/request-signal.ts` — `AbortSignal.any` composed with `AbortSignal.timeout`.
- `web/src/features/notifications/ui/push-subscriptions-section.ts` — the guarded `showModal()` call.

## Gotchas

- **A Vite, Lit, or TypeScript release can invalidate a rule here.** The refresh procedure includes
  re-reading the pattern docs that name a changed option.
- **The `.css` loader mapping is runtime-wide.** A future `bun run` entry point that imports CSS
  receives the file text, not an empty object; do not write code that reads that value.
- **`tsc --noEmit` and the built bundle disagree by construction.** Vite transforms; `tsc` only
  type-checks. A syntax the tsconfig accepts may still be lowered (or not) by the build.
- **Bun's type packages and Vite's client types are both in `types`** because the test runner and
  the build see different globals; removing one breaks a different check than it looks like.
- **happy-dom lags the platform.** Check any platform API used in a component against the runner
  before assuming it behaves as in a browser (the `showModal` stub is the recorded precedent).
- **A self-referencing controller field needs its type written out.** A class-field initializer
  that passes the host (`readonly subs = new Subs(this)`) is circular when two sections reference
  each other through the host interface; a compiler generation that detects the cycle eagerly
  reports an implicit `any` (TS7022) where a later one resolves it lazily. Annotate the field
  (`readonly subs: Subs = new Subs(this)`) — the pinned compiler and the editor's language server
  then agree, and the contract is visible at the field. The two section fields in
  `web/src/features/notifications/ui/push-settings.ts` are the recorded shape.

## Pointers

- Index: [../../README.md](../../README.md)
- Component and control rules: [components.md](components.md)
- CSS rules: [css.md](css.md)
- Test rules: [testing.md](testing.md)
- Go-side toolchain mirrors: [../go/toolchain.md](../go/toolchain.md)
- Official sources: `https://lit.dev/docs/`, `https://vite.dev/guide/`,
  `https://www.typescriptlang.org/docs/`, `https://bun.sh/docs/`, `https://web.dev/baseline`,
  `https://developer.mozilla.org/`, `https://html.spec.whatwg.org/multipage/`,
  `https://www.w3.org/WAI/ARIA/apg/`, the WCAG 2.2 Understanding pages
