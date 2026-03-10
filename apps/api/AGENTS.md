# apps/api — NestJS Backend Application

## Purpose

The main NestJS REST API for sick-fansubs. Exposes all backend functionality under the `/api` prefix. In local/dev mode, Swagger UI is available at `/api-docs`.

## Tech Stack

- **Framework**: NestJS 11 with Express platform
- **Database**: MongoDB via Mongoose
- **Auth**: Passport.js + JWT (`passport-jwt`)
- **Validation**: `class-validator` / `class-transformer` (global `ValidationPipe`)
- **Build**: Webpack (custom `webpack.config.js`)

## Entry Point

`src/main.ts` — bootstraps `AppModule`, sets global prefix `api`, global `ValidationPipe`, configures CORS from `environment.corsOptions`, and starts the server on `PORT` env var (default `3333`).

## App Module (`src/app/app.module.ts`)

Assembles all feature modules:

| Module                        | Source lib       | Domain                |
| ----------------------------- | ---------------- | --------------------- |
| `ApiAuthModule`               | `@api/auth`      | JWT login             |
| `ApiUserModule`               | `@api/user`      | User accounts         |
| `ApiBlogPostModule`           | `@api/blog-post` | Blog posts / releases |
| `ApiProjectModule`            | `@api/project`   | Fansub projects       |
| `ApiSearchModule`             | `@api/search`    | Cross-entity search   |
| `ConfigModule` (global)       | `@nestjs/config` | Env vars              |
| `MongooseModule.forRootAsync` | —                | MongoDB connection    |

## Health Endpoint

`GET /api/health` → `{ status, timestamp, uptime }` — implemented in `src/app/health.controller.ts`.

## Database Connection

Reads `DATABASE_URL` (prod) or `DATABASE_URL_DEV` (dev) from env.

- Prod DB name: `sick-db`
- Dev DB name: `dev-sick-db`

## Nx Tasks

```bash
pnpm nx serve api        # Start dev server with watch
pnpm nx build api        # Production build
pnpm nx lint api
pnpm nx test api
```

## Related Libs

All business logic lives in `libs/api/` — see `AGENTS.md` in each lib for details:

- [libs/api/auth](../../libs/api/auth/AGENTS.md)
- [libs/api/user](../../libs/api/user/AGENTS.md)
- [libs/api/blog-post](../../libs/api/blog-post/AGENTS.md)
- [libs/api/project](../../libs/api/project/AGENTS.md)
- [libs/api/search](../../libs/api/search/AGENTS.md)

## Key Conventions

- All routes prefixed with `/api`
- `blog-post` and `project` write endpoints (`POST`, `PATCH`, `DELETE`) are protected with `JwtAuthGuard` + `AdminGuard`
- `ValidationPipe` with `whitelist: true` and `forbidNonWhitelisted: true` is applied globally
