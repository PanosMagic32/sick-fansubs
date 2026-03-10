# libs/web/about — `@web/about`

## Purpose

The About page for the sick-fansubs website. Displays community links (Facebook, Discord, GitHub, Buy Me a Coffee, tracker) and shows a live API health status indicator.

## Path Alias

`@web/about` → `libs/web/about/src/index.ts`

## Public API (Exports)

| Export        | Description                                           |
| ------------- | ----------------------------------------------------- |
| `aboutRoutes` | Route config — mounted at `/about` in `app.routes.ts` |

## Routes (`src/lib/lib.routes.ts`)

| Path     | Component        | Load  |
| -------- | ---------------- | ----- |
| `/about` | `AboutComponent` | Eager |

## Key Files

### `src/lib/data-access/`

#### `health.service.ts`

- `checkHealth()` — `GET /api/health`
- Returns `Observable<boolean>` — `true` if the API is up, `false` on any error (uses `catchError`)

### `src/lib/feature/`

#### `about.component.ts` — `AboutComponent` (`sf-about`)

- Reads all social link URLs from `WebConfigService` (`FACEBOOK_URL`, `DISCORD_URL`, `GITHUB_URL`, `BUY_ME_A_COFFEE_URL`, `TRACKER_URL`)
- `isHealthy` signal from `toSignal(healthService.checkHealth())`
- Displays links and health status badge with Material card layout
- All UI strings in **Greek**

## Dependencies

- `@web/shared` — `WebConfigService` (for social link URLs)

## Nx Tasks

```bash
pnpm nx lint web-about
pnpm nx test web-about
```

## Notes

- This is the simplest feature lib — one service, one component, no forms or routing complexity
- Health check is triggered once on component init (not polling)
- Social links are injected at runtime via `WebConfigService`, not hardcoded — update `environment.config` to change them
