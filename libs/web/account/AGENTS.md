# libs/web/account — `@web/account`

## Purpose

Authenticated user account management page. Allows the logged-in user to view/update profile data and manage favorites for both blog posts and projects.

## Path Alias

`@web/account` → `libs/web/account/src/index.ts`

## Public API (Exports)

| Export          | Description                                             |
| --------------- | ------------------------------------------------------- |
| `accountRoutes` | Route config — mounted at `/account` in `app.routes.ts` |

## Routes (`src/lib/lib.routes.ts`)

| Path                     | Guards                                    | Component             | Load  |
| ------------------------ | ----------------------------------------- | --------------------- | ----- |
| ``(mounted at`/account`) | `requireAuthGuard`, `unsavedChangesGuard` | `WebAccountComponent` | Eager |

## Key Files

### `src/lib/data-access/`

#### `user.service.ts`

Angular `httpResource`-based service (signals-driven):

- `getUserProfile(userId: Signal<string>)` — `GET /api/user/:id`; returns `HttpResourceRef<UserProfile>`.
- `updateUserProfile(userId, data: WritableSignal<UpdateUserRequest | null>)` — `PATCH /api/user/:id`.
- `getFavoriteBlogPostIds(userId)` — `GET /api/user/:id/favorites`.
- `getFavoriteBlogPosts(userId, pageSize, currentPage, sortOrder)` — `GET /api/user/:id/favorites/posts?pagesize=<n>&page=<n>&sort=<newest|oldest>`.
- `removeFavoriteBlogPost(userId, postId)` — `DELETE /api/user/:id/favorites/:postId`.
- `getFavoriteProjectIds(userId)` — `GET /api/user/:id/favorites/projects`.
- `getFavoriteProjects(userId, pageSize, currentPage, sortOrder)` — `GET /api/user/:id/favorites/projects/items?pagesize=<n>&page=<n>&sort=<newest|oldest>`.
- `removeFavoriteProject(userId, projectId)` — `DELETE /api/user/:id/favorites/projects/:projectId`.

#### `types.ts`

Shared account feature types include profile and favorites view/pagination state for both posts and projects.

### `src/lib/feature/`

#### `feature/account/account.component.ts` — `WebAccountComponent` (`sf-account`)

Smart component:

- Gets `userId` from `TokenService.userId` signal.
- Loads user profile and pre-fills form fields via reactive effects.
- Applies password confirmation validation.
- Submits profile updates via signal-driven mutation trigger.
- Maintains independent favorites state for posts and projects.
- Uses API-side sort (`newest` / `oldest`) for favorites before pagination.
- Renders favorites in tabs (Posts/Projects).
- Handles favorite removals and refreshes ids + paginated resources.
- Handles expired sessions by clearing token and redirecting to login.
- Uses Greek snackbar notifications.

### `src/lib/ui/`

- `ui/profile-summary/account-profile-summary.component.ts` — profile overview + dashboard CTA for staff.
- `ui/profile-form/account-profile-form.component.ts` — presentational edit form.
- `ui/favorites/account-favorites.component.ts` — favorite blog posts list/card.
- `ui/favorite-projects/account-favorite-projects.component.ts` — favorite projects list/card.

## Dependencies

- `@web/auth` — `requireAuthGuard`.
- `@web/shared` — `TokenService`, `StatusCardComponent`, shared dialogs.
- `@web/dashboard` — navigation target (`/dashboard`) for staff roles.

## Nx Tasks

```bash
pnpm nx lint account
```

## Notes

- Only the logged-in user can access/update their own account data.
- Password field remains optional on profile update.
- Favorites sorting is API-driven to keep paginated ordering correct.
- Account layout remains responsive with mobile-safe spacing and tabbed favorites sections.
