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

- `TokenService` signals: `isAuthenticated`, `role`, `status`, `userId`, `isInitialized`
- `TokenService` computed helpers: `isStaff`, `canManageContent`
- `TokenService` methods: `hasRole()`, `hasAnyRole()`, `canAccessDashboard()`, `isValidToken()`, `getUserId()`, `restoreSession()`, `logout()`, `removeToken()`
- `jwtInterceptor`
- `mapAuthSessionErrorMessage`
- `WebConfigService`

## UI Components

- `HeaderComponent`
- `SidenavComponent`
- `NoContentComponent`
- `StatusCardComponent`

## Notes

- Session state is signal-based (`isAuthenticated`, `role`, `status`, `userId`)
- Content-management access is role-based (`admin` or `super-admin`) via `canManageContent`
- `MenuService` exposes responsive breakpoint state as signals (`isHandset`, `isSmall`, `isMedium`) used by `HeaderComponent`
- Logout button is shown for authenticated users
- `provideAppInitializer` restores session at app startup

## Validation Commands

```bash
pnpm nx lint web-shared
pnpm nx test web-shared
```
