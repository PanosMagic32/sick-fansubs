# Sick-Fansubs

A fansub community website — blog posts, projects, search, and media management. Version 2.0.0
replaces the legacy application at <https://sickfansubs.com>: a Go server with a SQLite database and
an embedded Lit + Vite frontend. The cutover procedure is [docs/ops/cutover.md](docs/ops/cutover.md).

## Stack

| Layer          | Choice                                |
| -------------- | ------------------------------------- |
| Backend        | Go (standard library first)           |
| Frontend       | Lit + Vite, embedded in the Go binary |
| Database       | SQLite (modernc.org/sqlite, pure Go)  |
| Object storage | Local filesystem                      |
| Reverse proxy  | Caddy (TLS) in the Docker topology    |

## Current Status

Version 2.0.0 is the production release. The deployment contract, the backup and restore procedures,
and the cutover record live under [docs/ops/](docs/ops/). AI agents start at [`AGENTS.md`](AGENTS.md).

## Quick Start

```bash
# Local development — Go API (:8080) + Vite dev server (http://localhost:3000, proxies /api/v1)
make dev-local

# Go API only (port 8080, or set PORT env var)
make dev-api

# Vite dev server only (requires the Go API running for /api/v1)
make dev-web

# Apply pending SQLite schema migrations locally (offline step, cmd/migrate — backs up first when a non-empty history has pending migrations)
make migrate-local

# Build the production artifact: Lit build embedded in the Go binary (pure Go, no CGO)
make build-all

# Full check: vet, format, docs guard, workflow lint, typecheck, and both test suites
make check
```

Frontend tooling is [Bun](https://bun.sh) (`bun install`, `bun run dev`, `bun test`). See the [Makefile](Makefile) for all targets (`make help`).

## Documentation

[`docs/README.md`](docs/README.md) is the documentation index: the pattern docs under [`docs/patterns/`](docs/patterns/), the operations docs under [`docs/ops/`](docs/ops/), and the wire contract [`docs/api/openapi.yaml`](docs/api/openapi.yaml). Every code scope carries its own `AGENTS.md` beside the code; [`AGENTS.md`](AGENTS.md) at the root is the agent entry point.

## Environment

```bash
cp .env.example .env
```

The Go application uses these variables:

| Variable          | Default                       | Purpose                                                                                                                            |
| ----------------- | ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `PORT`            | `8080`                        | HTTP server port                                                                                                                   |
| `DATA_DIR`        | `./data`                      | SQLite database directory                                                                                                          |
| `PUBLIC_BASE_URL` | `http://localhost:3000`       | Base URL for absolute media/API URLs (dev default pairs with the Vite proxy — change both together)                                |
| `APP_ENV`         | `development`                 | `development` \| `production` — production enables Secure session cookies                                                          |
| `TRUSTED_ORIGIN`  | `http://localhost:3000` (dev) | Canonical browser origin for origin/CSRF checks; production has NO default and fails closed unless an explicit https origin is set |

Local development needs no Docker: `make dev-local` runs the Go API and the Vite dev server against `./data` (SQLite). The deployment contract — the container topology, Caddy, and the tag-driven pipeline — lives in [`docs/ops/deploy.md`](docs/ops/deploy.md).

## License

MIT — see [LICENSE](LICENSE) for details.
