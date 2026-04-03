# libs/web/dashboard — @web/dashboard

## Purpose

Staff dashboard for user management, metrics, and operational tools. Role-gated feature area for `super-admin`, `admin`, and optionally `moderator` roles.

## MVP Scope (Milestone 2)

- Dashboard shell with tab navigation (implemented)
- Route guard for staff access (implemented)
- Users route skeleton under dashboard tabs (implemented)
- Users list with server-side pagination/sort/filter (implemented)
- Users management actions (role/status mutations) (planned)
- User detail page (planned)
- Role management flows (planned)
- Status management flows (planned)

## Route Structure

```
/dashboard                    -> shell (role-gated)
/dashboard/users              -> users list with server-side table
/dashboard/users/:id          -> user detail (planned)
/dashboard/content            -> (future) content operations
/dashboard/audit              -> (future) audit logs
```

## Current UX Notes

- Dashboard shell uses Material tab-nav links (`/dashboard`, `/dashboard/users`) above a nested router outlet.
- Tab bar is sticky below the global header; routed content includes top offset to avoid overlap.
- On narrow viewports, dashboard container expands full width to align with mobile shell behavior.
- Users view fetches staff-visible users from `GET /api/user/management` with server-side pagination, filtering, and sorting.
- Search requests from the users table are debounced (signals + RxJS interop) before calling the API.

## Role Access

- `super-admin`: Full dashboard access
- `admin`: Full dashboard access (no super-admin user management)
- `moderator`: Dashboard access with limited actions (view users, optional status changes)
- `user`: No dashboard access

## Internal Architecture

- `src/lib/data-access/` — Staff service layer (user CRUD, role/status updates)
- `src/lib/feature/` — Route components (dashboard shell, users list, user detail)
- `src/lib/ui/` — Reusable dashboard components (action cards, user tables, modals)
- `src/lib/utils/` — Authorization helpers and permission checks
- `src/lib/dashboard.routes.ts` — Dashboard feature routes
- `src/index.ts` — Public API exports

## Key Dependencies

- `@web/shared` — TokenService for session state and role checks
- `@shared/types` — UserRole, UserStatus, UserRef types
- `@angular/material` — Dashboard UI components

## Key Files (After Generation)

- `src/lib/data-access/dashboard.service.ts` — Staff operations
- `src/lib/data-access/dashboard.guard.ts` — Staff-only route protection
- `src/lib/feature/dashboard-shell/dashboard-shell.component.ts` — Layout
- `src/lib/feature/users-list/users-list.component.ts` — Users table (planned)
- `src/lib/feature/user-detail/user-detail.component.ts` — User inspector (planned)
- `src/lib/dashboard.routes.ts` — Lazy-loaded route tree

## Authorization Patterns

- Use `dashboardGuard` for route-level gate
- Use `tokenService.canAccessDashboard()` as the single staff-access predicate
- Use `canManageUser(actor, targetUser)` in dashboard.service for safe staff actions
- Hide actions the current role cannot complete; also enforce server-side

## Validation Commands

```bash
pnpm nx lint dashboard
pnpm nx test dashboard
```

## Integration with Web App

Dashboard is lazy-loaded under `/dashboard` route using `loadChildren()` from the web app root routing module.

```typescript
// in web app routing
{
  path: 'dashboard',
  loadChildren: () => import('@web/dashboard').then(m => m.dashboardRoutes)
}

// in dashboard.routes.ts
{
  path: '',
  canActivate: [dashboardGuard],
  component: DashboardShellComponent,
}
```
