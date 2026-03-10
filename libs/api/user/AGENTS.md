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

| Method   | Route           | Auth           | Access                |
| -------- | --------------- | -------------- | --------------------- |
| `POST`   | `/api/user`     | None           | Public (registration) |
| `GET`    | `/api/user`     | `JwtAuthGuard` | Admin only            |
| `GET`    | `/api/user/:id` | `JwtAuthGuard` | Self or admin         |
| `PATCH`  | `/api/user/:id` | `JwtAuthGuard` | Self or admin         |
| `DELETE` | `/api/user/:id` | `JwtAuthGuard` | Self or admin         |

Actor is extracted from `req.user` (set by Passport JWT strategy).

### Service (`src/lib/user.service.ts`)

- `create(dto)` — checks uniqueness of `email` and `username`, bcrypt-hashes password
- `findAll()` — admin only, returns public profiles (no passwords)
- `findOne(id, requesterId)` — self or admin check
- `update(id, dto, requesterId)` — hashes new password if provided
- `remove(id, requesterId)` — self or admin
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

`timestamps: true` — `createdAt` / `updatedAt` added automatically.
`toJSON` transform removes `password` from all serialized output.

### DTOs

| File                 | Fields                                                                               |
| -------------------- | ------------------------------------------------------------------------------------ |
| `create-user.dto.ts` | `username` (3–32), `email` (valid, max 254), `password` (8–128), `avatar?` (max 512) |
| `login-user.dto.ts`  | `username` (3–32), `password` (8–128) — used by `@api/auth`                          |
| `update-user.dto.ts` | All optional: `email?`, `avatar?`, `password?`                                       |

## Dependencies

None (foundational lib — depended on by `@api/auth` and `@api/search`)

## Nx Tasks

```bash
pnpm nx lint api-user
pnpm nx test api-user
```

## Notes

- `JwtAuthGuard` and `AdminGuard` are exported from this lib and used by other API controllers (`blog-post`, `project`)
- Password validation rules (8–128 chars) are defined in DTOs — ensure they stay in sync with the frontend `SignupFormComponent`
