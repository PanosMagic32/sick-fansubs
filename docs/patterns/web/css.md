# Web CSS

## Purpose

One styling system: where a component's CSS lives, how it reaches a shadow root, which values may
be literal, and how layout and responsive rules stay predictable.

## Rules

1. **A component's styles are `static styles` arrays.** The shared sheets come first, the
   component's own sheet last (later wins on equal specificity):

   ```ts
   static styles = [
     unsafeCSS(sharedStyles), // @core/styles/baseline.css
     unsafeCSS(controlStyles), // @shared/ui/controls.css
     unsafeCSS(styles), // this component
   ];
   ```

   A component that renders a form control adds `unsafeCSS(formStyles)`
   (`@shared/ui/forms.css`) in the same list. Every styled component must import both shared sheets
   — `web/src/architecture.test.ts` enforces it, and `core/styles/baseline.test.ts` spot-checks the
   resolved styles at runtime.

2. **`?inline` plus `unsafeCSS` is the file-to-sheet bridge, and it is a deliberate divergence.**
   Lit documents `css` tagged templates and shared style modules; it does not document a bundler
   `?inline` import (see [toolchain.md](toolchain.md)). The mechanism is safe here because every
   input is a trusted, in-repo `.css` file — the `unsafeCSS` caveat is about untrusted CSS "phoning
   home" through an injected URL, which cannot happen when the CSS ships from this repository.
   Selectors inside the sheets are written unscoped: shadow DOM isolates each component's copy.
3. **Never write a raw colour value in a component sheet; every colour is a token.**
   `--sf-bg`, `--sf-surface`, `--sf-border`, `--sf-text` (and `-muted`/`-dim`), `--sf-accent`,
   `--sf-accent-light`, `--sf-accent-strong`, `--sf-on-accent`, `--sf-error`, `--sf-success`,
   `--sf-success-strong`, `--sf-warning`, `--sf-project`, `--sf-project-bg`, `--sf-focus-ring`,
   `--sf-dot`, `--sf-accent-tint`, `--sf-link-on-tint`, and the `--sf-brand-*` tone pairs live in
   `core/shell/app-shell.css`, the
   token home. It declares every token twice (`:host` and `:host(.light)`), so both themes are
   complete; adding a token means adding both values. `architecture.test.ts` fails a hex literal in
   any other sheet and fails a `var(--sf-…)` name the token home does not declare. Two recorded
   exceptions exist outside the sheets: `core/index.css` — the global document sheet cannot see the
   shell host's custom properties, so its document-level focus baseline keeps a literal fallback —
   and `web/index.html`'s `theme-color` metas, which the browser reads before any CSS loads. The
   one accepted raw-colour exception INSIDE a sheet — a neutral black drop shadow — is recorded in
   rule 5. Colour literals that are asset data rather than theme colours (the emoticon glyph palette
   in `features/comments/ui/emoticons.ts`) are recorded where they live and stay out of the token
   set.
4. **No fallback values inside `var()`.** `var(--sf-accent, #8aaae0)` hides a typo and a missing
   theme value behind a stale literal; write `var(--sf-accent)`. The guard's token check exists
   precisely because the fallbacks would hide those mistakes.
5. **Tints of a token use `color-mix()`.** A translucent variant of a token is
   `color-mix(in srgb, var(--sf-brand-tracker) 10%, transparent)` (the about page rows, the
   notification result chips) — not a hand-mixed `rgba()` of the same colour, which drifts from its
   token. Neutral overlays that are not a token (a white/black wash on a code block) stay literal.
   The one accepted raw-colour exception inside a sheet: a neutral black overlay used as a drop
   shadow (`rgba(0, 0, 0, 0.2)` and similar) stays literal under rule 3 for now; tokenizing shadows
   is deferred.
6. **Breakpoints are a closed set.** The accepted ladder is `480px`, `560px`, `640px`/`641px`
   (the footer pair), `700px`/`700.01px` (the content band and the header's desktop-only boundary),
   `1150px` (the two content lists), `1300px` and `1920.01px` (the header ladder and the wide
   shell). The header keeps its own ladder by design — its elements differ per size — and the
   content widths stay as accepted. A new width needs a recorded reason in the guard's allowlist;
   `architecture.test.ts` fails an unlisted one. Consolidating the ladder is optional, isolated future
   work, reviewed live in the browser.
7. **`!important` is allowed only in the reduced-motion override** (`core/index.css` and
   `core/styles/baseline.css`): the global animation clamp must beat component rules whatever their
   shape, so removing it would silently disable the accessibility preference. Anywhere else it is a
   specificity failure — fix the selector instead. The guard fails every other occurrence.
8. **Focus indication is `:focus-visible`, defined once per context.** The global sheet styles the
   document and `baseline.css` mirrors the rule into every shadow root; a component that needs its
   own focus look overrides that rule locally. The skip link reveals itself on `:focus` — the
   recorded exception, because any focus that reaches a skip link is keyboard-originated. Never
   remove an outline without providing an equally visible replacement.
9. **The shell owns the shrink guard.** `main > * { min-width: 0 }` in `app-shell.css` keeps a
   route root from widening the page past the viewport: a flex item's automatic minimum size is its
   content's min-content width, so one unshrinkable row would add a horizontal scrollbar to the
   whole page. A grid track that must shrink says `minmax(0, …)` — a grid paints its track minimums
   whatever the container does. The guard is box-scoped — a descendant that still overflows is the
   owning component's fix.
10. **Motion respects the user preference** through the universal reduced-motion clamp plus
    component-local opt-outs where an animation is the point (none today). Layout state that must
    be visible while scrolling (the sticky header) is fine; a transition that is the only signal of
    a state change is not.
11. **Typography has one home.** `core/index.css` owns the five `@font-face` faces (vendored IBM
    Plex Sans 400/500/700 plus the italic, and IBM Plex Mono 500, each `font-display: swap`) and the
    document body stack. A shadow root that needs the mono face uses the `--sf-font-mono` token
    declared in `core/shell/app-shell.css` — never a repeated family literal.

## Pattern

A component sheet header states what the sheet owns, then the rules keep tokens and the shadows
keep isolation:

```css
/* Admin logs tab. The page chrome comes from admin-page.css; this sheet owns
   the filter row and the two tables. */

.logs-filter select,
.logs-filter input {
  padding: 0.35rem 0.6rem; /* dense toolbar: beats forms.css's base */
}
```

Recipes for the recurring changes:

- **Add a token.** Declare it in `core/shell/app-shell.css` twice (`:host` and `:host(.light)`) with
  a one-line comment saying what it means; use it everywhere through `var(--sf-…)`. The guards fail
  a hex literal in any other sheet and an undeclared name.
- **Add a breakpoint.** Add the width to the allowlist in `web/src/architecture.test.ts` with the
  reason in a comment, and add the style block; unlisted widths fail the guard.
- **Change a control look.** The variants live in `shared/ui/controls.css` (see the control-variant
  recipe in [components.md](components.md)); a component overrides only what is genuinely local
  (position, size in a band, one colour nuance).

## Examples

- `web/src/core/shell/app-shell.css` — the token home (both themes), the skip link's `:focus`
  exception, the `main > *` shrink guard.
- `web/src/features/about/about-page.css` — `color-mix()` tints over `--sf-brand-*` tokens, with no
  literal anywhere in the sheet.
- `web/src/features/admin/ui/admin-page.css` — dense filter controls that override the shared form
  base; `.admin-filter select` wins because the shared base is `:where()`-level.
- `web/src/shared/ui/controls.css` — the control vocabulary, including the `--icon` target floor.
- `web/src/shared/ui/forms.css` — the zero-specificity form base and the `.field` block.
- `web/src/shared/ui/content-card.css` — four card shapes in one sheet, each keyed on the host's
  `shape` attribute; the element's `::slotted()` rule is the only placement a slot owns.

## Gotchas

- **Removing a hex fallback can reveal a missing token.** Run the architecture guards after any
  colour change; they check both halves (no literals, no undefined names).
- **happy-dom cannot evaluate media queries**, so every breakpoint-dependent behavior is pinned
  against the CSS source by a test (the search page's phone-band submit, the pager chip's narrow
  band, the account page's stacking). Write those rules in a `@media` block whose shape a regex can
  find — `.phone = styles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)`.
- **A `:where()` base is intentionally weak.** `forms.css` styles controls at zero specificity so a
  component's own rule always wins; a rule that must beat the shared base cannot be written with
  `:where()` itself.
- **happy-dom drops `::slotted()` rules when it parses a sheet** (it keeps the rest of the sheet), so
  a placement or a size that lives in `::slotted()` is pinned against the CSS source — never through
  a rendered style.
- **Token values are per theme and both are live at once** (the shell toggles a class, it does not
  rebuild styles); a rule that assumes one theme looks wrong in the other until the owner reports
  it.

## Pointers

- Index: [../../README.md](../../README.md)
- Component and control rules: [components.md](components.md)
- Test rules for the CSS pins: [testing.md](testing.md)
- The styling mechanism's toolchain record: [toolchain.md](toolchain.md)
- Token declarations: `web/src/core/shell/app-shell.css`
- The guards: `web/src/architecture.test.ts`
