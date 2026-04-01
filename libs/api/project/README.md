# api-project

Project API feature library.

## Includes

- CRUD endpoints for projects
- unique slug generation
- managed thumbnail cleanup on replace and delete
- edited metadata sanitization for read models

## Batch Download Payload

`batchDownloadLinks` now accepts objects with:

- `name`
- `downloadLinkTorrent`
- `downloadLink`
- optional `downloadLink4kTorrent`
- optional `downloadLink4k`
