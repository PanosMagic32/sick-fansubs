# Sick-Fansubs

Nx monorepo for the Sick Fansubs website.

## Stack

- Angular 21 frontend
- NestJS 11 API
- MongoDB Atlas
- MinIO object storage for managed media uploads
- Nx workspace orchestration
- Docker Compose for local and production-style container runs

## Current Capabilities

- Account profile management with avatar upload or manual avatar URL entry
- Blog-post create and edit flows with managed thumbnail uploads
- Project create and edit flows with managed thumbnail uploads
- Project batch downloads with named torrent and magnet links plus optional 4K links
- Public media delivery through `/media/*` proxied to MinIO

## Development

### Install

```bash
pnpm install
```

### Run the apps locally with Docker

```bash
pnpm run docker:dev
```

This starts the Angular app, the NestJS API, and MinIO. MongoDB is expected to be provided through Atlas via `DATABASE_URL_DEV`.

### Build

```bash
pnpm run build:all
```

### Lint

```bash
pnpm nx run-many -t lint -p web,api
```

## Operations Notes

- Swagger is available at `/api-docs`.
- Managed uploads are accepted through `POST /api/media/images`.
- Public media URLs are served from `/media/images/<key>`.
- Production container startup uses `pnpm run docker:prod`.

## Additional Docs

- `docs/atlas-minio-setup-checklist.md`
- `docs/account-pages-next-plan.md`
