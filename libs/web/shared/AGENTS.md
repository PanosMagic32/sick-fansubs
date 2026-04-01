# libs/web/shared — @web/shared

## Purpose

Cross-feature frontend infrastructure: app shell UI, auth/session interceptors, runtime config, and reusable UI primitives.

## Current Auth/Session Responsibilities

- `jwtInterceptor` sends API requests with `withCredentials: true`
- On API `401` (non-auth endpoints), interceptor calls `/api/auth/refresh` and retries once
- If refresh fails, session state is cleared via `TokenService.removeToken()`
- `TokenService.restoreSession()` bootstraps auth state via `/api/auth/session`
- `TokenService.logout()` calls `/api/auth/logout` then clears local auth signals

## Public Exports (Auth-related)

- `TokenService`
- `jwtInterceptor`
- `mapAuthSessionErrorMessage`
- `WebConfigService`

## UI Components

- `HeaderComponent`
- `SidenavComponent`
- `NoContentComponent`
- `StatusCardComponent`

## Notes

- Session state is signal-based (`isAuthenticated`, `isAdmin`, `userId`)
- Logout button is shown for authenticated users
- `provideAppInitializer` restores session at app startup

## Validation Commands

```bash
pnpm nx lint web-shared
```
