# libs/api/user — @api/user

## Purpose

User domain module for profile management, favorites, and auth-adjacent account persistence.

## Auth-Related Additions

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
- Protected user routes keep self-or-admin enforcement

## Existing Account Data

- `favoriteBlogPostIds` and `createdBlogPostIds` are maintained as `ObjectId[]`
- Favorites posts endpoint supports pagination (`pagesize`, `page`) and returns `{ posts, count }`

## Validation Commands

```bash
pnpm nx lint api-user
```
