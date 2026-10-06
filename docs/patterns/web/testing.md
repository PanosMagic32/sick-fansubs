# Web Testing

## Purpose

One way to test the frontend: the runner, the DOM, how files and styles are read, how failures are
written, and where the machine-checked guards live.

## Rules

1. **`bun test` is the runner; tests live beside their subject** (`x.ts` / `x.test.ts`) in the same
   folder, and `make check` runs `bun test` through `web-test`. There is no watch mode in the gate
   and no coverage threshold configured.
2. **happy-dom is the DOM**, registered once per worker process. It is not a browser: media queries
   never evaluate, `dialog.showModal()` only sets the `open` attribute (no modality, no focus
   management), `<option selected>` is not honored after a detach, and there is no layout. Every
   behavior that depends on one of those is pinned against the source (the CSS regex pins, the
   `showModal` guard) instead of asserted through the DOM.
3. **Read files with `Bun.file`.** A fixture or a stylesheet is read as
   `await Bun.file(new URL("./x.css", import.meta.url)).text()`; `node:fs` is not used in web
   tests.
4. **`?inline` CSS is readable through the component.** `bunfig.toml` maps `.css` to Bun's text
   loader, so an imported `?inline` sheet is the real CSS string under test — a test may assert on
   the component's resolved `static styles` (the runtime baseline check does this). The loader
   mapping is a **runtime-wide** setting, not a test-only one: under `bun run`, a `.css` import also
   yields the file's text instead of Bun's default empty object. Nothing may depend on the import's
   value at runtime; this is recorded in [toolchain.md](toolchain.md).
5. **Two ways to read a sheet, two purposes.** Asserting what a component _renders_ goes through
   the `?inline` import (the resolved `static styles` above); asserting the _source_ — a geometry
   rule a browser would evaluate but happy-dom cannot — reads the file text with `Bun.file`. Prefer
   the resolved styles when the question is "did this rule reach the component", and the file text
   when the question is "does this exact rule still exist".
6. **A materialized component is asserted through its shadow root** after `await el.updateComplete`;
   `render()` helpers in the test file construct, mount, and clean up, and `afterEach` restores any
   global patch (a history/URL patch without its restore leaks across files in CI, which runs
   fewer workers than a dev box).
7. **Pin the contract, not the incidental.** A failure message says `got, want`; a test that reads
   CSS strips comments before matching (prose may name a retired value — the rule-blind pin
   lesson); a regex is bounded (`[^}]*` inside one block) so a match cannot reach into a later
   rule; a pin that guards a removed thing asserts the thing's absence positively.
8. **The architecture guards are one table-driven test file** (`web/src/architecture.test.ts`): one
   entry per guard, each with a bad snippet that must fail and a clean snippet that must pass
   (the `internal/codestyle` shape), plus a non-vacuity floor for the number of files the scan must
   touch. Add a guard by adding a table entry, not a new file; a check that spans files (the
   registration walk, the page-import existence walk) is a standalone test beside the table.
   The table covers:
   - route targets live at the feature root; every page module is imported (registered);
   - `shared/**` never imports `features/**`;
   - hex literals only in `app-shell.css`; every `var(--sf-*)` name is declared there;
   - the breakpoint allowlist; no `!important` outside the reduced-motion overrides;
   - every styled component imports both shared sheets _and_ uses them in its styles array;
   - every `<form>` keeps a native submit control in its own tree (the Enter-submission
     contract);
   - every `list-style: none` list keeps its semantics (`role="list"`);
   - every content surface renders cards through `<content-card>`; the retired per-surface card
     classes live only in the card's sheet;
   - a non-test `.ts` module stays at or under 700 lines (`*.test.ts` and the `catalog/el.ts`
     data table are exempt) — the rule-16 split ceiling;
   - no raw `fetch` outside the shared transport or the recorded footer health probe;
   - feature→feature imports stay in the recorded edge list (`components.md` rule 2).
9. **A CSS pin states why it exists** — the geometry it protects and why happy-dom cannot see it —
   and lives beside the component it pins. Cross-component CSS truths are pinned by the sheet's own
   suite: the control vocabulary and the icon target floor in `controls.test.ts`, the primary
   hover's darker fill and the token pairs in `core/shell/app-shell.test.ts`, the page-side token
   rule in `account-page.test.ts`.
10. **Timer, debounce, and fetch stubs are per test and restored in `afterEach`.** Shared helpers
    live in `shared/api/test-utils.ts` (the fetch doubles `mockFetchOnce`/`mockFetchImpl` +
    `restoreMockFetch`, the history-location shims, `waitFor`, `stubDebounceTimers`,
    `stubAbortSignalTimeout`, `stubProperty`, `setHappyDOMURL`/`resetHappyDOMURL`,
    `stripCssComments`, `stripSourceComments`); a third copy of
    a stub is the point to graduate it.
11. **Go-side tests are not the place for frontend behavior, and vice versa.** The only shared
    languages are the pins that read a frontend artifact from Go (the VAPID key copies, the audit
    event vocabulary) — those stay where their Go consumer lives.
12. **One process, shared globals, by choice.** `bun test` runs its default single-process model
    with one `GlobalRegistrator.register()` and no `--isolate`; the isolation cost is paid by the
    per-test restores above (rule 6) rather than by per-file workers. Coverage stays unconfigured —
    no threshold is a gate.

## Pattern

A component test that pins both behavior and a non-evaluable CSS fact:

```ts
test("the search submit drops to a full-width row on phones", async () => {
  const page = await renderPage();
  const submit = root(page).querySelector(".submit") as HTMLButtonElement;
  expect(submit.getAttribute("type")).toBe("submit");

  // happy-dom cannot evaluate media queries: the phone-band placement is
  // pinned against the CSS source.
  const phone =
    searchStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
  const button = phone.match(/\.submit \{([^}]*)\}/)?.[1] ?? "";
  expect(button).toContain("flex: 1 0 100%");
});
```

## Examples

- `web/src/architecture.test.ts` — the guard table and its self-checks.
- `web/src/core/styles/baseline.test.ts` — the runtime half: every spot-checked component's resolved
  styles carry both shared sheets.
- `web/src/shared/ui/controls.test.ts` — the variant vocabulary and the icon target floor.
- `web/src/shared/api/test-utils.ts` — the shared helpers listed in rule 10.
- `web/src/features/auth/account-page.test.ts` — the largest suite: session/favorites/sessions
  flows plus the source-level CSS pins for the card corner, the phone band, and the token rule.

## Gotchas

- **CI runs fewer workers than a dev box**, and Bun shares one happy-dom window per worker process.
  A suite that leaks a `history.pushState` patch, a property stub, or a global listener can pass
  locally and fail in CI; the paired restore in `afterEach` is load-bearing.
- **`Bun.file` on a missing path does not throw** until the content is read; a bad `new URL(...)`
  can silently assert against an empty string. Check that a `new URL` target still exists when a
  file moves.
- **`selected` on `<option>` is not honored under happy-dom** — a select's active value needs the
  imperative re-apply (`select[data-value]`) the pages already do.
- **A test that reads the whole component source** can be broken by a reflow; prefer the CSS block
  or the class list.
- **`bun test <path>` runs one file or folder** — use it while iterating; the full suite is the
  gate.
- **The blog and projects list suites stay separate until one is touched.** They are mirror suites
  with duplicated helpers, but the table-driven merge is parked; a third copy of any one test stub
  still graduates to the shared `test-utils` helpers (`shared/api/test-utils.ts`, rule 10).

## Pointers

- Index: [../../README.md](../../README.md)
- Component rules: [components.md](components.md)
- CSS rules and the pin shapes: [css.md](css.md)
- Toolchain and the Bun/happy-dom records: [toolchain.md](toolchain.md)
- Go-side test rules: [../go/testing.md](../go/testing.md)
