# libs/api/search — `@api/search`

## Purpose

Cross-entity full-text search across both blog posts and projects. Builds MongoDB `$or` regex queries, runs them in parallel, then merges and paginates results in-memory. Supports type filtering and optional date range filtering.

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
  2. Runs `BlogPost` and `Project` model queries in parallel via `Promise.all` (only queries enabled types)
  3. Merges arrays, tags each result with `type: 'blog-post' | 'project'`
  4. Re-sorts combined results by `dateTimeCreated` descending
  5. Applies in-memory pagination (`skip` / `take`)
  6. Returns `{ results, total }`

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
pnpm nx test api-search
```

## Notes

- Pagination is performed **in-memory** after merging both entity arrays — may be inefficient for large datasets. Consider MongoDB `$facet` aggregation for a future improvement.
- `Searchable` is shared via `@shared/types`, reducing frontend/backend contract drift.
- Date filtering (`dateFrom`, `dateTo`) is declared in the DTO but the service-level filter application (`filters.dateFrom`, `filters.dateTo`) should be verified for completeness.
