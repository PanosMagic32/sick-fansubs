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

| Path                     | Guard              | Component             | Load  |
| ------------------------ | ------------------ | --------------------- | ----- |
| ``(mounted at`/account`) | `requireAuthGuard` | `WebAccountComponent` | Eager |

## Key Files

### `src/lib/data-access/`

#### `user.service.ts`

Angular `httpResource`-based service (signals-driven):

- `getUserProfile(userId: Signal<string>)` — `GET /api/user/:id`; returns `HttpResourceRef<UserProfile>`. Runs reactively when `userId` signal changes.
- `updateUserProfile(userId, data: WritableSignal<UpdateUserRequest | null>)` — `PATCH /api/user/:id`; fires only when `data` signal is non-null (on-demand mutation pattern).
- `getFavoriteBlogPostIds(userId)` — `GET /api/user/:id/favorites`
- `getFavoriteBlogPosts(userId, pageSize, currentPage)` — `GET /api/user/:id/favorites/posts?pagesize=<n>&page=<n>` (paginated populated favorites)
- `addFavoriteBlogPost(userId, postId)` — `PUT /api/user/:id/favorites/:postId`
- `removeFavoriteBlogPost(userId, postId)` — `DELETE /api/user/:id/favorites/:postId`

#### `types.ts`

Shared account feature types:

- `AccountViewState`
- `FavoritesViewState`
- `FavoritesPageChange`

### `src/lib/feature/`

#### `feature/account/account.component.ts` — `WebAccountComponent` (`sf-account`)

Smart component:

- Gets `userId` from `TokenService.userId` signal
- Loads user profile via `getUserProfile(userId)` — pre-fills form via `effect()` when resource resolves
- Reactive form fields: `email?`, `avatar?`, `password?`, `confirmPassword?`
- Password confirmation cross-field validator
- Submit sets `updateRequest` signal → triggers `updateUserProfile` PATCH
- Loads favorite ids + paginated populated favorites and renders a dedicated favorites card
- Maintains local favorites pagination state (current page, page size, page-size options)
- Supports removing favorites and refreshes favorites ids and populated favorites resources
- Handles expired sessions by clearing token and redirecting to login
- Uses shared `StatusCardComponent` from `@web/shared` for loading/error shell states
- Success/error snackbar notifications (Greek messages)

### `src/lib/ui/`

#### `ui/profile-summary/account-profile-summary.component.ts`

Presentational profile summary block (avatar, username, email, role badge).

- Renders Greek role labels (`Υπερδιαχειριστής`, `Διαχειριστής`, `Συντονιστής`, `Χρήστης`)
- Shows a dedicated bottom CTA section for dashboard access when `canAccessDashboard` is true
- Emits `openDashboard` output to parent container

#### `ui/profile-form/account-profile-form.component.ts`

Presentational profile edit/password/security form. Receives form + UI flags via signal inputs and emits UI actions via outputs.

#### `ui/favorites/account-favorites.component.ts`

Presentational favorites card. Uses signal inputs/outputs, a computed `FavoritesViewState` switch for loading/error/empty/ready rendering, and a paginator emitting `FavoritesPageChange`.

## Update Flow

1. User fills form + submits
2. Component sets `updateRequest` signal to the new data
3. `httpResource` detects signal change → sends `PATCH /api/user/:id`
4. On success: shows snackbar, clears `updateRequest` signal (so resource won't re-fire)

## Dependencies

- `@web/auth` — `requireAuthGuard` (for route protection)
- `@web/shared` — `TokenService`, `StatusCardComponent` (loading/error shell)
- `@web/dashboard` — navigation target (`/dashboard`) exposed only for staff roles

## Nx Tasks

```bash
pnpm nx lint account
```

`account` currently exposes only a `lint` target.

## Notes

- Only the logged-in user can edit their own account via this page (enforced by `requireAuthGuard` + the API's `assertCanAccessUser` RBAC helper)
- Password field is optional on update — only submitted if the user fills it in
- Password confirmation is validated client-side only; the API does not have a `confirmPassword` field
