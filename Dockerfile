# ┌─────────────────────────────────────────────────────┐
# │  Sick-Fansubs — Multi-stage Docker build            │
# │                                                     │
# │  Stage 1 (frontend): bun + Vite → dist/             │
# │  Stage 2 (backend):  Go + embed dist/ → binary      │
# │  Stage 3 (runtime):  Alpine + binary → serve        │
# └─────────────────────────────────────────────────────┘
#
# BUILD WITH --target prod! The DEFAULT target is the LAST stage — which is
# "dev" (hot-reload with air). A target-less `docker build` ships the dev
# image: toolchain-sized (~280M) and air writes /app/tmp, which fails when
# the container runs as non-root (v2 flight incident 2026-08-17).
# docker build --target prod --build-arg APP_VERSION=<bare-semver> -t sick-fansubs:<tag> .

# ── Stage 1: Build Lit frontend ──────────────────────
FROM oven/bun:1.4.2 AS frontend
WORKDIR /src
COPY web/package.json web/bun.lock ./
# Cache mounts (BuildKit): the bun store and the Go module/build caches
# persist across runs via the workflow's gha cache backend, so a deploy
# whose dependencies are unchanged compiles in seconds. A cache miss just
# rebuilds — the mounts are never a correctness dependency.
RUN --mount=type=cache,target=/root/.bun/install/cache bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# ── Stage 2: Build Go backend (embed frontend) ──────
FROM golang:1.27.1-alpine3.24 AS backend
# APP_VERSION must be the BARE semantic version (e.g. 2.0.0-alpha.1, NO "v"
# prefix): the footer renders "v" + this value, so a v-prefixed value
# displays doubled ("vv2.0.0-alpha.1"). The default "dev" is bare too.
ARG APP_VERSION=dev
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# Place the built frontend where Go's embed directive finds it.
# frontend.go uses //go:embed web/dist/* relative to cmd/api/.
COPY --from=frontend /src/dist/ cmd/api/web/dist/
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${APP_VERSION}" -o main ./cmd/api/

# ── Stage 3: Runtime ─────────────────────────────────
FROM alpine:3.24 AS prod
WORKDIR /app

# ca-certificates for any future HTTPS outbound calls.
# The app user is pinned to uid/gid 1000 so the compose user: "1000:1000"
# matches the image's ownership of /app/data — including
# the named-volume copy-up seed.
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 1000 app && adduser -S -u 1000 -G app app

COPY --from=backend /app/main /app/main

# SQLite data directory (mounted as a volume in production).
RUN mkdir -p /app/data && chown app:app /app/data && chmod 700 /app/data

EXPOSE 8080
USER app

# Override PORT and DATA_DIR as needed via docker-compose or -e flags.
ENV PORT=8080
ENV DATA_DIR=/app/data

CMD ["./main"]

# ── Development stage (hot-reload with Air) ──────────
FROM backend AS dev
RUN go install github.com/air-verse/air@latest
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]
