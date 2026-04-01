# libs/web/projects — `@web/projects`

## Purpose

Fansub project pages — ongoing subtitle series or batch releases. Each project can have multiple batch download links. Supports paginated list, project details view, and admin create/edit flows.

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

Compatibility parsing in this service normalizes legacy payloads before they reach UI components:

- list responses returned as arrays, numeric-key objects, or single project objects
- `_id` values returned as plain strings or Mongo Extended JSON (`{ "$oid": "..." }`)
- `batchDownloadLinks` entries returned as structured objects, plain strings, or char-indexed objects

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
- `batchDownloadLinks: FormArray<FormControl<string>>` — dynamic array

### `src/lib/feature/`

#### `project-list/project-list.component.ts` — `ProjectListComponent` (`sf-project-list`)

- Paginated list; syncs `page` and `pageSize` to query params
- Shows FAB for creating (visible only to admins)
- Uses `ProjectItemComponent` for each card
- Responsive grid layout via semantic breakpoints:
  - base: 1 column
  - `2xl`: 2 columns
  - `4xl`: 3 columns

#### `project-create/project-create.component.ts` (lazy)

- Reactive form with dynamic `batchDownloadLinks` `FormArray`
- Add / remove download link fields at runtime
- On success: navigates to `/projects`

#### `project-details/project-details.component.ts` (lazy)

- Read-only view of a single project
- Lists all batch download links; opens each with `window.open()`
- Back button via `Location.back()`
- Admin-only edit action button (`/projects/:id/edit`)

#### `project-edit/project-edit.component.ts` (lazy)

- Pre-fills form from loaded project (including rebuilding `batchDownloadLinks` `FormArray`)
- Handles update (PATCH) + delete (DELETE)
- Mirrors `ProjectCreateComponent` for link add/remove
- Edit mode currently allows temporarily incomplete batch-link controls during migration
- TODO in code: restore strict required validators after legacy data migration

### `src/lib/ui/`

| Component                        | Selector               | Description                                                                                                                                                                              |
| -------------------------------- | ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `project-item.component.ts`      | `sf-project-item`      | Card with creator/editor metadata: 70px circular avatar with fallback to logo, title, created/edited dates with creator/editor usernames in Greek, edit FAB for admins. Responsive grid. |
| `project-item-form.component.ts` | `sf-project-item-form` | Dumb form presentation. Renders base fields + dynamic batch download link list with add/remove controls.                                                                                 |

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

- `batchDownloadLinks` is a `FormArray` — always rebuild it when pre-filling form from an existing project (clear first, then patch each entry)
- The `/projects/create` route must come **before** `/projects/:id` in the route list to avoid `create` being matched as an `:id` param — verify `lib.routes.ts` ordering
- Creator avatar is 70px circular with 35% secondary color border and soft shadow; defaults to `/logo/logo.png` if creator has no avatar
- Timestamps and usernames are displayed in Greek ("Προστέθηκε: ... από", "Επεξεργάστηκε: ... από")
- Editor username shown only if `updatedBy` exists and differs from `creator`; falls back to "Άγνωστος χρήστης" if username unavailable
- During migration window, edit payload omits `batchDownloadLinks` when no complete link pairs are present, preventing accidental overwrite of legacy links
