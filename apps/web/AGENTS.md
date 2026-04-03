# apps/web — Angular Frontend Application

## Purpose

The Angular SPA for sick-fansubs — a Greek fansub group's website. Fully standalone (no NgModule), signal-based (zoneless), and built with Angular Material.

## Tech Stack

- **Framework**: Angular 21 (standalone, zoneless via `provideZonelessChangeDetection()`)
- **UI**: Angular Material 21 + CDK
- **State**: Angular Signals (`signal()`, `computed()`, `toSignal()`, `httpResource`)
- **HTTP**: `HttpClient` with functional JWT interceptor
- **Forms**: Angular Reactive Forms with typed `FormGroup<T>`
- **Build**: `@angular/build` (esbuild-based)

## Entry Point

`src/main.ts` → bootstraps `AppComponent` with `appConfig`.

## App Config (`src/app/app.config.ts`)

- `provideZonelessChangeDetection()` — no Zone.js
- `provideHttpClient(withInterceptors([jwtInterceptor]))` — auto-attaches Bearer token to all `/api/*` requests
- `provideRouter(appRoutes, withComponentInputBinding())` — route params bind to `input()` signals
- `provideAppInitializer` — seeds `WebConfigService` from `environment.config` on boot

## Styling System

- Global stylesheet: `apps/web/src/styles.scss`
- Shared design tokens are imported via `@use 'tokens'` from `libs/web/shared/src/lib/styles/_tokens.scss`
- Angular build uses `stylePreprocessorOptions.includePaths` so feature libs can `@use 'breakpoints'` and shared style primitives consistently

## Routing (`src/app/app.routes.ts`)

| Path         | Lazy lib         | Feature         |
| ------------ | ---------------- | --------------- |
| `/` (root)   | `@web/blog-post` | Blog post feed  |
| `/projects`  | `@web/projects`  | Fansub projects |
| `/about`     | `@web/about`     | About page      |
| `/auth`      | `@web/auth`      | Login / Signup  |
| `/search`    | `@web/search`    | Search          |
| `/account`   | `@web/account`   | User account    |
| `/dashboard` | `@web/dashboard` | Staff dashboard |

## Shell Component (`src/app/app.component.ts`)

`AppComponent` renders a 3-column `MatSidenavContainer` shell with `<sf-header>`, `<sf-sidenav>`, and `<router-outlet>`.

On mobile breakpoints, shell side sections are hidden and the main section renders without artificial side gutters so feature pages can control their own spacing.

Session hydration is handled in `app.config.ts` through `provideAppInitializer`, which calls `TokenService.restoreSession()` during app bootstrap.

## Nx Tasks

```bash
pnpm nx serve web        # Start dev server (proxies /api to localhost:3333)
pnpm nx build web        # Production build
pnpm nx lint web
```

`web` currently does not expose a dedicated `test` target.

## Proxy

`proxy.conf.json` proxies `/api/*` to `http://localhost:3333` in dev.

## Related Libs

All feature code lives in `libs/web/` — see `AGENTS.md` in each lib for details:

- [libs/web/shared](../../libs/web/shared/AGENTS.md)
- [libs/web/auth](../../libs/web/auth/AGENTS.md)
- [libs/web/blog-post](../../libs/web/blog-post/AGENTS.md)
- [libs/web/projects](../../libs/web/projects/AGENTS.md)
- [libs/web/search](../../libs/web/search/AGENTS.md)
- [libs/web/about](../../libs/web/about/AGENTS.md)
- [libs/web/account](../../libs/web/account/AGENTS.md)
- [libs/web/dashboard](../../libs/web/dashboard/AGENTS.md)

## Key Conventions

- All UI strings are in **Greek** (target audience is a Greek fansub community)
- Feature libs follow strict **data-access / feature / ui** layering
- Smart (feature) components own state and call services; dumb (ui) components accept only `input()` signals
- `httpResource` is used instead of manual HTTP subscriptions for resource loading
