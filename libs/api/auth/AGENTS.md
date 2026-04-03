# libs/api/auth — @api/auth

## Purpose

Authentication/session module for the NestJS API using HttpOnly cookie tokens.

## Current Auth Model

- Access token: short-lived JWT in cookie `sf_access_token`
- Refresh token: rotating JWT in cookie `sf_refresh_token`
- Refresh session state stored per user in Mongo (`refreshTokenHash`, `refreshTokenJti`, `refreshTokenExpiresAt`)
- JWT strategy extracts access token from cookie first, then bearer header fallback

## Endpoints

| Method | Route             | Guard                                       | Notes                                                                                                   |
| ------ | ----------------- | ------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| POST   | /api/auth/login   | CredentialThrottlerGuard + Throttle(5/min)  | Validates credentials, sets access+refresh cookies                                                      |
| POST   | /api/auth/refresh | CredentialThrottlerGuard + Throttle(10/min) | Validates refresh token, rotates refresh session, reissues cookies                                      |
| POST   | /api/auth/logout  | CredentialThrottlerGuard + Throttle(10/min) | Clears cookies and revokes refresh session                                                              |
| GET    | /api/auth/session | JwtAuthGuard                                | Returns authenticated session payload (`sub`, `username`, `email`, `role`, `status`, derived `isAdmin`) |

## Key Files

- `src/lib/api-auth.controller.ts`
- `src/lib/api-auth.service.ts`
- `src/lib/strategies/jwt.strategy.ts`
- `src/lib/auth.constants.ts`
- `src/lib/api-auth.module.ts`

## Security Notes

- Cookie flags: `HttpOnly`, `Secure` in production, `SameSite=Lax`, path `/`
- Refresh token rotation is enforced through `jti` + hashed-token validation
- Revoked or reused refresh token attempts invalidate persisted refresh session
- JWT issuer/audience are enforced for sign and verify (`sick-fansubs-api` / `sick-fansubs-web`)

## Dependencies

- `@api/user` for user lookup and refresh-session persistence
- `@nestjs/throttler` for login/refresh/logout rate limiting

## Validation Commands

```bash
pnpm nx lint api-auth
pnpm nx test api-auth
```
