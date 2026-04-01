# libs/api/project — `@api/project`

## Purpose

Full CRUD REST API for fansub "projects" — ongoing subtitle series or batch releases. Each project can have multiple batch download links.

## Path Alias

`@api/project` → `libs/api/project/src/index.ts`

## Public API (Exports)

| Export             | Description                                   |
| ------------------ | --------------------------------------------- |
| `ApiProjectModule` | NestJS module — import in `AppModule`         |
| `Project`          | Mongoose schema class — used by `@api/search` |

## Key Files

### Controller (`src/lib/project.controller.ts`)

| Method   | Route              | Query params       | Notes                        |
| -------- | ------------------ | ------------------ | ---------------------------- |
| `POST`   | `/api/project`     | —                  | Create (JWT + Admin guarded) |
| `GET`    | `/api/project`     | `pagesize`, `page` | Paginated list               |
| `GET`    | `/api/project/:id` | —                  | Single project               |
| `PATCH`  | `/api/project/:id` | —                  | Update (JWT + Admin guarded) |
| `DELETE` | `/api/project/:id` | —                  | Delete (JWT + Admin guarded) |

`GET` endpoints are public; write endpoints require authenticated admin access.

### Service (`src/lib/project.service.ts`)

- `create(dto)` — creates a new project document with `creator` ref
- `findAll(pageSize, currentPage)` — sorted by `dateTimeCreated` descending; populates `creator` and `updatedBy` refs
- `findOne(id)` — throws `NotFoundException` if missing; populates `creator` and `updatedBy` refs
- `update(id, dto, actorId)` — updates document and sets `updatedBy` ref to `actorId`; populates refs
- `remove(id)` — throws `NotFoundException` if missing

### Schema (`src/lib/schemas/project.schema.ts`)

MongoDB collection: `projects`
| Field | Type | Notes |
|---|---|---|
| `title` | String | Required |
| `description` | String | Required |
| `thumbnail` | String | URL |
| `dateTimeCreated` | String | ISO string |
| `creator` | ObjectId ref `User` | Set on create |
| `updatedAt` | Date | Mongoose timestamp (auto) |
| `updatedBy` | ObjectId ref `User` | Set on update, optional |
| `batchDownloadLinks` | String[] | Array of download URLs |

### DTOs

| File                    | Notes                                                                                  |
| ----------------------- | -------------------------------------------------------------------------------------- |
| `create-project.dto.ts` | `title`, `description`, `thumbnail`, `batchDownloadLinks: string[]`, `dateTimeCreated` |
| `update-project.dto.ts` | `PartialType(CreateProjectDto)` — all optional                                         |

`create-project.dto.ts` uses `class-validator` metadata (`@IsString`, `@MinLength`, `@IsArray`, `@IsDateString`, etc.).
This is required because the API uses global `ValidationPipe` with `whitelist: true` and `forbidNonWhitelisted: true`.

## Dependencies

- `@api/user` — `User` schema (for `creator` ref)
- Exported `ProjectFeature` and `ProjectService` are re-used by `@api/search`

## Nx Tasks

```bash
pnpm nx lint api-project
```

`api-project` currently exposes only a `lint` target.

## Notes

- `batchDownloadLinks` is a flat string array — no structured metadata per link
- Mirrors the structure of `@api/blog-post` but with `batchDownloadLinks` instead of individual link fields
- `updatedAt` is a Mongoose timestamp — automatically set on create and update
- `creator` is set on create and never changes; `updatedBy` tracks the user who last edited the project
- `update()` method requires `actorId` parameter to set `updatedBy` ref
- Do not rely on Swagger decorators for validation; only `class-validator` metadata is used by Nest validation pipe
