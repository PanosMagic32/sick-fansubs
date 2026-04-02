# Atlas + MinIO Setup Checklist

This checklist covers the current stack when MongoDB is hosted in Atlas and media files are stored in MinIO.

Current media consumers:

- account avatar uploads
- blog-post thumbnail uploads
- project thumbnail uploads

## 1) Local Development (Atlas DB)

1. Copy `.env.example` to `.env` and set:
   - `NODE_ENV=development`
   - `DATABASE_URL_DEV=<your Atlas URI>`
   - `MINIO_ENDPOINT=http://minio:9000`
   - `MINIO_ACCESS_KEY=<local minio user>`
   - `MINIO_SECRET_KEY=<local minio secret>`
   - `MINIO_BUCKET=images`
   - `MINIO_PUBLIC_BASE_URL=http://localhost:4200`
2. Start local stack without local MongoDB:
   - `pnpm run docker:dev`
   - API image is built from `Dockerfile.api` and web image from `Dockerfile.web`
3. Validate services:
   - `curl -i http://localhost:4200/health`
   - `curl -i http://localhost:4200/api/health`
   - `curl -i http://localhost:9001`
4. Smoke test media upload (authenticated):
   - upload image -> expect `201`
   - upload invalid file -> expect `400`
   - fetch returned `/media/images/...` URL -> expect `200`
5. Smoke test UI integrations:
   - upload an avatar from `/account` and save the profile
   - create or edit a blog post with a thumbnail upload
   - create or edit a project with a thumbnail upload and batch links

## 2) Production Environment (Atlas DB)

1. Set production secrets/env:
   - `NODE_ENV=production`
   - `DATABASE_URL=<your Atlas URI>`
   - `MINIO_ENDPOINT=<internal MinIO endpoint>`
   - `MINIO_ACCESS_KEY=<strong secret>`
   - `MINIO_SECRET_KEY=<strong secret>`
   - `MINIO_BUCKET=images`
   - `MINIO_REGION=us-east-1`
   - `MINIO_PUBLIC_BASE_URL=https://<your-public-domain>`
2. Ensure network pathing:
   - `/api/*` proxies to API
   - `/media/*` proxies to MinIO
3. Ensure MinIO persistence is enabled (named volume mounted on `/data`).
4. Deploy and verify:
   - `curl -i https://<domain>/health`
   - `curl -i https://<domain>/api/health`
   - upload one image and verify `https://<domain>/media/images/<key>` returns `200`
   - verify account, blog-post, and project media previews render correctly through nginx

## 3) Security/Operations Recommendations

1. Keep MinIO console private (firewall/VPN/IP allowlist).
2. Rotate MinIO keys and store secrets in a vault/secret manager.
3. Enable backups for MinIO volume.
4. Monitor API 4xx/5xx on `/api/media/images` and MinIO availability.
