# libs/api/user — @api/user

## Purpose

User domain module for profile management, favorites, and auth-adjacent account persistence.

## Auth-Related Additions

- User schema now includes role-based access fields:
  - `role` (`super-admin` | `admin` | `moderator` | `user`)
  - `status` (`active` | `suspended`)
  - Compatibility fallback for legacy records without `role`
- Schema stores refresh-session metadata:
  - `refreshTokenHash`
  - `refreshTokenJti`
  - `refreshTokenExpiresAt`
- Service exposes refresh-session helpers:
  - `storeRefreshTokenSession()`
  - `isRefreshTokenSessionValid()`
  - `clearRefreshTokenSession()`
- `CredentialThrottlerGuard` is provided/exported for credential endpoints (login/register)

## Controller Security

- `POST /api/user` is protected by credential throttling (`5/min`)
- Protected user routes keep self-or-staff enforcement via role-aware checks
- `GET /api/user/management` is staff-only and supports server-side pagination, search, role/status filters, and sorting for dashboard user management
- `PATCH /api/user/:id` — when a password change is included, the active refresh-token session is invalidated immediately via `clearRefreshTokenSession()` after the update succeeds

## Existing Account Data

- `favoriteBlogPostIds` and `createdBlogPostIds` are maintained as `ObjectId[]`
- Favorites posts endpoint supports pagination (`pagesize`, `page`) and returns `{ posts, count }`

## Validation Commands

```bash
pnpm nx lint api-user
pnpm nx test api-user
```
