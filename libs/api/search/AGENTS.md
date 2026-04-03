# libs/api/search — `@api/search`

## Purpose

Cross-entity full-text search across both blog posts and projects. Builds MongoDB `$or` regex queries, runs aggregation pipelines with pagination stages in parallel, then merges and sorts results. Supports type filtering and optional date range filtering.

## Path Alias

`@api/search` → `libs/api/search/src/index.ts`

## Public API (Exports)

| Export            | Description                           |
| ----------------- | ------------------------------------- |
| `ApiSearchModule` | NestJS module — import in `AppModule` |

## Key Files

### Controller (`src/lib/api-search.controller.ts`)

`GET /api/search`

Query params (via `SearchDto`):
| Param | Type | Default | Notes |
|---|---|---|---|
| `searchTerm` | string | — | Optional; matched against `title`, `description`, `subtitle` |
| `type` | `all` \| `blog-post` \| `project` | `all` | Filter by entity type |
| `page` | number | `0` | Zero-based page index |
| `pageSize` | number | `10` | Min 1 |
| `dateFrom` | string → Date | — | Optional date filter |
| `dateTo` | string → Date | — | Optional date filter |

### Service (`src/lib/api-search.service.ts`)

- `search(options: SearchOptions)` — core method
  1. Builds `$or` regex filter on `title`, `description`, `subtitle`
  2. Calculates `skip` and `limit` from pagination params
  3. Runs aggregation pipelines on both `BlogPost` and `Project` models in parallel via `Promise.all`:
     - `$match` stage applies regex filters
     - `$sort` stage orders by `dateTimeCreated` descending
     - `$skip` / `$limit` stages applied **at database level** (not in-memory)
     - `$project` stage selects fields and adds `type` enum
  4. Counts total matching documents in each collection (before pagination)
  5. Merges paginated result arrays, tags with `type: 'blog-post' | 'project'`
  6. For 'all' type, re-sorts combined results by `dateTimeCreated` to maintain correct cross-collection ordering
  7. Returns `{ results, total }`

**Performance**: Pagination now happens at the database level using MongoDB's aggregation pipeline `$skip` and `$limit` stages, avoiding in-memory pagination of large result sets.

### DTOs

`src/lib/dtos/search.dto.ts` — `SearchDto`:

- `searchTerm?` — optional free-text
- `type?` — enum `blog-post | project | all`, default `all`
- `dateFrom?`, `dateTo?` — transformed to `Date` via `@Transform`
- `pageSize` — number (min 1, default 10)
- `page` — number (min 0, default 0)

### Interfaces

| File                                     | Interface       | Description                                                                                                                               |
| ---------------------------------------- | --------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `interfaces/search-options.interface.ts` | `SearchOptions` | Internal service contract: `searchTerm?`, `type?`, `filters?`, `pagination`, `sort?`                                                      |
| `interfaces/searchable.interface.ts`     | `Searchable`    | Common result shape: `_id`, `title`, `subtitle?`, `description`, `thumbnail`, `dateTimeCreated`, `downloadLink`, `downloadLink4k`, `type` |

## Dependencies

- `@api/blog-post` — `ApiBlogPostModule` + `BlogPost` model (accessed via Mongoose injection)
- `@api/project` — `ApiProjectModule` + `Project` model (accessed via Mongoose injection)

## Nx Tasks

```bash
pnpm nx lint api-search
```

## Notes

- ✅ **Pagination optimized** — Refactored to use MongoDB aggregation pipeline `$skip` and `$limit` stages instead of in-memory slicing. Queries only fetch needed results from the database.
- For 'all' type results, `$skip` and `$limit` are applied to **each** collection query independently, then results are combined and re-sorted. This may return slightly more or fewer than `pageSize` items when combining across collections with heterogeneous item counts.
- `Searchable` is shared via `@shared/types`, reducing frontend/backend contract drift.
- Date filtering (`dateFrom`, `dateTo`) is declared in the DTO but needs verification for service-level application.
