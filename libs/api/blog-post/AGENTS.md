# libs/api/blog-post — `@api/blog-post`

## Purpose

Full CRUD REST API for blog posts — the fansub group's release announcements. Each blog post represents a subtitle release with download links for multiple formats (standard, torrent, 4K, 4K torrent).

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

- `create(dto)` — creates a new blog post document
- `findAll(pageSize, currentPage)` — sorted by `dateTimeCreated` descending
- `findOne(id)` — throws `NotFoundException` if missing
- `update(id, dto)` — returns updated document
- `delete(id)` — throws `NotFoundException` if missing
- `count(options?)` — document count for pagination

### Schema (`src/lib/schemas/blog-post.schema.ts`)

MongoDB collection: `blogposts`
| Field | Type | Notes |
|---|---|---|
| `title` | String | Required |
| `subtitle` | String | Required |
| `description` | String | Required |
| `thumbnail` | String | URL |
| `downloadLink` | String | Primary download |
| `downloadLinkTorrent` | String | Torrent variant |
| `downloadLink4k` | String | Optional 4K |
| `downloadLink4kTorrent` | String | Optional 4K torrent |
| `dateTimeCreated` | String | ISO string |
| `creator` | ObjectId ref `User` | Reference |

### DTOs

| File                      | Notes                                                                      |
| ------------------------- | -------------------------------------------------------------------------- |
| `create-blog-post.dto.ts` | All fields with `@ApiProperty`                                             |
| `update-blog-post.dto.ts` | `PartialType(CreateBlogPostDto)` — all optional                            |
| `search-blog-post.dto.ts` | `searchTerm?`, `pageSize?` (min 1, default 10), `page?` (min 0, default 0) |

`create-blog-post.dto.ts` uses `class-validator` decorators (`@IsString`, `@MinLength`, `@IsDateString`, etc.).
This is required because the API uses global `ValidationPipe` with `whitelist: true` and `forbidNonWhitelisted: true`.

## Dependencies

- `@api/user` — `User` schema (for `creator` ref)
- Exported `BlogPostFeature` and `ApiBlogPostService` are re-used by `@api/search`

## Nx Tasks

```bash
pnpm nx lint api-blog-post
pnpm nx test api-blog-post
```

## Notes

- Pagination uses `pagesize` (lowercase) query param — keep in sync with frontend `BlogPostService`
- `dateTimeCreated` is stored as a string (not a `Date`) — used for display sorting
- Do not rely on Swagger decorators for validation; only `class-validator` metadata is used by Nest validation pipe
