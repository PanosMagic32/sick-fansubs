# libs/web/shared — `@web/shared`

## Purpose

Cross-cutting shared infrastructure for the Angular frontend. Contains the app shell layout components, JWT interceptor, token management service, runtime config service, and responsive breakpoint helpers. **All other web libs depend on this.**

## Path Alias

`@web/shared` → `libs/web/shared/src/index.ts`

## Public API (Exports)

| Export               | Type             | Description                                     |
| -------------------- | ---------------- | ----------------------------------------------- |
| `HeaderComponent`    | Component        | App toolbar with nav links and social icons     |
| `SidenavComponent`   | Component        | Mobile slide-out nav                            |
| `NoContentComponent` | Component        | Fallback empty-state UI                         |
| `jwtInterceptor`     | HTTP Interceptor | Attaches Bearer token to `/api/*` requests      |
| `TokenService`       | Service          | JWT storage, decoding, and auth signals         |
| `WebConfigService`   | Service          | Runtime config (API URL, social links, version) |

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

- `isHandset$`, `isSmall$`, `isMedium$` — observables for responsive layout
- Consumed internally by `HeaderComponent`

### `src/lib/ui/`

#### `header/header.component.ts` — `HeaderComponent` (`sf-header`)

Material toolbar. Features:

- Nav links: Blog, Projects, Search, About, Auth
- Admin badge when `isAdmin` is true
- Social link buttons (from `WebConfigService`)
- Logout button (calls `TokenService.removeToken()`)
- Emits `sidenavToggle` output for sidenav open/close
- Responsive via `MenuService` breakpoints

#### `sidenav/sidenav.component.ts` — `SidenavComponent` (`sf-sidenav`)

Mobile navigation list:

- Social links and tracker link
- App version display
- Emits `sidenavClose` output

#### `no-content/no-content.component.ts` — `NoContentComponent` (`sf-no-content`)

Reusable empty-state fallback. Inputs:

- `message` — customizable label (default: Greek "No content to display")
- Shows `images/oh-no.png`

## Dependencies

Only Angular core — this lib has no dependency on other local libs.

## Nx Tasks

```bash
pnpm nx lint web-shared
pnpm nx test web-shared
```

## Notes

- `MenuService` is intentionally not exported — use the exported components which consume it internally
- All user-facing strings are in **Greek**
- `TokenService` signals (`isAdmin`, `userId`) are the source of truth for auth state across the entire frontend
