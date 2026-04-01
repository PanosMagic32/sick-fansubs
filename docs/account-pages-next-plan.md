# Account Pages Plan (Next Iteration)

## Scope

Deliver the next set of high-impact improvements for `/account`, focused on security-critical auth/session hardening, production-ready media upload, and the remaining account UX features.

## Prioritized Backlog (Criticality Order)

### C0 — Security and Session Integrity (Highest)

Status: Completed.

1. Completed: login/register rate limiting with `@nestjs/throttler`.
2. Completed: auth token transport moved from `localStorage` to secure `HttpOnly` cookies.
3. Completed: refresh-token rotation with revocation support.
4. Completed: centralized frontend auth error mapping for expired/invalid/revoked sessions.
5. Partially completed: auth-focused tests added in `api-auth.service.spec.ts`; endpoint-level rate-limit/e2e cases remain for a dedicated follow-up.
6. Completed: refresh-token session invalidated on password change (`update()` in `api-user` now calls `clearRefreshTokenSession()` when a new password is set).

### C1 — Media Pipeline for Account and Future Features

1. Add MinIO service and persistent volume in Docker Compose (dev + prod).
2. Proxy `/media/` to MinIO in `nginx`.
3. Add `MINIO_*` environment variables.
4. Create `libs/api/media` with `POST /api/media/images` (multipart, validation, upload to MinIO, URL response).
5. Register `ApiMediaModule` in `AppModule`.
6. Install required packages:
   - `@aws-sdk/client-s3`
   - `@aws-sdk/lib-storage`
   - `multer`
   - `@types/multer`
7. Update account avatar flow to upload first, then patch `avatar` URL.

### C2 — Account UX and Safety Enhancements

1. Add unsaved-changes route protection for account edits.
2. Add account deletion flow with explicit confirmation and forced logout.
3. Add optional favorites sort controls (newest/oldest).

### C3 — Optional Security Extension

1. Add optional email verification during registration (SMTP-backed).

## Backend Coordination Constraints

- Keep user-profile/favorites access rules as self or admin.
- Preserve strict DTO validation behavior (`ValidationPipe` with `transform`, `whitelist`, `forbidNonWhitelisted`).
- Keep update endpoint error semantics consistent for frontend mapping (`400/401/403/404/409`).

## Security Baseline Targets

- Cookie flags: `HttpOnly`, `Secure` (prod), `SameSite=Lax`.
- Access token lifetime: short (for example 15m).
- Refresh token: hashed in DB, rotated on use, invalidated on logout/password change.
- Login throttling: strict per IP + username/email key.
- No tokens persisted in browser `localStorage`.
- JWT issuer/audience validation is enforced (`sick-fansubs-api` -> `sick-fansubs-web`).

## MinIO Target Architecture

```text
Browser
  |
  |-- POST /api/media/images (multipart, field: file)
  |      |
  |      NestJS API
  |       |- Multer memory storage (no disk)
  |       |- Image type + size validation (max 5 MB)
  |       |- Upload via @aws-sdk/lib-storage to MinIO
  |       `- Returns { url: "https://domain.com/media/images/<key>" }
  |
  `-- GET https://domain.com/media/...
          `-- nginx proxies /media/ to MinIO :9000
```

## MinIO Implementation Files

| #   | Scope                           | Files                                                                                                                   |
| --- | ------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| 1   | Add MinIO service + volume      | `docker-compose.yml`, `docker-compose.dev.yml`                                                                          |
| 2   | Proxy media routes              | `nginx.conf`                                                                                                            |
| 3   | Add environment variables       | `.env`, `.env.example`                                                                                                  |
| 4   | Add media upload API lib        | `libs/api/media/**`                                                                                                     |
| 5   | Register media module           | `apps/api/src/app/app.module.ts`                                                                                        |
| 6   | Add dependencies                | `package.json`                                                                                                          |
| 7   | Wire account avatar upload flow | `libs/web/account/src/lib/data-access/user.service.ts`, `libs/web/account/src/lib/feature/account/account.component.ts` |

## Notes

- Keep all user-facing strings in Greek.
- Preserve `httpResource` + signals architecture.
- Prefer incremental delivery to minimize auth/account regressions.
