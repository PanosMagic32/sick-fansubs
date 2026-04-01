# Account Pages Plan (Next Iteration)

## Scope

Finalize the remaining account UX features after the media pipeline rollout. Auth/session hardening (C0) and media upload delivery (C1) are complete.

## Prioritized Backlog (Criticality Order)

### C1 — Media Pipeline for Account and Future Features

Status: Completed.

1. Completed: MinIO service and persistent volume in Docker Compose (dev + prod).
2. Completed: `/media/` is proxied to MinIO in `nginx`.
3. Completed: `MINIO_*` environment variables were added.
4. Completed: `libs/api/media` now exposes `POST /api/media/images` with multipart validation and MinIO upload.
5. Completed: `ApiMediaModule` is registered in `AppModule` and reused by avatar and thumbnail flows.
6. Completed: required S3 and upload dependencies are installed.
7. Completed: account avatar flow supports file upload and manual URL override.

### C2 — Account UX and Safety Enhancements

1. Add unsaved-changes route protection for account edits.
2. Add account deletion flow with explicit confirmation and forced logout.
3. Add optional favorites sort controls (newest/oldest).

### C3 — Optional Security Extension

1. Add optional email verification during registration (SMTP-backed).

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
- Follow-up UX work should preserve the new managed media flow instead of reintroducing direct file-hosting assumptions.
