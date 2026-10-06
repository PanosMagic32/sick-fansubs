# Web Components, Structure, and Controls

## Purpose

One agreed shape for the frontend tree: where a file lives, how components compose, which controls
exist, and the platform contracts (forms, dialogs, accessibility) every component follows.

## Rules

1. **One scope layout for every feature.** A feature folder holds its route targets at its root
   (`features/<feature>/<page>.ts`, with the page's `.css` and `.test.ts` beside it), every other
   component in `ui/`, pure non-Lit helpers in `utils/`, and API modules and DTOs in `data-access/`.
   `shared/` holds the cross-feature units (`shared/ui`, `shared/utils`, `shared/api`, `shared/pwa`,
   `shared/catalog`); `core/` holds the app frame (router, shell, service worker, styles).
   `web/src/architecture.test.ts` fails a page module under any `ui/`, and fails a registered page
   module (a `core/index.ts` import) that does not exist on disk.
2. **Layer contracts.** `shared/**` never imports `features/**` and never calls an API. Shared
   components are generic: they receive data through properties, render it, and dispatch events.
   Feature `ui/` children may own local state and call `data-access`; pages own route, URL, and
   session wiring (a page reads the URL and the session context, loads through `data-access`, and
   hands values down). Features reach each other only through the recorded capability edges: the
   session context (`@features/auth/data-access/session.js`, open to every feature), the detail
   pages' `favorite-toggle`/`follow-toggle` (auth) and `comments-section` (comments), and the
   account page's `push-settings` (notifications). `web/src/architecture.test.ts` guards the list; a
   new edge gets an entry there and a sentence here. `core/` imports features only under its own
   recorded exceptions (element registration in `index.ts`, the session-aware shell and header;
   test files may import the elements they exercise, e.g. `baseline.test.ts`'s style enumeration).
3. **A component is a custom element.** Every element registers with `@customElement("<kebab-case-name>")` and
   extends `LitElement`. Prefer `@property` for anything the owner sets, `@state` for private
   state; the public properties are the component's API and a component never writes its own public
   properties except in response to user input, where it dispatches an event to inform the owner
   ("properties down, events up"). Add an `HTMLElementTagNameMap` entry whenever a TypeScript call
   site consumes the element's type (`document.createElement`/`querySelector` casts then disappear);
   elements referenced only from templates need none.
4. **Lifecycle.** Post-render DOM work goes in `updated()` (and `firstUpdated()` for one-time work);
   changing a property inside them schedules another cycle, changing it earlier does not. Listeners
   on anything other than the component's own DOM (window, document, other elements) are added in
   `connectedCallback` and removed in `disconnectedCallback`. Await `updateComplete` in tests
   before asserting.
5. **Composition over inheritance and over reactive sharing.** A child relationship is a component;
   reusable template logic is a directive (`formField` is the tree's directive); cross-tree data
   uses `@lit/context` (`provide`/`consume` — the session context is the tree's only one). Choose a
   controller over a mixin unless a feature must add public API. A helper function that returns a
   template stays a helper while it has no state, no events, and no second page; the moment it has
   any of those, it becomes a component (the granularity checklist: own state, own template, used
   in more than one place, one job, well-defined API). The tiebreaker between a helper function and
   a child element is a house rule: lit.dev documents both templates and components but no decision
   rule between them.
6. **Events cross shadow boundaries only when composed.** A child that must be heard outside
   dispatches `new CustomEvent("sf-…", { bubbles: true, composed: true })` in response to user
   interaction or an async state change, never in response to a property the owner set. The shell's
   `_onLinkClick` reads `e.composedPath()` for exactly this reason, so an anchor inside a child
   still routes as a link.
7. **Controls are CSS classes on native elements.** `.button` plus at most one variant
   (`--primary`, `--secondary`, `--danger`, `--ghost`, `--link`) and the `--icon` / `--sm`
   modifiers live in `shared/ui/controls.css`; `shared/ui/controls.ts` owns the typed
   `buttonClass()` helper. Every action control uses the vocabulary, and every component imports
   `controls.css` beside `baseline.css` — `architecture.test.ts` enforces the import, and the
   rendered sheet is spot-checked in `core/styles/baseline.test.ts`.
8. **Never render a button through a child component's shadow root.** A submit control stays a
   native `<button type="submit">` in the same shadow root as its `<form>`. The HTML Standard
   defines the form's default button as the first submit button — `<button>` or
   `<input type=submit>` — in tree order, and a form-associated element associates only with a
   `form` ancestor in its own tree. A custom element is not a submit button, and a button inside a
   child component's shadow tree has no form owner: either shape silently breaks Enter-key implicit
   submission on every form with two or more blocking fields. `architecture.test.ts` walks every
   `<form>` block and fails one that has no native submit button in the same file. Do not adopt
   form-associated custom elements (`formAssociated` + `ElementInternals`) without an owner
   decision; Lit documents nothing for them and the platform rules above are the whole story.
9. **Forms: raw controls carry the look.** `shared/ui/forms.css` styles bare `input`, `select`, and
   `textarea` (excluding checkbox/radio/file) at zero specificity, plus the `.field` block and its
   message lines; a component that renders a visible form control imports it. Dense toolbars keep
   raw controls and their own compact overrides (the admin filter rows); there is no forced
   wrapper. The `formField` directive (shared/ui/form-field.ts) is the optional convenience for a
   labeled field with error wiring — autocomplete tokens, `aria-invalid`, `aria-describedby`.
10. **Dialogs use the native `<dialog>` element** with `showModal()`, `aria-labelledby` pointing at
    its heading, and a close control; the element provides the focus trap, focus restore, Escape
    handling, and the inert backdrop that a hand-rolled `role="dialog"` cannot — an explicit
    `role="dialog"`/`aria-modal` over it is redundant and is not written. When a dialog renders only
    while some state says so (the admin reset result), opening is a post-render step in `updated()`,
    guarded by `dialog.open` so a re-render is a no-op; a permanently rendered, click-opened dialog
    (push-settings) calls `showModal()` in its handler. happy-dom's `showModal()` is a stub that
    only sets the `open` attribute — no modality, no focus management — so a component guards the
    call (`typeof dialog?.showModal === "function"`) with the `open`-attribute fallback for a DOM
    without the method, and the test suite asserts the dialog's markup and state transitions
    instead of modal behavior.
11. **Accessibility is a construction rule, not a review afterthought.** Native semantics first:
    a control is a `<button>`, a navigation is an `<a href>`, never a repurposed `div` with a role.
    Icon-only controls carry a catalog accessible name (`aria-label`), and a control whose action is
    not obvious from the glyph also carries `title` with the SAME string (the row actions do; the
    header, pager, and toggle icons rely on the accessible name alone). The glyph carries
    `aria-hidden="true"` by construction (`icons.ts`). Icon targets are at least 2rem (36px at the
    root font size) by house ruling — above the WCAG 2.5.8 minimum, below MDN's 44px
    recommendation, matching the row density of the row actions.
    Elements with naming prohibitions (`code`, `span`, `div`…) never carry `aria-label` — use
    visually hidden text (`.sr-only`). Data tables carry a `<caption>` (visually hidden is fine).
    Status text lives in a live region that is mounted before its text changes. A region that mounts
    together with its text at initial render is accepted for initial-load empty/loading states;
    status text that changes after an action must live in a region that persists across the change
    (the push-settings shape). Headings, lists, landmarks, and `fieldset`/`legend` groups reflect the
    real structure.
12. **The catalog is the only copy source.** Every user-facing string comes from
    `shared/catalog/el.ts` (Greek); an anglicism rendered as-is carries `lang="en"` — a
    WHOLE-VALUE label takes the scope, while a Latin loanword inside a Greek sentence renders plain
    (Greek speech with the loanword is the intent). Components never inline Greek text.
13. **Boundaries that are deliberately not controls** (house rule). Three shapes keep their own
    surface styling instead of `.button`: a row-entry button (the notifications feed item), an ARIA
    tab (`role="tab"` in the account tabs), and a disclosure row (the member card's head). They are
    list entries, tab stops, and row openers — not actions. The control vocabulary covers actions
    and links.
14. **ARIA tab sets follow the APG Tabs contract.** The tablist is ONE tab stop (roving tabindex —
    the active tab carries `tabindex="0"`, the rest `-1`), Left/Right walk the tabs with
    wraparound and Home/End jump to the ends, activation follows focus (automatic), and every tab
    carries `aria-selected` and a panel that is `aria-labelledby` its tab. A tab set whose panels
    mount on activation omits `aria-controls`: the IDREF would dangle while the panel is out of the
    DOM, and a dangling IDREF fails axe (the account-chip disclosure sets the precedent). The
    association is carried by the panel's `aria-labelledby` and by the focus move to the activated
    panel. The keyboard path keeps focus on the tab so the arrows keep walking; a direct activation hands
    focus to the activated panel — the panels' `tabindex="-1"` exists for exactly that move.
    `features/auth/account-page.ts` is the reference implementation.
15. **Content cards are one shared element.** A surface that lists or leads with content composes
    `<content-card>` (`shared/ui/content-card.ts`) instead of hand-rolling card markup. The element
    owns four shapes — `grid` (the blog/projects list cards), `hero` (the blog list's featured
    post), `row` (the search results and the favorites/admin lists, with `dense` for their compact
    geometry) and `detail` (a detail page's lead block) — each keyed on the host's `shape`
    attribute in `content-card.css`. The `grid` and `hero` shapes read «Title – Subtitle» on one
    line: the subtitle sits beside the title behind a decorative separator, and the `row` shape
    renders the same inline line when a `subtitle` input is set — every row surface passes one when
    its kind has a subtitle (search, favorites, and the blog staff rows; projects have none). A row
    without one keeps the plain title and its `secondary` line, and the `detail` shape keeps its
    stacked subtitle. A caller
    fills typed inputs
    (`href`, `title`, `heading-level`,
    `subtitle`, `description`, `thumbnail-url`, `badge`/`badge-tone`, `secondary`, `meta-label`/
    `meta-instant`, `creator`/`published-at`/`sr-username`, `updater`/`updated-at`, and the two
    counts that drive the stats chip) and fills the slots; it never restyles a shape. The element's
    doc comment is the input-by-shape contract. Per-item controls ride the `actions` slot (in-flow:
    a row's trailing control, the detail title row's toggles and staff edit link) or the `overlay`
    slot (a linked row's corner control — the element places it and reserves the room). A linked
    card never carries an in-flow action: an interactive control inside the card's own anchor is
    invalid, so a linked row uses `overlay`. The surface styling lives only in
    `shared/ui/content-card.css` — `architecture.test.ts` fails the retired per-surface classes
    anywhere else, and fails a card surface that renders no `<content-card>`. One recorded
    exception: the admin users member list (`admin-users-page.ts`) is an interactive management
    surface — collapsed heads with staged selects and a per-member panel — rather than a content
    listing, and keeps its own `.member-card` markup (`ui/member-section.ts`) and styling
    (`admin-users-page.css`).
16. **An oversized element splits into section controllers, not per-section shadow roots.** A
    non-test web module stays at or under 700 lines (`architecture.test.ts` fails one past the
    ceiling; `*.test.ts` and the `catalog/el.ts` data table are exempt). When an element outgrows
    that, extract each cohesive section — its state, handlers, and templates — into a reactive
    controller in a companion module under the scope's `ui/`, rendered by the host through the
    section's template method(s) (the `UrlCursorPagingController` idiom: `host.addController(this)`
    in the constructor, the field's type written out — see [toolchain.md](toolchain.md)). The host
    keeps what the sections share: the session, the guard, the
    URL/tab wiring, and flags two sections read. A host interface names exactly the members a
    section uses, and a section reaches another section only through a narrow surface (a `Pick` or
    a small interface), never through its whole class. The rendered DOM stays byte-identical — one
    shadow root — which is what keeps the `aria-controls`/`aria-labelledby` IDREFs, the scope's
    stylesheet, and the CSS/deep-link contracts working; a child element adds an interactive
    boundary and is the right shape only when the piece is an independently meaningful, reusable
    component. Controller state is not reactive by itself: every mutation calls
    `host.requestUpdate()` (a small `refresh()` helper), and a directly template-bound handler is an
    arrow class field, because Lit invokes listeners with `this` = the host. The account page's
    four `ui/account-*-section.ts` modules are the reference; the tests for a split element pass
    unchanged, which is the move-only proof.

## Pattern

The feature tree, with the page at the root:

```
features/blog/
  AGENTS.md            # the feature doc
  blog-list-page.ts    # route target: page root, css/test beside
  blog-list-page.css
  blog-list-page.test.ts
  ui/                  # child components (not route targets)
  data-access/         # API modules + DTOs
```

A page composes children and owns the wiring; a child takes properties and reports events:

```ts
@customElement("blog-list-page")
export class BlogListPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles), // baseline.css
    unsafeCSS(controlStyles), // controls.css
    unsafeCSS(styles),
  ];
  // loads through data-access, passes values down, listens for sf-* events up
  render() {
    return html`<pager-nav
      .hasNext=${this.hasNext}
      @sf-pager-next=${this._onNext}
    ></pager-nav>`;
  }
}
```

A control site needs no component at all:

```ts
html`<button
  type="submit"
  class=${buttonClass({ variant: "primary" })}
  ?disabled=${this.pending}
>
  ${el.ui.save}
</button>`;
```

Recipes for the recurring changes:

- **Add a page.** Create `features/<feature>/<page>.ts` with its `.css` and `.test.ts` beside it,
  register the module in `core/index.ts`, and add its route to the table in `core/router.ts`. The
  architecture guard fails a page module under `ui/` and fails a page nothing imports.
- **Add a child component.** Create `features/<feature>/ui/<name>.ts` (+ css/test). Keep it in the
  feature until a second feature needs it; then graduate it to `shared/ui/` and import the shared
  API aliases (`@shared/...`).
- **Add a feature.** Create `features/<feature>/` with its `AGENTS.md` scope doc, the `data-access/`
  module, and the page module; add a row to the root `AGENTS.md` map and the index where the scope
  belongs.
- **Add a route.** Add a `RouteConfig` entry to `routes` in `core/router.ts` (named `path`, a
  `render` returning the page tag) and the page's module import to `core/index.ts`.
- **Add a control variant.** Add the modifier rule to `shared/ui/controls.css`, add the name to
  `BUTTON_VARIANTS` in `shared/ui/controls.ts`, and extend `shared/ui/controls.test.ts` (it pins
  that every variant has a declared modifier).
- **Add a shared component.** Graduate on the second consumer: move the module to `shared/ui/`,
  keep its sheet beside it, take data through properties and report through events, and make sure
  it imports both shared sheets.
- **Add a catalog key.** Add the Greek string to the right group in `shared/catalog/el.ts`; a new
  group needs its own enumeration call in `el.test.ts`. Anglicisms also get `lang="en"` at the
  render site.
- **Add a data-access module.** Create `features/<feature>/data-access/<name>-api.ts` plus the
  DTOs in that feature's `types.ts` (mirroring the Go handler), with a test; the module imports the
  shared transport from `shared/api/`.
- **Split an oversized element.** Move each cohesive section (state, handlers, templates) into a
  `ui/<scope>-section.ts` reactive controller rendered by the host through the section's template
  method(s); keep the host's shared state and the one shadow root (rule 16). The file-size guard
  names the ceiling.
- **Add a card to a surface.** Compose `<content-card>` with the shape the surface needs and fill
  its inputs; the input-by-shape contract is the element's doc comment. A new shape is a change to
  the element and its sheet, reviewed with its own surface — never a page-local variant.
- **Add a guard.** Add a table entry to `web/src/architecture.test.ts` with the file filter, the
  check function, a bad and a clean self-check snippet, and a non-vacuity floor (see
  [testing.md](testing.md)).

## Examples

- `web/src/features/about/about-page.ts` — a page at the feature root with no children and no
  `ui/` folder at all.
- `web/src/features/search/search-page.ts` — the page owns the URL state machine (query, type,
  sort, window) and hands values down; the result rows are template functions.
- `web/src/shared/ui/pager-nav.ts` — a shared child: properties in (`hasPrevious`, `hasNext`,
  `limit`), `sf-pager-*` events out, chrome-free layout.
- `web/src/shared/ui/form-field.ts` — the directive precedent, and `controls.ts` — the typed
  door to the control vocabulary.
- `web/src/shared/ui/content-card.ts` — the card element: four shapes, typed inputs, two action
  slots; `content-card.css` holds each shape's geometry.
- `web/src/features/notifications/ui/push-subscriptions-section.ts` — the native dialog precedent (a guarded `showModal()`), inside the push-settings element's split.

## Gotchas

- **A child's shadow root can own a button but never the form's submit.** See rule 8; the failure
  is invisible until Enter stops working in a two-field form.
- **Slots:** fallback content renders only when nothing is assigned, and whitespace counts as
  assigned; only direct slotted children can be styled with `::slotted()`; read slotted content
  through `assignedElements()` after render.
- **Class fields break reactivity** when properties are declared through the static `properties`
  field: the tsconfig keeps `useDefineForClassFields: false` for this reason (see
  [toolchain.md](toolchain.md)).
- **A property change inside `updated()` re-renders**; if a test counts renders, expect the extra
  cycle.
- **happy-dom is not a browser.** Media queries never evaluate, `showModal()` is a stub that only
  sets the `open` attribute (no modality, no focus trap), and `selected` on an `<option>` needs an
  imperative re-apply — pin those facts against the CSS or source (see [testing.md](testing.md)).
- **Only the ACTIVE tab's panel is rendered.** A tab set that switches content (the account page)
  renders one panel at a time, so an inactive tab's `aria-controls` names an element that is not
  in the DOM at that moment; the active pair always resolves, and the panel-focus move waits for
  `updateComplete` before it queries.
- **A nullable attribute binding arrives as an empty attribute, not as `null`.** Lit writes
  `setAttribute(name, value ?? "")`, so a card input that must stay `null` is written
  `attr=${value ?? nothing}` (the `nothing` sentinel removes the attribute) — the idiom
  `form-field.ts` already uses. The card treats an empty value as its no-image form either way.
- **A slot is a box until it is told not to be.** Slotting a control into a row's flex line
  (the card's `actions`) needs `display: contents` on the slot; `::slotted()` then carries only
  what the shadow sheet must place (the card's `overlay` corner).
- **The per-page request lifecycle is duplicated on purpose for now.** Each page module owns its
  own abort/controller/request-signal/aborted-guard/paint chain, and each site's policy differs
  (scroll, dedupe keys, cursor, error slots), so no mass refactor runs; the accepted next-touch
  shape is a tiny `LatestRequest` helper in `shared/utils/` owning only `begin()`, `isCurrent()`,
  and `abort()` — adopted with the next new page and opportunistically on touch.

## Pointers

- Index: [../../README.md](../../README.md)
- CSS rules, tokens, breakpoints: [css.md](css.md)
- Test rules and the guard shape: [testing.md](testing.md)
- Toolchain and the divergences we record deliberately: [toolchain.md](toolchain.md)
- Scope docs: each `web/src/**/AGENTS.md` (root `AGENTS.md` carries the map)
- Mechanical guards: `web/src/architecture.test.ts`; the CSS baseline check:
  `web/src/core/styles/baseline.test.ts`
