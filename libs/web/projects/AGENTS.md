# libs/web/projects — `@web/projects`

## Purpose

Fansub project pages — ongoing subtitle series or batch releases. Each project can have multiple batch download links. Each batch link requires at least one complete resolution pair (1080p torrent+magnet or 2160p torrent+magnet), or both. Supports paginated list, project details view, and admin create/edit flows.

## Path Alias

`@web/projects` → `libs/web/projects/src/index.ts`

## Public API (Exports)

| Export           | Description                                              |
| ---------------- | -------------------------------------------------------- |
| `projectsRoutes` | Route config — mounted at `/projects` in `app.routes.ts` |

## Routes (`src/lib/lib.routes.ts`)

| Path                 | Guard               | Component                 | Load  |
| -------------------- | ------------------- | ------------------------- | ----- |
| `/projects`          | —                   | `ProjectListComponent`    | Eager |
| `/projects/create`   | `authGuard` (admin) | `ProjectCreateComponent`  | Lazy  |
| `/projects/:id`      | —                   | `ProjectDetailsComponent` | Lazy  |
| `/projects/:id/edit` | `authGuard` (admin) | `ProjectEditComponent`    | Lazy  |

## Key Files

### `src/lib/data-access/`

#### `projects.service.ts`

Angular `httpResource`-based service (signals-driven):

- `getProjects(perPage, page)` → `HttpResourceRef<ProjectResponse>`
- `getProjectById(id)` → `HttpResourceRef<Project>`
- `createProject(project: Signal<CreateProject | null>)` → fires when signal is non-null
- `updateProject(id, data: Signal<EditProject | null>)` → fires when signal is non-null
- `deleteProject(id: Signal<string | null>)` → fires when signal is non-null

Compatibility parsing normalizes legacy payloads into the current `Project` shape:

- list responses returned as arrays, numeric-key objects, or single project objects
- `_id` values returned as plain strings or Mongo Extended JSON (`{ "$oid": "..." }`)
- `batchDownloadLinks` entries returned as structured objects, plain strings, or char-indexed objects
- Filter in `normalizeProject()` keeps only batches with at least one complete resolution pair

#### `batch-link.validators.ts`

Validator functions shared across all project form components:

- `batchTorrentUrlValidator()` — validates torrent URLs start with `http://` or `https://`
- `batchMagnetUrlValidator()` — validates magnet URLs start with `magnet:?xt=`
- `atLeastOneResolutionForBatch()` — cross-field FormGroup validator: requires at least one complete pair (torrent+magnet for 1080p or 2160p)

#### `project.interface.ts`

| Type              | Description                                                                                                                   |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `Project`         | `_id?`, `title`, `description`, `thumbnail`, `dateTimeCreated`, `creator?`, `updatedAt?`, `updatedBy?`, `batchDownloadLinks?` |
| `CreateProject`   | Project fields without `_id` / `creator` / `updatedAt` / `updatedBy`                                                          |
| `EditProject`     | Project fields without `dateTimeCreated` / `creator` / `updatedAt` / `updatedBy`                                              |
| `ProjectResponse` | `{ projects: Project[]; count: number }`                                                                                      |

Imports `User` type from `@api/user`.

#### `project-form.interface.ts`

`ProjectFormModel` — typed `FormGroup` shape:

- `title`, `description`, `thumbnail`
- `batchDownloadLinks: FormArray<FormGroup<BatchDownloadLinkFormModel>>` — dynamic array; each group has `atLeastOneResolutionForBatch` validator

### `src/lib/feature/`

#### `project-list/project-list.component.ts` — `ProjectListComponent` (`sf-project-list`)

- Paginated list; syncs `page` and `pageSize` to query params
- Shows FAB for creating (visible only to admins)
- Uses `ProjectItemComponent` for each card
- Responsive grid:
  - base: 1 column
  - `2xl`: 2 columns
  - `4xl`: 3 columns

#### `project-create/project-create.component.ts` (lazy)

- Reactive form with dynamic `batchDownloadLinks` `FormArray`
- Initial batch group includes `atLeastOneResolutionForBatch` cross-field validator (imported from `batch-link.validators.ts`)
- Submit filter keeps only links with at least one complete resolution pair
- Empty resolution fields sent as `undefined`
- On success: navigates to `/projects`

#### `project-details/project-details.component.ts` (lazy)

- Read-only view of a single project
- `getBatchLinks()` normalizes and filters batch links to only show those with complete pairs
- 1080p and 2160p download buttons are rendered conditionally per batch link
- Back button via `Location.back()`
- Admin-only edit action button (`/projects/:id/edit`)

#### `project-edit/project-edit.component.ts` (lazy)

- Pre-fills form from loaded project (including rebuilding `batchDownloadLinks` `FormArray`)
- `createBatchDownloadLinkGroup()` includes `atLeastOneResolutionForBatch` validator
- Submit filter keeps only links with at least one complete resolution pair
- Handles update (PATCH) + delete (DELETE)

### `src/lib/ui/`

| Component                        | Selector               | Description                                                                                                                                                                 |
| -------------------------------- | ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `project-item.component.ts`      | `sf-project-item`      | Card with creator/editor metadata: 70px circular avatar with fallback to logo, title, created/edited dates, edit FAB for admins, favorite toggle.                           |
| `project-item-form.component.ts` | `sf-project-item-form` | Dumb form presentation. Renders base fields + dynamic batch links with add/remove. `onAddBatchDownloadLink()` creates groups with `atLeastOneResolutionForBatch` validator. |

## Dependencies

- `@web/auth` — `authGuard` (for create/edit routes)
- `@web/shared` — `TokenService`, `NoContentComponent`, `WebConfigService`
- `@api/user` — `User` type

## Nx Tasks

```bash
pnpm nx lint projects
```

`projects` currently exposes only a `lint` target.

## Notes

- Each batch download link must contain at least one complete resolution pair (1080p torrent+magnet or 2160p torrent+magnet); both pairs may be provided
- `atLeastOneResolutionForBatch` validator is defined in `batch-link.validators.ts` and imported by `project-create`, `project-edit`, and `project-item-form`
- 1080p download fields no longer have `required` HTML attributes or `Validators.required` in batch link groups
- The `/projects/create` route must come **before** `/projects/:id` in the route list — verify `lib.routes.ts` ordering
- Creator avatar is 70px circular with 35% secondary color border and soft shadow; defaults to `/logo/logo.png`
- Timestamps and usernames are displayed in Greek ("Προστέθηκε: ... από", "Επεξεργάστηκε: ... από")
- Editor username shown only if `updatedBy` exists and differs from `creator`
- Project list and card styles include `min-width: 0`/wrapping safeguards for mobile overflow
