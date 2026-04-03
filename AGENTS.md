<!-- nx configuration start-->
<!-- Leave the start & end comments to automatically receive updates. -->

# General Guidelines for working with Nx

- For navigating/exploring the workspace, invoke the `nx-workspace` skill first - it has patterns for querying projects, targets, and dependencies
- When running tasks (for example build, lint, test, e2e, etc.), always prefer running the task through `nx` (i.e. `nx run`, `nx run-many`, `nx affected`) instead of using the underlying tooling directly
- Prefix nx commands with the workspace's package manager (e.g., `pnpm nx build`, `npm exec nx test`) - avoids using globally installed CLI
- You have access to the Nx MCP server and its tools, use them to help the user
- For Nx plugin best practices, check `node_modules/@nx/<plugin>/PLUGIN.md`. Not all plugins have this file - proceed without it if unavailable.
- NEVER guess CLI flags - always check nx_docs or `--help` first when unsure

## Scaffolding & Generators

- For scaffolding tasks (creating apps, libs, project structure, setup), ALWAYS invoke the `nx-generate` skill FIRST before exploring or calling MCP tools

## When to use nx_docs

- USE for: advanced config options, unfamiliar flags, migration guides, plugin configuration, edge cases
- DON'T USE for: basic generator syntax (`nx g @nx/react:app`), standard commands, things you already know
- The `nx-generate` skill handles generator discovery internally - don't call nx_docs just to look up generator syntax

<!-- nx configuration end-->

## Workspace Map

This monorepo is a **Greek fansub group's website** — Angular 21 frontend + NestJS 11 backend, MongoDB, fully Dockerized.

### Apps

| App | Path        | Stack                             | AGENTS.md                                |
| --- | ----------- | --------------------------------- | ---------------------------------------- |
| API | `apps/api/` | NestJS + Mongoose                 | [apps/api/AGENTS.md](apps/api/AGENTS.md) |
| Web | `apps/web/` | Angular 21 (zoneless, standalone) | [apps/web/AGENTS.md](apps/web/AGENTS.md) |

### Backend Libs (`libs/api/`)

| Lib       | Path alias       | Domain                                 | AGENTS.md                                                    |
| --------- | ---------------- | -------------------------------------- | ------------------------------------------------------------ |
| auth      | `@api/auth`      | JWT login, Passport strategies, guards | [libs/api/auth/AGENTS.md](libs/api/auth/AGENTS.md)           |
| user      | `@api/user`      | User accounts, RBAC, password hashing  | [libs/api/user/AGENTS.md](libs/api/user/AGENTS.md)           |
| blog-post | `@api/blog-post` | Fansub releases / blog posts (CRUD)    | [libs/api/blog-post/AGENTS.md](libs/api/blog-post/AGENTS.md) |
| project   | `@api/project`   | Fansub projects / series (CRUD)        | [libs/api/project/AGENTS.md](libs/api/project/AGENTS.md)     |
| search    | `@api/search`    | Cross-entity full-text search          | [libs/api/search/AGENTS.md](libs/api/search/AGENTS.md)       |

### Frontend Libs (`libs/web/`)

| Lib       | Path alias       | Domain                                                        | AGENTS.md                                                    |
| --------- | ---------------- | ------------------------------------------------------------- | ------------------------------------------------------------ |
| shared    | `@web/shared`    | Shell layout, JWT interceptor, TokenService, WebConfigService | [libs/web/shared/AGENTS.md](libs/web/shared/AGENTS.md)       |
| auth      | `@web/auth`      | Login/signup UI + three route guards                          | [libs/web/auth/AGENTS.md](libs/web/auth/AGENTS.md)           |
| blog-post | `@web/blog-post` | Blog post feed (root route), create/edit                      | [libs/web/blog-post/AGENTS.md](libs/web/blog-post/AGENTS.md) |
| projects  | `@web/projects`  | Fansub projects list, details, create/edit                    | [libs/web/projects/AGENTS.md](libs/web/projects/AGENTS.md)   |
| search    | `@web/search`    | Full-text search page                                         | [libs/web/search/AGENTS.md](libs/web/search/AGENTS.md)       |
| about     | `@web/about`     | About page + API health check                                 | [libs/web/about/AGENTS.md](libs/web/about/AGENTS.md)         |
| account   | `@web/account`   | Authenticated profile management + favorites                  | [libs/web/account/AGENTS.md](libs/web/account/AGENTS.md)     |

### Shared Contracts

| Lib   | Path alias      | Domain                                                                        |
| ----- | --------------- | ----------------------------------------------------------------------------- |
| types | `@shared/types` | Shared TS contracts (`BlogPost`, `Project`, `Searchable`, `SearchTypeOption`) |

### Key Patterns

- **API**: Controller → Service → Mongoose Schema. All routes under `/api`. Write endpoints for `blog-post` and `project` are guarded by `JwtAuthGuard` + `AdminGuard`.
- **Web**: All feature libs use strict `data-access / feature / ui` layering. Signals everywhere (`httpResource`, `signal()`, `toSignal()`).
- **Styling**: Shared design tokens and responsive helpers live in `libs/web/shared/src/lib/styles` and are consumed via SCSS include paths.
- **Auth flow**: Angular `authGuard` / `requireAuthGuard` rely on role-aware `TokenService` session signals (`role`, `status`) → `JwtAuthGuard` on protected API routes.
- **UI language**: All user-facing strings are in **Greek**.
