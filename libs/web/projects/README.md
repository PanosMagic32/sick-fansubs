# projects

Projects web feature library.

## Includes

- project listing and detail pages
- admin create and edit screens
- thumbnail upload with preview
- project details header with creator and editor metadata
- admin-only edit action on the project details page
- backward-compatible project response normalization for legacy payload shapes

## Batch Downloads

Each batch supports:

- a custom batch name
- one 1080p torrent link
- one 1080p magnet link
- optional 4K torrent link
- optional 4K magnet link

The details page renders these links through dropdown menus matching the blog-post download UI.

## Compatibility

The projects data-access layer normalizes legacy API payloads to the current `Project` contract.

- supports list responses as arrays, numeric-key objects, or single-object payloads
- supports `_id` values as plain strings or Mongo Extended JSON (`{ "$oid": "..." }`)
- supports batch download links as structured objects, strings, or char-indexed objects
