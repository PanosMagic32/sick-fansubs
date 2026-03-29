# Account Pages Plan (Next Iteration)

## Scope

Improve reliability, UX, and maintainability of the `/account` experience across profile editing, auth session handling, and favorites management.

## Priorities

### P0 — Stability and Data Integrity

- Harden URL handling for downloads and avatar image usage (prevent runtime errors on malformed URLs).
- Reduce unnecessary network reloads after successful profile/favorites mutations.
- Improve error-message mapping for profile update and favorites actions (status-aware UX).
- Add safer loading/error states for favorites when one resource succeeds and the other fails.

### P1 — Form and UX Improvements

- ~~Add avatar URL validation and preview fallback behavior.~~ **Done (P0).**
- **Image upload via MinIO (self-hosted object storage):** see dedicated section below.
- Improve password UX:
  - Show/hide toggles.
  - Clearer password mismatch timing.
  - Better strength/requirements feedback.
- Add unsaved-changes route protection for account edits.

### P2 — Feature Additions

- Add account deletion flow with explicit confirmation and forced logout.
- Add optional favorites sort controls (newest/oldest).
- Add pagination

## Backend Coordination

- Keep user-profile/favorites access rules as "self or admin".
- Preserve strict DTO validation behavior (global `ValidationPipe` with `transform`, `whitelist`, `forbidNonWhitelisted`).
- Ensure update endpoint error semantics are consistent for frontend mapping (`400/401/403/404/409`).

---

## User Security Hardening (Auth + Session)

### Decision

- Keep the current custom auth implementation for now (no `better-auth` migration in this iteration).
- Prioritize security hardening in place, then re-evaluate auth-framework migration later if OAuth/SSO/2FA requirements grow.

### Implementation Steps

| #   | Scope                                                                                            | Files                                                                                              |
| --- | ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| 1   | Add login/register rate limiting with `@nestjs/throttler` (anti brute-force)                     | `apps/api/src/app/app.module.ts`, auth controller/module files                                     |
| 2   | Move auth token transport from `localStorage` to `HttpOnly` secure cookie                        | `libs/api/auth/**`, `libs/web/shared/src/lib/data-access/token.service.ts`, web interceptor/guards |
| 3   | Add refresh-token rotation flow (short-lived access token + revocable refresh token)             | `libs/api/auth/**`, user schema/service, web auth data-access                                      |
| 4   | Add centralized auth error mapping on frontend (expired, invalid, revoked sessions)              | `libs/web/auth/**`, `libs/web/shared/**`, account/auth features                                    |
| 5   | Add optional email verification for registration (SMTP-backed)                                   | auth module + email infrastructure                                                                 |
| 6   | Add auth-focused tests (rate limits, cookie flags, refresh reuse detection, logout invalidation) | `libs/api/auth/**/*.spec.ts`, web auth tests                                                       |

### Security Baseline Targets

- Cookie flags: `HttpOnly`, `Secure` (prod), `SameSite=Lax`.
- Access token lifetime: short (for example 15m).
- Refresh token: hashed in DB, rotated on use, invalidated on logout/password change.
- Login throttling: strict per IP + username/email key.
- No tokens persisted in browser `localStorage`.

---

## Image Upload — MinIO (Self-Hosted Object Storage)

### Architecture

```
Browser
  │
  ├── POST /api/media/images   (multipart, field: "file")
  │         │
  │       NestJS API
  │         ├── Multer (memory storage — no disk, streams to MinIO)
  │         ├── Type + size validation (images only, max 5 MB)
  │         ├── Upload via @aws-sdk/lib-storage → MinIO container
  │         └── Returns { url: "https://domain.com/media/images/<key>" }
  │
  └── GET  https://domain.com/media/...
              └── nginx proxies /media/ → MinIO :9000
```

DB always stores only URL strings. MinIO holds the binary data.

### Implementation Steps

| #   | Scope                                                                                                                               | Files                                                                                                                                                                                                                                                                                                 |
| --- | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0   | **Rollback current local-disk avatar upload changes** and restore URL-only avatar update behavior until MinIO media module is ready | `apps/api/src/main.ts`, `libs/api/user/src/lib/user.controller.ts`, `libs/api/user/src/lib/user.service.ts`, `libs/web/account/src/lib/data-access/user.service.ts`, `libs/web/account/src/lib/feature/account.component.ts`, `libs/web/account/src/lib/feature/account.component.html`, `.gitignore` |
| 1   | Add MinIO service + named volume to Docker Compose (dev + prod)                                                                     | `docker-compose.yml`, `docker-compose.dev.yml`                                                                                                                                                                                                                                                        |
| 2   | Proxy `/media/` to MinIO in nginx                                                                                                   | `nginx.conf`                                                                                                                                                                                                                                                                                          |
| 3   | Add `MINIO_*` env vars (root user, password, bucket, endpoint)                                                                      | `.env` / `.env.example`                                                                                                                                                                                                                                                                               |
| 4   | Create `libs/api/media` NestJS lib — one `POST /api/media/images` endpoint                                                          | ~4 files                                                                                                                                                                                                                                                                                              |
| 5   | Register `ApiMediaModule` in `AppModule`                                                                                            | `apps/api/src/app/app.module.ts`                                                                                                                                                                                                                                                                      |
| 6   | Install `@aws-sdk/client-s3`, `@aws-sdk/lib-storage`, `multer`, `@types/multer`                                                     | `package.json`                                                                                                                                                                                                                                                                                        |
| 7   | Revert direct-disk upload approach from earlier in this session (avatar endpoint in `UserController`)                               | `user.controller.ts`, `user.service.ts`, `main.ts`                                                                                                                                                                                                                                                    |
| 8   | Update account component to call `/api/media/images` first, then patch avatar URL                                                   | `account.component.ts`                                                                                                                                                                                                                                                                                |
| 9   | Blog-post / project create-edit forms: optionally add upload button alongside URL field                                             | future P2                                                                                                                                                                                                                                                                                             |

### MinIO Docker Snippet

```yaml
minio:
  image: minio/minio:latest
  command: server /data --console-address ":9001"
  environment:
    MINIO_ROOT_USER: ${MINIO_ROOT_USER}
    MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD}
  volumes:
    - minio_data:/data
  ports:
    - '9000:9000'
    - '9001:9001' # console — expose only via SSH tunnel in prod
  healthcheck:
    test: ['CMD', 'mc', 'ready', 'local']
    interval: 30s
    timeout: 10s
    retries: 3
  restart: unless-stopped
```

### Nginx Snippet

```nginx
location /media/ {
  proxy_pass         http://minio:9000/;
  proxy_set_header   Host $host;
}
```

### Notes

- Bucket policy: `public-read` for the images bucket (avatars + thumbnails are public assets).
- Reuse this pipeline for blog-post and project thumbnails — no per-domain storage logic.
- VPS storage: 250 GB is ample. MinIO idle RAM ≈ 256–512 MB.

---

## Notes

- Keep all user-facing strings in Greek.
- Preserve existing `httpResource` + signal-driven architecture.
- Prefer incremental changes to avoid regressions in auth/account flows.
