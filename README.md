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

For local media previews, development compose overrides `MINIO_PUBLIC_BASE_URL` to `http://localhost:4200` for the API container. This ensures upload responses return local `/media/...` URLs instead of the production domain.

### Dockerfiles

- `Dockerfile.api`: API production image (`api`) and API development image (`api-dev`)
- `Dockerfile.web`: web production image (`web`) and web development image (`web-dev`)

### Validate Docker Images

```bash
docker build -f Dockerfile.api --target api -t sick-api:test .
docker build -f Dockerfile.web --target web -t sick-web:test .
```

### Docker Migration Note

The workspace no longer uses a single root `Dockerfile`.

- Old pattern: `docker build -f Dockerfile --target api ...`
- New pattern: `docker build -f Dockerfile.api --target api ...`

- Old pattern: `docker build -f Dockerfile --target web ...`
- New pattern: `docker build -f Dockerfile.web --target web ...`

- Old compose builds: `dockerfile: Dockerfile`
- New compose builds: `dockerfile: Dockerfile.api` for `api`, `dockerfile: Dockerfile.web` for `web`

Use `pnpm run docker:dev` and `pnpm run docker:prod` as before; those scripts now resolve through the split Dockerfiles via compose.

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
