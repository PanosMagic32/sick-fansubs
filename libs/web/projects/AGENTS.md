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

#### `project.interface.ts`

| Type              | Description                                                                                                 |
| ----------------- | ----------------------------------------------------------------------------------------------------------- |
| `Project`         | `_id?`, `title`, `description`, `thumbnail`, `dateTimeCreated`, `creator?`, `batchDownloadLinks?: string[]` |
| `CreateProject`   | Project fields without `_id` / `creator`                                                                    |
| `EditProject`     | Project fields without `dateTimeCreated` / `creator`                                                        |
| `ProjectResponse` | `{ projects: Project[]; count: number }`                                                                    |

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
- Grid layout: `repeat(auto-fill, minmax(340px, 1fr))` — fills available width automatically

#### `project-create/project-create.component.ts` (lazy)

- Reactive form with dynamic `batchDownloadLinks` `FormArray`
- Add / remove download link fields at runtime
- On success: navigates to `/projects`

#### `project-details/project-details.component.ts` (lazy)

- Read-only view of a single project
- Lists all batch download links; opens each with `window.open()`
- Back button via `Location.back()`

#### `project-edit/project-edit.component.ts` (lazy)

- Pre-fills form from loaded project (including rebuilding `batchDownloadLinks` `FormArray`)
- Handles update (PATCH) + delete (DELETE)
- Mirrors `ProjectCreateComponent` for link add/remove

### `src/lib/ui/`

| Component                        | Selector               | Description                                                                                                               |
| -------------------------------- | ---------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `project-item.component.ts`      | `sf-project-item`      | Card: thumbnail, title, date. "More" navigates to details. Edit FAB for admins. Priority image loading for first 2 items. |
| `project-item-form.component.ts` | `sf-project-item-form` | Dumb form presentation. Renders base fields + dynamic batch download link list with add/remove controls.                  |

## Dependencies

- `@web/auth` — `authGuard` (for create/edit routes)
- `@web/shared` — `TokenService`, `NoContentComponent`, `WebConfigService`
- `@api/user` — `User` type

## Nx Tasks

```bash
pnpm nx lint web-projects
pnpm nx test web-projects
```

## Notes

- `batchDownloadLinks` is a `FormArray` — always rebuild it when pre-filling form from an existing project (clear first, then patch each entry)
- The `/projects/create` route must come **before** `/projects/:id` in the route list to avoid `create` being matched as an `:id` param — verify `lib.routes.ts` ordering
