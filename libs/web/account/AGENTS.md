# libs/web/account — `@web/account`

## Purpose

Authenticated user account management page. Allows the logged-in user to view/update profile data and manage favorite blog posts.

## Path Alias

`@web/account` → `libs/web/account/src/index.ts`

## Public API (Exports)

| Export          | Description                                             |
| --------------- | ------------------------------------------------------- |
| `accountRoutes` | Route config — mounted at `/account` in `app.routes.ts` |

## Routes (`src/lib/lib.routes.ts`)

| Path       | Guard              | Component             | Load  |
| ---------- | ------------------ | --------------------- | ----- |
| `/account` | `requireAuthGuard` | `WebAccountComponent` | Eager |

## Key Files

### `src/lib/data-access/`

#### `user.service.ts`

Angular `httpResource`-based service (signals-driven):

- `getUserProfile(userId: Signal<string>)` — `GET /api/user/:id`; returns `HttpResourceRef<User>`. Runs reactively when `userId` signal changes.
- `updateUserProfile(userId, data: WritableSignal<UpdateUserData | null>)` — `PATCH /api/user/:id`; fires only when `data` signal is non-null (on-demand mutation pattern).
- `getFavoriteBlogPostIds(userId)` — `GET /api/user/:id/favorites`
- `getFavoriteBlogPosts(userId)` — `GET /api/user/:id/favorites/posts` (single populated favorites call)
- `removeFavoriteBlogPost(userId, postId)` — `DELETE /api/user/:id/favorites/:postId`

### `src/lib/feature/`

#### `account.component.ts` — `WebAccountComponent` (`sf-account`)

Smart component:

- Gets `userId` from `TokenService.userId` signal
- Loads user profile via `getUserProfile(userId)` — pre-fills form via `effect()` when resource resolves
- Reactive form fields: `email?`, `avatar?`, `password?`, `confirmPassword?`
- Password confirmation cross-field validator
- Submit sets `updateRequest` signal → triggers `updateUserProfile` PATCH
- Loads favorite ids + populated favorites and renders a dedicated favorites card
- Supports removing favorites and refreshes both ids and populated favorites resources
- Shows `MatProgressSpinner` while loading
- Success/error snackbar notifications (Greek messages)

## Update Flow

1. User fills form + submits
2. Component sets `updateRequest` signal to the new data
3. `httpResource` detects signal change → sends `PATCH /api/user/:id`
4. On success: shows snackbar, clears `updateRequest` signal (so resource won't re-fire)

## Dependencies

- `@web/auth` — `requireAuthGuard` (for route protection)
- `@web/shared` — `TokenService` (for `userId` signal)

## Nx Tasks

```bash
pnpm nx lint account
pnpm nx test account
```

## Notes

- Only the logged-in user can edit their own account via this page (enforced by `requireAuthGuard` + the API's `assertCanAccessUser` RBAC helper)
- Password field is optional on update — only submitted if the user fills it in
- Password confirmation is validated client-side only; the API does not have a `confirmPassword` field
