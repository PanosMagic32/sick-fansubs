# UI Styling — Audit & Improvement Plan

> Audited: March 11, 2026

## Overview

The Angular frontend uses Angular Material 3 as its component library with a handful of global CSS custom properties, but most styling is done ad-hoc at the component level with no shared design system. The result is a set of hard-to-maintain, inconsistent styles scattered across 19+ SCSS files.

---

## What's Good

- **Material 3 theme** (`apps/web/src/styles.scss`) — properly set up with `@include mat.theme()`, system classes included, strong focus indicators enabled.
- **Account component** (`libs/web/account/src/lib/feature/account.component.scss`) — the only component that defines its own scoped design tokens (`--sf-space-*`, `--sf-danger`, `--sf-warning`). This pattern should be extracted to global scope.
- **Projects grid** (`libs/web/projects/src/lib/feature/project-list/project-list.component.scss`) — uses `repeat(auto-fill, minmax(340px, 1fr))`, no hardcoded breakpoints needed.

---

## Issues

### 1. No Global Design Token System

Only 3 CSS custom properties exist globally (`--primary-color`, `--background-primary-color-active-light`, `--mat-sys-outline-variant`). Everything else is hardcoded.

**Examples of hardcoded values that should be tokens:**

| Location                        | Value                          | Should Be                                  |
| ------------------------------- | ------------------------------ | ------------------------------------------ |
| `blog-post-item.component.scss` | `#56ff26` (chip color)         | `--sf-color-release`                       |
| `blog-post-item.component.scss` | `#3f51b5` (border)             | `--sf-color-primary` / Material token      |
| `blog-post-item.component.scss` | `#ff6b81` (favorite)           | `--sf-color-accent`                        |
| `about.component.scss`          | `#4caf50` / `#f44336` (health) | `--sf-color-success` / `--sf-color-danger` |
| `header.component.scss`         | `#fff` (links)                 | `--sf-color-on-primary`                    |
| `account.component.scss`        | local `--sf-*` tokens          | → move to global                           |

---

### 2. Breakpoint Fragmentation

Eight different breakpoint values are used across the app with no consistency.

| Component                       | Breakpoint(s)               |
| ------------------------------- | --------------------------- |
| `app.component.scss`            | `68.75em` (1100px)          |
| `blog-post-list.component.scss` | `60em`, `100em`, `143.75em` |
| `project-item.component.scss`   | `68.75em`, `100em`          |
| `search.component.scss`         | `106.25em` (1700px)         |
| `account.component.scss`        | `768px`, `1024px`           |
| `menu.service.ts` (JS)          | `1050px`, `1530px`          |

There is no shared breakpoint file, no naming convention, and no mobile-first strategy.

---

### 3. Spacing Inconsistencies

No standardized spacing scale. Values in use: `0.5rem`, `1rem`, `1.2rem`, `1.5rem`, `2rem`, `3rem`, `4rem`, `5rem`, `10%`, `0.625rem`. Forms use `margin: 5rem 1rem` vs `margin: 2rem 0rem` in similar contexts.

---

### 4. Layout Anti-Patterns

- **Hardcoded header height assumption**: `calc(100vh - 75px)` in `app.component.scss` — breaks if the header changes height.
- **Negative margins**: sidenav uses `margin-bottom: -0.5rem` for icon alignment; should use `gap` + `align-items`.
- **Margin-based hover shift**: sidenav applies `margin-right: 1.5rem` on hover which moves content; should use `padding-right` instead.
- **Magic numbers**: `margin-left: 9rem` for tracker icon, `margin-bottom: -3px` for menu items — undocumented and fragile.
- **`::ng-deep` usage**: `about.component.scss` — breaks style encapsulation.

---

### 5. Responsive Gaps

- Auth forms use `width: 60%` — no mobile handling.
- Search card uses `max-width: 50vw` — overflows on small screens.
- Several components have no mobile styling at all.
- No mobile-first approach — all queries are `min-width` overrides of a desktop-only base.

---

### 6. Code Duplication

- `.paginator-container` — same rule written twice inside `blog-post-list.component.scss`.
- `line-clamp` pattern duplicated in 3+ component SCSS files.
- Card image sizing (`width: 85%`, `max-height: 40vh`, `object-fit: cover`) repeated in multiple card components.
- No shared SCSS mixins or placeholders.

---

### 7. Naming Inconsistencies

Nav-related class names across the app: `.nav-caption`, `.nav-list`, `.navigation-items`, `.navigation-items-icons-only`, `.menu-icons-only` — no consistent BEM or naming convention.

---

## Proposed Work Stages

### Stage 1 — Global Token System

Create `libs/web/shared/src/lib/styles/tokens.scss` and import it into `apps/web/src/styles.scss`.

Tokens to define:

```scss
// Spacing (8px base grid)
--sf-space-1: 0.25rem; // 4px
--sf-space-2: 0.5rem; // 8px
--sf-space-3: 0.75rem; // 12px
--sf-space-4: 1rem; // 16px
--sf-space-6: 1.5rem; // 24px
--sf-space-8: 2rem; // 32px
--sf-space-12: 3rem; // 48px
--sf-space-16: 4rem; // 64px
--sf-space-20: 5rem; // 80px

// Semantic colors
--sf-color-success: #4caf50;
--sf-color-danger: #f44336;
--sf-color-warning: #ff9800;
--sf-color-release: #56ff26;
--sf-color-on-primary: #fff;

// Border radius
--sf-radius-sm: 0.25rem;
--sf-radius-md: 0.5rem;
--sf-radius-lg: 1rem;
--sf-radius-pill: 9999px;

// Header
--sf-header-height: 64px;
```

Remove the local token declarations from `account.component.scss` once global equivalents exist.

---

### Stage 2 — Shared Breakpoint Mixins

Create `libs/web/shared/src/lib/styles/breakpoints.scss`:

```scss
$sf-breakpoints: (
  'sm': 576px,
  'md': 768px,
  'lg': 1050px,
  // matches MenuService isHandset threshold
  'xl': 1280px,
  '2xl': 1530px,
  // matches MenuService isMedium threshold
  '3xl': 1920px,
);

@mixin bp-up($key) {
  @media (min-width: map.get($sf-breakpoints, $key)) {
    @content;
  }
}

@mixin bp-down($key) {
  @media (max-width: #{map.get($sf-breakpoints, $key) - 1}) {
    @content;
  }
}
```

Replace all ad-hoc breakpoint values in component SCSS files with these mixins.

---

### Stage 3 — Shared SCSS Mixins

Create `libs/web/shared/src/lib/styles/mixins.scss`:

- `@mixin line-clamp($lines)` — replace 3+ duplicated instances
- `@mixin card-image` — replace repeated `width: 85%; max-height: 40vh; object-fit: cover`
- `@mixin page-container` — standard page wrapper with max-width and horizontal padding

---

### Stage 4 — Fix Anti-Patterns

- Replace `calc(100vh - 75px)` with `calc(100vh - var(--sf-header-height))`.
- Remove negative margins in sidenav; use `align-items: center` + `gap`.
- Replace margin-based hover shifts with `padding` transitions.
- Remove `::ng-deep` from `about.component.scss`; use a proper Material theming API instead.
- Add comments to any remaining magic numbers.

---

### Stage 5 — Responsive Audit

- Switch to mobile-first (base styles = mobile, `bp-up` for larger screens).
- Fix `width: 60%` on auth forms — use `max-width` with responsive fallback.
- Fix `max-width: 50vw` on search card.
- Verify all feature pages on 375px, 768px, 1280px, 1920px.

---

### Stage 6 — Dark Mode (Optional / Later)

Material 3 system already supports it. Extend `:root` with dark-mode variants gated on `@media (prefers-color-scheme: dark)` or a `.dark` class toggle.

---

## Suggested File Structure

```
libs/web/shared/src/lib/styles/
├── _tokens.scss        ← All CSS custom properties
├── _breakpoints.scss   ← Breakpoint map + bp-up / bp-down mixins
├── _mixins.scss        ← line-clamp, card-image, page-container, etc.
└── _index.scss         ← @forward all of the above
```

Import `@use '@web/shared/styles' as sf;` from component SCSS files (requires a Sass path alias — see tsconfig / webpack config).

---

## Files to Touch (by Stage)

| Stage             | Files                                                                                                                                                      |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 (tokens)        | `styles.scss`, all component SCSS files with hardcoded colors                                                                                              |
| 2 (breakpoints)   | `menu.service.ts`, `app.component.scss`, `blog-post-list.component.scss`, `project-item.component.scss`, `search.component.scss`, `account.component.scss` |
| 3 (mixins)        | `blog-post-item.component.scss`, `project-item.component.scss`, all card components                                                                        |
| 4 (anti-patterns) | `app.component.scss`, `sidenav.component.scss`, `about.component.scss`                                                                                     |
| 5 (responsive)    | `auth` forms, `search.component.scss`, all feature pages                                                                                                   |
