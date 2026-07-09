# libs/api/blog-post — `@api/blog-post`

## Purpose

Full CRUD REST API for blog posts — the fansub group's release announcements. Each blog post represents a subtitle release with download links for 1080p and/or 2160p (4K) resolutions. At least one complete resolution pair (torrent + magnet) must be provided.

## Path Alias

`@api/blog-post` → `libs/api/blog-post/src/index.ts`

## Public API (Exports)

| Export              | Description                                   |
| ------------------- | --------------------------------------------- |
| `ApiBlogPostModule` | NestJS module — import in `AppModule`         |
| `BlogPost`          | Mongoose schema class — used by `@api/search` |

## Key Files

### Controller (`src/lib/api-blog-post.controller.ts`)

| Method   | Route                | Query params       | Notes                        |
| -------- | -------------------- | ------------------ | ---------------------------- |
| `POST`   | `/api/blog-post`     | —                  | Create (JWT + Admin guarded) |
| `GET`    | `/api/blog-post`     | `pagesize`, `page` | Paginated list               |
| `GET`    | `/api/blog-post/:id` | —                  | Single post                  |
| `PATCH`  | `/api/blog-post/:id` | —                  | Update (JWT + Admin guarded) |
| `DELETE` | `/api/blog-post/:id` | —                  | Delete (JWT + Admin guarded) |

`GET` endpoints are public; write endpoints require authenticated admin access.

### Service (`src/lib/api-blog-post.service.ts`)

- `create(dto, creatorId)` — validates resolution via `validateResolution()`, creates document with `creator` ref
- `findAll(pageSize, currentPage)` — sorted by `dateTimeCreated` descending; populates `creator` and `updatedBy` refs
- `findOne(id)` — throws `NotFoundException` if missing; populates `creator` and `updatedBy` refs
- `update(id, dto, actorId)` — merges existing + incoming resolution fields, validates via `validateResolution()`, updates document and sets `updatedBy` ref
- `delete(id)` — throws `NotFoundException` if missing
- `count(options?)` — document count for pagination

### Schema (`src/lib/schemas/blog-post.schema.ts`)

MongoDB collection: `blogposts`

| Field                   | Type                | Notes                       |
| ----------------------- | ------------------- | --------------------------- |
| `title`                 | String              | Required                    |
| `subtitle`              | String              | Required                    |
| `description`           | String              | Required                    |
| `thumbnail`             | String              | URL                         |
| `downloadLink`          | String              | Optional 1080p magnet       |
| `downloadLinkTorrent`   | String              | Optional 1080p torrent      |
| `downloadLink4k`        | String              | Optional 2160p (4K) magnet  |
| `downloadLink4kTorrent` | String              | Optional 2160p (4K) torrent |
| `dateTimeCreated`       | String              | ISO string                  |
| `creator`               | ObjectId ref `User` | Set on create               |
| `updatedAt`             | Date                | Mongoose timestamp (auto)   |
| `updatedBy`             | ObjectId ref `User` | Set on update, optional     |

### DTOs

| File                      | Notes                                                                      |
| ------------------------- | -------------------------------------------------------------------------- |
| `create-blog-post.dto.ts` | All fields with `@ApiProperty`; all resolution fields are `@IsOptional()`  |
| `update-blog-post.dto.ts` | `PartialType(CreateBlogPostDto)` — all optional                            |
| `search-blog-post.dto.ts` | `searchTerm?`, `pageSize?` (min 1, default 10), `page?` (min 0, default 0) |

`CreateBlogPostDto` uses `class-validator` decorators (`@IsString`, `@MinLength`, `@IsDateString`, `@Matches`, etc.).
Resolution pair validation is handled at the **service layer**: `ApiBlogPostService.validateResolution()` checks that at least one complete pair (1080p torrent+magnet or 2160p torrent+magnet) is present, throwing `BadRequestException` otherwise. Both `create()` and `update()` call this validator — on update, existing fields are merged with the incoming DTO before validation.

## Dependencies

- `@api/user` — `User` schema (for `creator` ref)
- Exported `BlogPostFeature` and `ApiBlogPostService` are re-used by `@api/search`

## Nx Tasks

```bash
pnpm nx lint api-blog-post
```

`api-blog-post` currently exposes only a `lint` target.

## Notes

- At least one complete resolution pair (1080p torrent+magnet or 2160p torrent+magnet) is required; both pairs can be provided
- Update (PATCH) validates the merged result of existing + incoming resolution fields, preventing incomplete pairs
- Pagination uses `pagesize` (lowercase) query param — keep in sync with frontend `BlogPostService`
- `dateTimeCreated` is stored as a string (not a `Date`) — used for display sorting
- `updatedAt` is a Mongoose timestamp — automatically set on create and update
- `creator` is set on create and never changes; `updatedBy` tracks the user who last edited the post
- `update()` method requires `actorId` parameter to set `updatedBy` ref
- Do not rely on Swagger decorators for validation; only `class-validator` metadata is used by Nest validation pipe
