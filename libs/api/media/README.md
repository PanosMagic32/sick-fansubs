# api-media

Media upload API library backed by MinIO.

## Endpoint

- `POST /api/media/images`

## Behavior

- requires an authenticated user
- accepts multipart uploads under the `file` field
- allows `jpeg`, `png`, `gif`, and `webp`
- rejects files larger than 5 MB
- stores files in the configured MinIO bucket
- returns a public URL under `/media/images/<key>`

## Consumers

- account avatar uploads
- blog-post thumbnail uploads
- project thumbnail uploads
