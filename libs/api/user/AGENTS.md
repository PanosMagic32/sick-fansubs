# libs/api/user — `@api/user`

## Purpose

User account management for the NestJS API. Handles registration, lookup, updates, deletion, password hashing, and role-based access control (RBAC).

## Path Alias

`@api/user` → `libs/api/user/src/index.ts`

## Public API (Exports)

| Export          | Description                                                  |
| --------------- | ------------------------------------------------------------ |
| `ApiUserModule` | NestJS module — import in `AppModule`                        |
| `UserService`   | Service — re-exported for use by `@api/auth`                 |
| `LoginUserDto`  | DTO used by `@api/auth` login flow                           |
| `User`          | Mongoose schema class — used as type reference by other libs |

## Key Files

### Controller (`src/lib/user.controller.ts`)

| Method   | Route                             | Auth           | Access                                      |
| -------- | --------------------------------- | -------------- | ------------------------------------------- |
| `POST`   | `/api/user`                       | None           | Public (registration)                       |
| `GET`    | `/api/user`                       | `JwtAuthGuard` | Admin only                                  |
| `GET`    | `/api/user/:id`                   | `JwtAuthGuard` | Self or admin                               |
| `GET`    | `/api/user/:id/favorites`         | `JwtAuthGuard` | Self or admin                               |
| `GET`    | `/api/user/:id/favorites/posts`   | `JwtAuthGuard` | Self or admin (supports `pagesize`, `page`) |
| `PUT`    | `/api/user/:id/favorites/:postId` | `JwtAuthGuard` | Self or admin                               |
| `DELETE` | `/api/user/:id/favorites/:postId` | `JwtAuthGuard` | Self or admin                               |
| `PATCH`  | `/api/user/:id`                   | `JwtAuthGuard` | Self or admin                               |
| `DELETE` | `/api/user/:id`                   | `JwtAuthGuard` | Self or admin                               |

Actor is extracted from `req.user` (set by Passport JWT strategy).

### Service (`src/lib/user.service.ts`)

- `create(dto)` — checks uniqueness of `email` and `username`, bcrypt-hashes password
- `findAll()` — admin only, returns public profiles (no passwords)
- `findOne(id, requesterId)` — self or admin check
- `update(id, dto, requesterId)` — hashes new password if provided
- `remove(id, requesterId)` — self or admin
- `getFavoriteBlogPostIds(id, actor)` — returns favorite blog post ids
- `getFavoriteBlogPosts(id, actor, pageSize?, currentPage?)` — returns paginated populated favorite blog post documents and total count
- `addFavoriteBlogPost(id, postId, actor)` — idempotent add via `$addToSet`
- `removeFavoriteBlogPost(id, postId, actor)` — idempotent remove via `$pull`
- `addCreatedBlogPost(id, postId)` — idempotent add created blog post id via `$addToSet`
- `removeCreatedBlogPost(id, postId)` — idempotent remove created blog post id via `$pull`
- `toPublicUser(user)` — strips `password` from response
- RBAC helpers: `assertAdmin()`, `assertCanAccessUser()`, `assertValidId()`

### Schema (`src/lib/schemas/user.schema.ts`)

MongoDB collection: (default Mongoose collection name)
| Field | Type | Notes |
|---|---|---|
| `username` | String | Unique |
| `password` | String | Hashed with bcrypt |
| `email` | String | Unique |
| `avatar` | String | Optional |
| `isAdmin` | Boolean | Default `false` |
| `favoriteBlogPostIds` | ObjectId[] (`BlogPost`) | Default `[]` |
| `createdBlogPostIds` | ObjectId[] (`BlogPost`) | Default `[]` |

`timestamps: true` — `createdAt` / `updatedAt` added automatically.
`toJSON` transform removes `password` from all serialized output.

### DTOs

| File                 | Fields                                                                                                                   |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `create-user.dto.ts` | `username` (3–32), `email` (valid, max 254), `password` (8–128 + lowercase/uppercase/number/symbol), `avatar?` (max 512) |
| `login-user.dto.ts`  | `username` (3–32), `password` (8–128) — used by `@api/auth`                                                              |
| `update-user.dto.ts` | All optional: `email?`, `avatar?`, `password?` (8–128 + lowercase/uppercase/number/symbol)                               |

## Dependencies

None (foundational lib — depended on by `@api/auth` and `@api/search`)

## Nx Tasks

```bash
pnpm nx lint api-user
```

`api-user` currently exposes only a `lint` target.

## Notes

- `JwtAuthGuard` and `AdminGuard` are exported from this lib and used by other API controllers (`blog-post`, `project`)
- Password validation rules (8–128 chars) are defined in DTOs — ensure they stay in sync with the frontend `SignupFormComponent`
- Favorites access is enforced by `assertCanAccessUser` (self or admin)
