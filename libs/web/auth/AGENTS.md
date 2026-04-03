# libs/web/auth — @web/auth

## Purpose

Authentication feature UI and route guards for the Angular app.

## Current Flow

- Login: `POST /api/auth/login` (cookies set by API), then `TokenService.restoreSession()`
- Signup: `POST /api/user`, then redirect to login
- Guards rely on cookie-backed session signals from `TokenService`

## Guards

- `adminGuard`: admin-only access (`isAuthenticated && hasAnyRole(['super-admin', 'admin'])`)
- `requireAuthGuard`: authenticated-user access
- `isLoggedInGuard`: redirects authenticated users away from login/signup

## Error Handling

- Login/signup map auth errors using centralized session mapper (`401/403/429/5xx/network`)
- Rate-limit feedback is user-facing in Greek

## Key Files

- `src/lib/data-access/auth.service.ts`
- `src/lib/data-access/auth.guard.ts`
- `src/lib/data-access/require-auth.guard.ts`
- `src/lib/data-access/logged-in.guard.ts`
- `src/lib/ui/login-form/login-form.component.ts`
- `src/lib/ui/signup-form/signup-form.component.ts`

## Validation Commands

```bash
pnpm nx lint auth
pnpm nx test auth
```
