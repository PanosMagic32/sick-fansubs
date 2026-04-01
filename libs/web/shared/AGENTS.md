# libs/web/shared — `@web/shared`

## Purpose

Cross-cutting shared infrastructure for the Angular frontend. Contains the app shell layout components, JWT interceptor, token management service, runtime config service, responsive breakpoint helpers, and shared design-system styles. **All other web libs depend on this.**

## Path Alias

`@web/shared` → `libs/web/shared/src/index.ts`

## Public API (Exports)

| Export                | Type             | Description                                       |
| --------------------- | ---------------- | ------------------------------------------------- |
| `HeaderComponent`     | Component        | App toolbar with nav links and social icons       |
| `SidenavComponent`    | Component        | Mobile slide-out nav                              |
| `NoContentComponent`  | Component        | Fallback empty-state UI                           |
| `StatusCardComponent` | Component        | Reusable loading/error card with optional actions |
| `jwtInterceptor`      | HTTP Interceptor | Attaches Bearer token to `/api/*` requests        |
| `TokenService`        | Service          | JWT storage, decoding, and auth signals           |
| `WebConfigService`    | Service          | Runtime config (API URL, social links, version)   |

## Key Files

### `src/lib/data-access/`

#### `token.service.ts` — `TokenService` (`providedIn: 'root'`)

- Stores JWT in `localStorage`
- Readonly signals: `isAdmin`, `userId`
- `getUserIDFromToken()` — decodes JWT payload, populates signals (called on app init from `AppComponent`)
- `isValidToken()` — checks token expiry
- `setToken(token)` — saves token and updates signals
- `removeToken()` — clears token and resets signals

#### `jwt.interceptor.ts` — `jwtInterceptor` (functional interceptor)

- Attaches `Authorization: Bearer <token>` header to:
  - relative URLs starting with `/api`
  - absolute URLs matching `http(s)://.../api/...`
- Token retrieved from `TokenService`
- Registered globally in `app.config.ts`

#### `web-config.service.ts` — `WebConfigService` (`providedIn: 'root'`)

Holds runtime configuration. Fields:

- `API_URL`, `APP_VERSION`
- `FACEBOOK_URL`, `DISCORD_URL`, `GITHUB_URL`, `BUY_ME_A_COFFEE_URL`, `TRACKER_URL`

Set once on app init via `provideAppInitializer` in `app.config.ts`.

#### `menu.service.ts` — `MenuService` (`providedIn: 'root'`)

CDK `BreakpointObserver` wrappers (not exported publicly):

- `isHandset$` — `true` at ≤1050px (hamburger shown, nav hidden)
- `isSmall$` — alias of `isHandset$`
- `isMedium$` — `true` at 1051px–1530px (icons-only nav)
- At >1530px all three are `false` → full text nav + social icons
- Consumed internally by `HeaderComponent`

### `src/lib/styles/`

Shared styling primitives imported by the app and feature libraries:

- `_tokens.scss` — global `:root` CSS custom properties (`--sf-*` spacing, colors, typography, radii, icon sizes, layout tokens)
- `_breakpoints.scss` — semantic responsive breakpoints (used by feature SCSS via `@use 'breakpoints' as bp`)
- `_mixins.scss` — reusable style mixins for cross-feature consistency

These files are provided to feature SCSS via Angular build `stylePreprocessorOptions.includePaths`.

### `src/lib/ui/`

#### `header/header.component.ts` — `HeaderComponent` (`sf-header`)

Material toolbar. Features:

- Nav links: Blog, Projects, Search, About, Auth
- Admin badge when `isAdmin` is true
- Social link buttons (from `WebConfigService`)
- Logout button (calls `TokenService.removeToken()`)
- Emits `sidenavToggle` output for sidenav open/close
- Responsive via `MenuService` breakpoints: >1530px full text nav, 1051–1530px icons-only, ≤1050px hamburger

#### `sidenav/sidenav.component.ts` — `SidenavComponent` (`sf-sidenav`)

Mobile navigation list:

- Social links and tracker link
- App version display
- Emits `sidenavClose` output

#### `no-content/no-content.component.ts` — `NoContentComponent` (`sf-no-content`)

Reusable empty-state fallback. Inputs:

- `message` — customizable label (default: Greek "No content to display")
- Shows `images/oh-no.png`

#### `status-card/status-card.component.ts` — `StatusCardComponent` (`sf-status-card`)

Reusable shell card for loading/error states.

- Inputs:
  - `state`: `'loading' | 'error'`
  - `message`: main state message
  - Optional action configuration (`showRetry`, `retryLabel`, `showSecondaryAction`, `secondaryActionLabel`, etc.)
- Outputs:
  - `retry`
  - `secondaryAction`

## Dependencies

Only Angular core — this lib has no dependency on other local libs.

## Nx Tasks

```bash
pnpm nx lint web-shared
```

`web-shared` currently exposes only a `lint` target.

## Notes

- `MenuService` is intentionally not exported — use the exported components which consume it internally
- All user-facing strings are in **Greek**
- `TokenService` signals (`isAdmin`, `userId`) are the source of truth for auth state across the entire frontend
