# User Roles And Dashboard Plan

## Status Note

This document is a roadmap that includes both implemented Milestone 1 work and pending dashboard phases.
Use the milestone execution and validation sections as the source of truth for current implementation status.

## Why This Needs A Real Plan

The current system is effectively binary authorization:

- authenticated user
- admin via `isAdmin`

That works for content create/edit today, but it will not scale cleanly to a dashboard with multiple staff roles. The first step should be moving from a boolean admin flag to an explicit role model with typed authorization rules.

The referenced WebDevSimplified example is useful because it stages the system from basic permissions to RBAC and then ABAC. That is the right direction here too, but this codebase should stop at a typed RBAC foundation first. Full ABAC or a library like CASL can come later if the product actually needs it.

## Final Direction

These are the concrete decisions for this repo:

1. Refactor from `isAdmin` to `role` plus `status`.
2. Keep the migration backward compatible for persisted users.
3. Accept that some active sessions may need to be reissued during rollout.
4. Build the dashboard as a sub-route inside the existing Angular app.
5. Keep the dashboard isolated as its own feature area so it can be extracted later if the product outgrows the current shape.

## What To Reuse From The WebDevSimplified Repo

The most useful part of the example repo is the progression across branches, not the exact framework code.

### Branches That Matter Most

| Branch                                         | Why It Matters Here                                                                                                                                      |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `1-basic-permissions`                          | Shows the starting point and why scattered permission checks become fragile.                                                                             |
| `2-fix-basic-permission-errors`                | Useful as a reminder that edge cases appear quickly even in simple permission systems.                                                                   |
| `3-add-service-layer`                          | Highly relevant. This repo should centralize permission decisions in helpers/services rather than spread them across controllers, guards, and templates. |
| `3.5-basic-rbac`                               | Good checkpoint for introducing explicit roles and typed permission checks.                                                                              |
| `4-basic-rbac`                                 | The most directly reusable mental model for this refactor.                                                                                               |
| `5-rbac-limits`                                | Useful because it shows where pure role checks stop being enough.                                                                                        |
| `5.5-basic-abac-checkpoint` and `6-basic-abac` | Useful as reference for specific edge rules around actor versus target, but not something this repo should jump to immediately.                          |
| `7-advanced-abac`                              | Useful later if the product gains more scoped moderation or ownership rules.                                                                             |
| `8-casl`                                       | Useful only as a late-stage reference. It should not drive the first implementation here.                                                                |

### Patterns Worth Borrowing

1. A single permission vocabulary such as `dashboard.view`, `user.read.all`, and `user.role.update`.
2. A service or policy layer where authorization decisions live.
3. A staged rollout from simple permissions to RBAC before considering ABAC.
4. Tests focused on policy decisions rather than only endpoint happy paths.

### Things Not Worth Copying Directly

1. Their framework-specific implementation details, because their stack is different.
2. Department scoping, unless this product actually gains that requirement.
3. CASL or similar policy libraries as the starting point.

## Recommendation

Use a hybrid of:

1. Role enum for identity and hierarchy
2. Typed permission map for capability checks
3. A small number of attribute-based rules for user-management edge cases

Do not build the next version around hard-coded `if (isAdmin)` checks. Those checks should become transitional compatibility helpers only.

## Proposed Roles

Internal role ids should stay in English. UI labels should be Greek.

| Role ID       | Purpose                             | Notes                                                                                                                   |
| ------------- | ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `super-admin` | Platform owner role                 | Very limited membership. Can manage all users, including admins and moderators, and handle system-level settings later. |
| `admin`       | Full operational staff role         | Can manage content and all moderator and default users, but not `super-admin` users.                                    |
| `moderator`   | Back-office moderation/support role | Can manage all default users, but not other moderators or admin/super-admin users.                                      |
| `user`        | Default public account              | Created via signup. No dashboard access.                                                                                |

## Role Hierarchy

```text
super-admin > admin > moderator > user
```

This hierarchy should be used for comparison logic, but not as the only authorization mechanism. Some actions should still be explicitly blocked even if the actor outranks the target.

## Recommended V1 Permissions

### Dashboard

| Permission               | super-admin | admin | moderator | user |
| ------------------------ | ----------- | ----- | --------- | ---- |
| `dashboard.view`         | yes         | yes   | yes       | no   |
| `dashboard.metrics.view` | yes         | yes   | optional  | no   |

### Users

| Permission           | super-admin | admin   | moderator | user |
| -------------------- | ----------- | ------- | --------- | ---- |
| `user.read.self`     | yes         | yes     | yes       | yes  |
| `user.update.self`   | yes         | yes     | yes       | yes  |
| `user.read.all`      | yes         | yes     | yes       | no   |
| `user.status.update` | yes         | yes     | optional  | no   |
| `user.role.update`   | yes         | limited | no        | no   |
| `user.delete`        | yes         | limited | no        | no   |

### Content

| Permission             | super-admin | admin | moderator      | user |
| ---------------------- | ----------- | ----- | -------------- | ---- |
| `blog-post.create`     | yes         | yes   | optional later | no   |
| `blog-post.update.any` | yes         | yes   | optional later | no   |
| `blog-post.delete.any` | yes         | yes   | no             | no   |
| `project.create`       | yes         | yes   | optional later | no   |
| `project.update.any`   | yes         | yes   | optional later | no   |
| `project.delete.any`   | yes         | yes   | no             | no   |

## Critical Rule: Moderator Must Have Real Work

Right now the product does not appear to have comments, reports, bans, or approval queues. That means a `moderator` role does not naturally map to real actions yet.

Because of that, there are two acceptable approaches:

1. Ship the data model for `moderator`, but keep moderator-facing features out of the MVP until there is real moderation work.
2. Define moderator V1 as a support role with clear, narrow powers such as viewing users, viewing account details, and updating user status, but not changing roles or deleting users.

The second option is probably the better dashboard story if user management is the next feature.

## Recommended User Status Model

If the dashboard is going to manage users, roles alone are not enough. Add status.

Suggested field:

```ts
type UserStatus = 'active' | 'suspended';
```

Optional later:

```ts
type UserStatus = 'active' | 'suspended' | 'pending-verification' | 'deleted';
```

This is more useful than trying to overload role changes for account control.

## Data Model Changes

## API User Schema

Current schema stores `isAdmin: boolean`. Replace that with:

```ts
type UserRole = 'super-admin' | 'admin' | 'moderator' | 'user';

role: UserRole;
status: 'active' | 'suspended';
```

Migration guidance:

- existing `isAdmin: true` users -> `admin`
- existing `isAdmin: false` users -> `user`
- create one initial `super-admin` explicitly via seed or environment bootstrap

Do not rely on normal signup to create a `super-admin`.

## Backward Compatibility Strategy

This refactor can be backward compatible for existing users, but only if it is handled as a staged migration.

### What Can Stay Backward Compatible

Persisted user accounts can transition safely:

- existing `isAdmin: true` users map to `role: 'admin'`
- existing `isAdmin: false` users map to `role: 'user'`
- new `status` field defaults to `active`

That means account data can be migrated without forcing a destructive reset.

### What Needs Deeper Attention

The risky parts are not the user rows themselves. The risky parts are:

1. active JWT and refresh token payloads that currently carry `isAdmin`
2. frontend session state and guards that currently depend on `isAdmin`
3. API authorization helpers that currently assume a boolean actor model
4. bootstrap logic for the first `super-admin`
5. safety rules around self-demotion and last-super-admin protection

So the short answer is: backward compatibility is possible, but the rollout order matters.

## Migration And Deployment Order

The safest rollout is a compatibility-first deployment, not a one-shot schema flip.

### Step 1: Introduce New Types And Dual-Read Logic

1. Add `role` and `status` to the schema and types.
2. Where needed, derive role from legacy `isAdmin` if `role` is absent.
3. Treat `status` as `active` if absent.

### Step 2: Emit Compatibility Session Payloads

During transition, session and auth responses should return:

```ts
{
  sub,
  username,
  email,
  role,
  status,
  isAdmin: role === 'admin' || role === 'super-admin'
}
```

That keeps older frontend assumptions working while new code moves to role-aware checks.

### Step 3: Backfill Existing Users

Run a migration or one-off backfill that:

1. sets `role` from `isAdmin`
2. sets `status` to `active` when missing
3. explicitly creates or upgrades one trusted account to `super-admin`

### Step 4: Move Frontend And API To Role As Source Of Truth

1. Update `TokenService` to store `role` and `status`.
2. Update guards to use roles instead of `isAdmin`.
3. Update API policy helpers and route guards to use role-aware logic.

### Step 5: Remove Legacy Dependence On `isAdmin`

Only after all consumers use `role` should `isAdmin` stop being emitted or persisted.

## Session Compatibility Notes

Even with a careful migration, some active sessions may need to be reissued.

That is acceptable and normal, but it should be considered part of the rollout plan. The cleanest approach is:

1. keep refresh/session payloads compatible during the transition
2. tolerate re-login if an old token shape reaches unsupported code paths
3. avoid trying to support legacy token semantics forever

Backward compatibility should be strongest for user records, not for outdated session payloads.

## Shared Types

Update shared contracts so all layers work with:

- `role`
- `status`
- derived helpers such as `isStaff` or `canAccessDashboard`

Avoid keeping `isAdmin` as the source of truth. If it must exist during migration, it should be derived from role and removed later.

## Session And JWT Changes

The auth session currently returns:

- `sub`
- `username`
- `email`
- `isAdmin`

It should move to:

- `sub`
- `username`
- `email`
- `role`
- `status`

Temporary compatibility payload during migration:

```ts
{
  sub,
  username,
  email,
  role,
  status,
  isAdmin: role === 'admin' || role === 'super-admin'
}
```

That lets the frontend and protected API routes migrate incrementally.

## Authorization Rules

## Do Not Scatter Logic

The current backend checks are centered on `assertAdmin()` and `assertCanAccessUser()`. That is the right place conceptually, but the logic needs to be generalized.

Introduce a dedicated authorization helper or policy service for user operations.

Suggested actor shape:

```ts
interface AuthActor {
  sub: string;
  role: UserRole;
  status: UserStatus;
}
```

Suggested core helpers:

- `hasPermission(actor, permission)`
- `isRoleAtLeast(actor.role, targetRole)`
- `canManageUser(actor, targetUser)`
- `canChangeUserRole(actor, targetUser, nextRole)`
- `canSuspendUser(actor, targetUser)`

## Required User-Management Safety Rules

These rules should be explicit and tested:

1. A `user` cannot access the dashboard.
2. A `moderator` cannot change roles.
3. An `admin` cannot create or promote a `super-admin`.
4. An `admin` cannot modify or delete a `super-admin`.
5. No one can remove their own access accidentally without an explicit protected flow.
6. The last remaining `super-admin` cannot be demoted or deleted.
7. Suspended users cannot authenticate successfully.

Those seven rules matter more than the broad role matrix because they prevent the worst operational mistakes.

## Backend Implementation Plan

### Phase 1: Role Foundation

1. Add `UserRole` and `UserStatus` types in the API user domain.
2. Replace `isAdmin` in the Mongoose schema with `role` and `status`.
3. Update user serialization and DTOs.
4. Update auth validation, JWT payloads, refresh payloads, and `/api/auth/session`.
5. Introduce role/permission utility functions and use them in user-service checks.
6. Keep a temporary derived `isAdmin` field in API responses only if it reduces migration risk.

### Phase 2: Route Protection

1. Replace `AdminGuard` with a more general role guard or permission guard.
2. Keep content routes simple at first:
   - `admin` and `super-admin` keep current content write access
   - `moderator` content powers stay disabled until intentionally granted
3. Update `JwtStrategy` actor payload typing.

### Phase 3: User Management Endpoints

Add dedicated admin endpoints instead of overloading the self-service account API.

Suggested endpoints:

- `GET /api/user` with filters and pagination
- `GET /api/user/:id`
- `PATCH /api/user/:id/role`
- `PATCH /api/user/:id/status`
- `DELETE /api/user/:id`

Why separate endpoints matter:

- self-service profile editing and staff management have different validation and security needs
- role updates should not piggyback on the public profile update DTO

## Frontend Implementation Plan

## Session State

Update `TokenService` from boolean admin state to role-aware session state:

- `role`
- `status`
- `isStaff`
- `isAdminLike` if needed during migration
- `hasRole(...)`
- `canAccessDashboard()`

Avoid reintroducing raw role comparisons all over templates.

## Route Guards

Current Angular guards are:

- `adminGuard`
- `requireAuthGuard`
- `isLoggedInGuard`

Add or replace with:

- `roleGuard(['super-admin', 'admin', 'moderator'])` for dashboard shell
- `roleGuard(['super-admin', 'admin'])` for privileged user-management actions
- keep `requireAuthGuard` for account pages

## Dashboard Library

Create a dedicated frontend feature library for staff tools.

Recommended name:

- `libs/web/dashboard`

Reason:

The dashboard is a product area, not just a single page. It will likely grow into users, moderation, audit, and operational views.

## Dashboard App Boundary

The dashboard should start as a sub-route in the existing Angular app, not as a separate app or subdomain.

### Recommended V1 Shape

```text
existing web app
  /
  /projects
  /account
  /search
  /dashboard
```

### Why This Is The Right First Step

1. The current repo already serves one Angular SPA and proxies `/api` on the same origin.
2. Cookie-based auth is already designed around that same-origin setup.
3. Route-based feature loading already exists, so `/dashboard` fits the current structure naturally.
4. It avoids the extra operational cost of a second app, second deploy target, and more nginx complexity before the product needs it.

### How To Keep It Future-Proof

Even though the dashboard should live inside the existing app, it should still be treated as a product area with clear boundaries:

1. dedicated `libs/web/dashboard` feature area
2. dedicated route tree under `/dashboard`
3. dedicated data-access services for staff operations
4. no leaking of dashboard-specific logic into public feature libraries unless shared intentionally

That structure keeps later extraction possible if the dashboard eventually becomes large enough for its own Angular app or a staff subdomain.

### When A Separate App Would Become Reasonable

Only split it into a separate app if one or more of these become true:

1. the dashboard needs a distinct release cadence
2. the staff UI becomes large enough to justify separate bundles and ownership
3. security posture or infrastructure needs become meaningfully different
4. the dashboard becomes more of an internal operations product than a section of the public website

## Dashboard Information Architecture

### MVP Routes

```text
/dashboard
/dashboard/users
/dashboard/users/:id
```

Suggested Angular route shape:

```text
/dashboard                    -> dashboard shell + overview
/dashboard/users              -> user list
/dashboard/users/:id          -> user details
```

### Later Routes

```text
/dashboard/content
/dashboard/audit
/dashboard/settings
```

Suggested later route shape:

```text
/dashboard/content            -> staff content operations
/dashboard/audit              -> audit log and change history
/dashboard/settings           -> staff/system settings if needed later
```

## Dashboard MVP Scope

### 1. Dashboard Home

Purpose:

- entry point for staff
- simple counters and shortcuts

Suggested cards:

- total users
- active users
- suspended users
- admins/moderators count
- quick links to user management

### 2. Users List

Purpose:

- search and filter users
- display role and status clearly

Suggested table columns:

- avatar
- username
- email
- role
- status
- created date
- updated date
- actions

Suggested filters:

- search text
- role
- status

### 3. User Detail Page

Purpose:

- inspect a user
- perform safe staff actions

Suggested sections:

- profile summary
- account metadata
- role management card
- status management card
- favorites summary or activity summary later

## Staff Actions By Role

### `super-admin`

- view all users
- change any non-protected role
- suspend or reactivate users
- delete users except protected last-super-admin case
- manage admins and moderators

### `admin`

- view all users
- change `user` and `moderator` roles
- suspend or reactivate `user` and `moderator`
- cannot act on `super-admin`

### `moderator`

- view all users
- optionally suspend/reactivate regular users only if the product wants support tooling in V1
- no role changes
- no destructive actions

## UX Rules

1. Never show actions the current actor cannot complete.
2. Also enforce every rule server-side even if the action is hidden client-side.
3. Use explicit confirmation for destructive or privilege-changing actions.
4. Show the current actor why an action is unavailable where useful.
5. Keep all visible dashboard strings in Greek.

## Recommended Rollout

## Implementation Sequence

This is the recommended execution order for this repo.

### Wave 1: Compatibility Foundation

1. add `role` and `status` to backend types and schema
2. make auth/session payloads dual-shape compatible
3. preserve existing content admin flows during transition

### Wave 2: Policy Refactor

1. replace boolean admin checks with typed role and permission helpers
2. update API guards and service assertions
3. update frontend token state and route guards

### Wave 3: Dashboard MVP

1. add `/dashboard` route area in the existing Angular app
2. add dashboard shell and user-management screens
3. expose dedicated staff endpoints for role and status management

### Wave 4: Hardening

1. add audit logging
2. enforce last-super-admin and self-demotion protections everywhere
3. improve testing and operational visibility

## Milestone 1: Auth Refactor Only

Deliver:

- roles and status in schema
- JWT/session payload updated
- frontend token state updated
- existing admin-only content flows still work

Goal:

- no user-facing dashboard yet
- remove `isAdmin` as the source of truth safely

### Milestone 1 Substeps (What And When)

This milestone should be done in small, reviewable batches. Each substep can map to one PR.

#### M1.1 - Contract And Type Foundation

When: first implementation batch.

Scope:

1. add `UserRole` and `UserStatus` types in backend and shared contracts
2. update API actor/session typings to include `role` and `status`
3. keep compatibility typing for derived `isAdmin`

Output:

- no behavior changes yet
- compiles with dual-shape session types

Done criteria:

1. project builds and lints successfully
2. no route behavior changes yet

#### M1.2 - Persistence Layer Migration

When: immediately after M1.1.

Scope:

1. extend user schema with `role` and `status`
2. add safe defaults (`role: 'user'`, `status: 'active'`)
3. implement dual-read logic that tolerates legacy users missing `role`

Output:

- old and new user records can be read safely

Done criteria:

1. existing users can still log in
2. newly created users get `role` and `status`

#### M1.3 - Auth Service And JWT Transition

When: after schema migration reads are safe.

Scope:

1. update login/refresh/session payloads to include `role` and `status`
2. emit temporary compatibility field `isAdmin`
3. keep refresh-token rotation behavior unchanged

Output:

- session API supports both legacy frontend checks and new role-aware checks

Done criteria:

1. `/api/auth/session` returns `role` and `status`
2. existing auth flow remains functional

#### M1.4 - Backend Authorization Refactor

When: once auth payload includes role/status.

Scope:

1. replace boolean admin assumptions in user-service assertions
2. introduce role-aware helper utilities for authorization decisions
3. keep current protected content behavior equivalent for now

Output:

- backend authorization decisions are role-based internally

Done criteria:

1. admin-only content write routes still behave exactly as before
2. no accidental permission expansion for non-admin users

#### M1.5 - Frontend Session And Guard Refactor

When: after backend session payload is stable.

Scope:

1. update `TokenService` to store `role` and `status`
2. migrate guard logic from `isAdmin` checks to role-aware helpers
3. keep compatibility helper for `isAdmin` during transition only

Output:

- frontend no longer depends on `isAdmin` as primary state

Done criteria:

1. login/logout/session restore still work
2. current admin-only routes remain accessible only to admin-like roles

#### M1.6 - Data Backfill And Bootstrap

When: after dual-read and dual-write logic is deployed.

Scope:

1. backfill legacy users (`isAdmin -> role`, missing `status -> active`)
2. explicitly set one trusted account as `super-admin`
3. verify no user is left without a valid role

Output:

- data is normalized for post-compatibility cleanup

Done criteria:

1. backfill script or migration run is verified
2. role distribution can be inspected and validated

#### M1.7 - Compatibility Cleanup Gate

When: only after successful verification in staging.

Scope:

1. remove internal dependency on persisted `isAdmin`
2. keep or remove response-level `isAdmin` based on readiness of all consumers
3. lock Milestone 1 with tests and release notes

Output:

- `role` and `status` become the only source of truth

Done criteria:

1. all guards and policy checks read role/status only
2. test suite covers auth/session and authorization regressions

### Milestone 1 Sequence Summary

```text
M1.1 Types -> M1.2 Schema -> M1.3 JWT/session -> M1.4 backend authorization -> M1.5 frontend guards -> M1.6 backfill -> M1.7 cleanup
```

### Milestone 1 Execution Status

Current implementation status in this repo:

1. `M1.1` completed in code.
2. `M1.2` completed in code (dual-read defaults and schema fields added).
3. `M1.3` completed in code (session/JWT compatibility payload now includes role and status with derived `isAdmin`).
4. `M1.4` completed in code (backend role-aware helper usage and guard updates with compatibility fallback).
5. `M1.5` completed in code (frontend token service and admin guard now role-aware, `isAdmin` retained as compatibility signal).
6. `M1.6` implementation completed in code, pending execution in environment:

- backfill script added
- explicit super-admin promotion supported by email

7. `M1.7` intentionally pending until after staging verification and successful backfill execution.

### Validation Snapshot (April 2026)

**Milestone 1 Completion Status:**

M1.1–M1.5 implementation fully validated; M1.6 data backfill executed successfully; M1.7 cleanup is ready.

**M1.6 Execution Results:**

- Normalized 2 existing user records (legacy `isAdmin` converted to `role`)
- Promoted `kushoyarou@sick.com` to `super-admin`
- All users now have valid `role` and `status` fields
- Verified: Account page returns role/status/isAdmin compatibility payload ✅

**M1.7 Readiness:**

- All backend and frontend code is role-aware
- Backward compatibility payload still emitted (safety gate complete)
- Ready to remove `isAdmin` persistence after staging sign-off

**Test Infrastructure:**

- Vitest workspace config in place (root `vitest.config.ts`, `vitest.setup.ts`)
- Nx test targets added: `api-auth`, `api-user`, `web-shared`, `auth`
- All tests passing; no module boundary violations

**Next: Milestone 2 (Dashboard MVP) — Starting**

4. The following verification command succeeded:

- `pnpm nx run-many -t test -p api-auth,api-user,web-shared,auth`

Validation conclusion:

- Milestone 1 code refactor is functionally validated at the test/lint level.
- Remaining Milestone 1 completion work is operational rollout work:
  - execute M1.6 backfill in target environment
  - complete M1.7 compatibility cleanup gate after staging verification

### Suggested Checkpoints

1. Checkpoint A (after M1.3): auth/session stable with compatibility payload.
2. Checkpoint B (after M1.5): frontend fully role-aware without UX regressions.
3. Checkpoint C (after M1.7): compatibility mode can be reduced or removed.

## Milestone 2: User Management MVP

Deliver:

- dashboard shell
- users list
- user detail page
- role change flow
- status change flow

Goal:

- staff can manage users safely

Current implementation status (April 2026):

- `libs/web/dashboard` scaffolded with staff guard and tabbed shell navigation
- `/dashboard` route wired in the web app
- dashboard children are now lazy-loaded by feature (`/dashboard` overview and `/dashboard/users`)
- account page now exposes a role-aware CTA for staff users to open dashboard
- dashboard users page now uses a Material table backed by server-side pagination/sort/filter
- dedicated staff endpoint added: `GET /api/user/management` (page, pageSize, search, role, status, sortBy, sortDirection)
- responsive shell behavior was tightened for mobile (no empty side gutters/sections)
- account page now uses explicit responsive inline spacing so cards do not render edge-to-edge
- projects list/cards now avoid mobile overflow by allowing action rows and metadata to wrap safely
- user detail and management actions remain pending in Milestone 2

## Milestone 3: Operational Hardening

Deliver:

- audit trail for role/status changes
- protected rules around self-demotion and last-super-admin safety
- better filtering and pagination
- test coverage for authorization decisions

## Milestone 4: Optional ABAC Expansion

Only do this if real needs appear, such as:

- department-scoped staff access
- ownership-based exceptions
- resource-specific moderation rules

This is where a policy library could become worthwhile. It should not be the starting point.

## Testing Plan

Authorization logic is a regression magnet, so tests should be planned upfront.

### Backend

Add unit tests for:

- role hierarchy helpers
- permission resolution
- `canManageUser` logic
- last-super-admin protection
- suspended-user login rejection

Add integration tests for:

- session payload contents
- dashboard-protected endpoints
- role update and status update endpoints

### Frontend

Add tests for:

- `TokenService` role/session handling
- route guards
- hidden versus visible actions on dashboard pages

## Concrete Recommendation For This Repo

If the next implementation cycle is limited, do this in order:

1. Replace `isAdmin` with `role` plus `status` in API and session payloads.
2. Keep existing content editing limited to `admin` and `super-admin` only.
3. Build a dashboard MVP with only user listing, user detail, role changes, and suspension.
4. Treat `moderator` as read/support staff until there is real moderation work.
5. Add audit logging before broadening moderator or admin powers further.

That sequence gives you a scalable foundation without overengineering the first release.
