# libs/web/dashboard — @web/dashboard

## Purpose

Staff dashboard for user management, metrics, and operational tools. Role-gated feature area for `super-admin`, `admin`, and `moderator`.

## Current Scope Status

- Dashboard shell with tab navigation (implemented)
- Staff route guard (implemented)
- Users route under dashboard tabs (implemented)
- Users list with server-side pagination/sort/filter (implemented)
- Row-level user actions (status, role, delete) (implemented)
- Bulk user status actions with selection (implemented)
- User detail page (planned)
- Content and audit subareas (planned)

## Route Structure

```
/dashboard                    -> shell (role-gated)
/dashboard/users              -> users list with server-side table
/dashboard/users/:id          -> user detail (planned)
/dashboard/content            -> (future) content operations
/dashboard/audit              -> (future) audit logs
```

## Current UX Notes

- Dashboard shell uses Material tab-nav links above nested router outlet.
- Tab bar remains sticky under the global header.
- Users view reads from `GET /api/user/management` with server-side search/filter/sort/pagination.
- Search input is debounced.
- Table supports:
  - row selection + select-all per page
  - row action menu (status/role/delete)
  - bulk status updates for selected users
- Row/bulk actions use in-flight disable states to prevent duplicate submissions.
- Avatars use API values with fallback to `/logo/logo.png`.

## Role Access Rules

- `super-admin`: Full dashboard access and user management.
- `admin`: Full dashboard access, cannot manage `super-admin` users.
- `moderator`: Dashboard access with restricted management actions.
- `user`: No dashboard access.

## Internal Architecture

- `src/lib/data-access/` — staff service layer (`getManagementUsers`, `updateUserRole`, `updateUserStatus`, `deleteUser`).
- `src/lib/feature/` — route components (shell, home, users).
- `src/lib/dashboard.routes.ts` — lazy-loaded dashboard route tree.
- `src/index.ts` — public API exports.

## Validation Commands

```bash
pnpm nx lint dashboard
pnpm nx test dashboard
pnpm nx build web --configuration=development
```

## Integration with Web App

Dashboard is lazy-loaded under `/dashboard` via `loadChildren()` from web app routing.
