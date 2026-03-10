# libs/web/auth — `@web/auth`

## Purpose

Authentication UI for the Angular frontend. Provides login and signup pages, the `AuthService` for API calls, and three route guards used across the app.

## Path Alias

`@web/auth` → `libs/web/auth/src/index.ts`

## Public API (Exports)

| Export             | Type         | Description                                                               |
| ------------------ | ------------ | ------------------------------------------------------------------------- |
| `authRoutes`       | Route config | Routes for `/auth/login` and `/auth/signup`                               |
| `authGuard`        | Route Guard  | Admin-only. Requires valid token + `isAdmin`. Redirects to `/auth/login`. |
| `isLoggedInGuard`  | Route Guard  | Blocks access when already logged in. Redirects to `/`.                   |
| `requireAuthGuard` | Route Guard  | Requires any valid token. Redirects to `/auth/login`.                     |

## Routes (`src/lib/lib.routes.ts`)

| Path          | Guard             | Component                 |
| ------------- | ----------------- | ------------------------- |
| `auth/`       | —                 | Redirects to `auth/login` |
| `auth/login`  | `isLoggedInGuard` | `LoginComponent` (lazy)   |
| `auth/signup` | `isLoggedInGuard` | `SignupComponent` (lazy)  |

## Key Files

### `src/lib/data-access/`

#### `auth.service.ts`

- `login(username, password)` — `POST /api/auth/login` → stores token via `TokenService.setToken()` → navigates to `/`
- `signUp(username, email, password, confirmPassword)` — `POST /api/user` → saves user to `localStorage` → navigates to `/auth/login`
- `isLoading` — readonly signal, `true` while requests are in-flight

#### Guards

| File                    | Guard              | Logic                                                                   |
| ----------------------- | ------------------ | ----------------------------------------------------------------------- |
| `auth.guard.ts`         | `authGuard`        | `isValidToken() && isAdmin()` → proceed; else redirect to `/auth/login` |
| `logged-in.guard.ts`    | `isLoggedInGuard`  | Already logged in → redirect to `/`; else proceed                       |
| `require-auth.guard.ts` | `requireAuthGuard` | `isValidToken()` → proceed; else redirect to `/auth/login`              |

#### Interfaces

- `login-form.interface.ts` — `LoginFormModel`: typed form `{ username: FormControl<string>, password: FormControl<string> }`
- `signup-form.interface.ts` — `SignupFormModel`: `{ username, email, password, confirmPassword, avatar? }`

### `src/lib/feature/`

Thin wrappers that render the smart UI components:

- `login/login.component.ts` — `LoginComponent` (default export, lazy) → renders `<sf-login-form />`
- `signup/signup.component.ts` — `SignupComponent` (default export, lazy) → renders `<sf-signup-form />`

### `src/lib/ui/`

| Component                  | Selector         | Description                                                                                                                              |
| -------------------------- | ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `login-form.component.ts`  | `sf-login-form`  | Reactive form: `username` + `password`. Calls `AuthService.login()`. Shows `MatProgressBar` while loading.                               |
| `signup-form.component.ts` | `sf-signup-form` | Reactive form: `username`, `email`, `password`, `confirmPassword`. Cross-field `checkPasswords` validator. Calls `AuthService.signUp()`. |

## Guard Usage Map

| Guard              | Used by                                                                     |
| ------------------ | --------------------------------------------------------------------------- |
| `authGuard`        | `@web/blog-post` (create/edit routes), `@web/projects` (create/edit routes) |
| `isLoggedInGuard`  | This lib's own login/signup routes                                          |
| `requireAuthGuard` | `@web/account` (account route)                                              |

## Dependencies

- `@web/shared` — `TokenService` (for auth signal checks)

## Nx Tasks

```bash
pnpm nx lint web-auth
pnpm nx test web-auth
```
