# libs/web/search — `@web/search`

## Purpose

Full-text search page that queries both blog posts and projects at once. Supports type filtering, free-text search, and paginated results. Query params are reflected in the URL for shareability.

## Path Alias

`@web/search` → `libs/web/search/src/index.ts`

## Public API (Exports)

| Export         | Description                                            |
| -------------- | ------------------------------------------------------ |
| `searchRoutes` | Route config — mounted at `/search` in `app.routes.ts` |

## Routes (`src/lib/lib.routes.ts`)

| Path      | Component            | Load  |
| --------- | -------------------- | ----- |
| `/search` | `WebSearchComponent` | Eager |

## Key Files

### `src/lib/data-access/`

#### `search.service.ts`

Imperative HTTP service using RxJS pipeline (not `httpResource`):

- `search(searchTerm, type, pageSize, page)` — calls `GET /api/search` with query params
- Maintains signals:
  - `results: WritableSignal<Searchable[]>`
  - `isLoading: WritableSignal<boolean>`
  - `total: WritableSignal<number>`
- Error handling via `MatSnackBar` (Greek error message)

#### `search-form.interface.ts`

| Type               | Description                                                                                                                                                                   |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SearchTypeOption` | Re-exported from `@shared/types`                                                                                                                                              |
| `SearchFormModel`  | Typed form: `searchTerm`, `type`                                                                                                                                              |
| `Searchable`       | Re-exported from `@shared/types`; common result shape: `_id`, `title`, `subtitle?`, `description`, `thumbnail`, `dateTimeCreated`, `downloadLink?`, `downloadLink4k?`, `type` |
| `SearchResponse`   | `{ results: Searchable[]; total: number }`                                                                                                                                    |

### `src/lib/feature/`

#### `web-search.component.ts` — `WebSearchComponent` (`sf-web-search`)

Smart component:

- Form with `searchTerm` text input + `type` select (`all` / `blog-post` / `project`)
- Reads and writes `searchTerm`, `page`, `pageSize` to URL query params
- Re-runs search when `MatPaginator` changes
- Displays results via `WebSearchResultItemComponent`
- Shows `NoContentComponent` when results are empty
- Uses `StatusCardComponent` from `@web/shared` for loading state

### `src/lib/ui/`

#### `web-search-result-item.component.ts` — `WebSearchResultItemComponent` (`sf-web-search-result-item`)

Renders a single search result card:

- Shows thumbnail, title, type badge, download button
- "More" button navigates to `/projects/:id` (note: only works correctly for project type results)
- Admin edit FAB — navigates to `/:id/edit` (blog post) or `/projects/:id/edit` (project) based on `type`

## Dependencies

- `@web/shared` — `NoContentComponent`, `StatusCardComponent`, `TokenService` (for role-derived admin edit FAB), `WebConfigService`

## Nx Tasks

```bash
pnpm nx lint web-search
```

## Notes

- `SearchService` uses RxJS (not `httpResource`) unlike other web services — candidate for refactoring for consistency
- The "More" navigation is intentionally shown only for project-type results and routes to `/projects/:id`
- Shared contracts now come from `@shared/types` to avoid drift with API-side search result types
