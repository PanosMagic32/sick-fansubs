# Packages covered by the Go gate: internal packages and the commands. The
# commands carry the tool tests (backup, restore, docslint), so the gate must
# include them.
GO_TEST_PKGS = ./internal/... ./cmd/...

# ── Help ─────────────────────────────────────────────────────

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── Local Development (no Docker) ──────────────────────────────────────────────────────

# dev-local migrates first so the strict application opener never fails on
# a fresh or pending-migration database. cmd/migrate takes a validated
# pre-migration backup when a non-empty history has pending migrations and
# rolls back on failure (docs/patterns/go/sqlite.md — migration-scoped
# rollback).
dev-local: migrate-local ## Start Go API + Lit dev server locally (Ctrl+C to stop both)
	@if ss -ltn 2>/dev/null | grep -qE ':3000[[:space:]]'; then \
		echo "Port 3000 is in use — a stale Vite dev server is running."; \
		echo "Kill it with: pkill -f vite   (then retry)"; \
		exit 1; \
	fi
	@if ss -ltn 2>/dev/null | grep -qE ':8080[[:space:]]'; then \
		echo "Port 8080 is in use — a stale Go API process is running."; \
		echo "Kill it with: pkill -f 'cmd/api'   (then retry)"; \
		exit 1; \
	fi
	@set -e; \
	trap 'kill $$API_PID $$WEB_PID 2>/dev/null || true' INT TERM EXIT; \
	(cd web && exec bun run dev) & \
	WEB_PID=$$!; \
	exec go run ./cmd/api/... & \
	API_PID=$$!; \
	wait $$API_PID

migrate-local: ## Apply pending SQLite migrations locally (no Docker) — backs up first when a non-empty history has pending migrations
	@go run ./cmd/migrate

restore-local: ## Restore an older backup UNIT via the fail-closed staged restore (docs/patterns/go/sqlite.md) — BACKUP=<path to the unit's .db artifact>; its .media-manifest.json + media tar must sit next to it, optional ACKNOWLEDGE_LOSS=1
	@test -n "$$BACKUP" || (echo "BACKUP is required (the .db artifact of a complete backup unit, e.g. data/backups/2026-09-02/sick-fansubs.db)"; exit 1)
	@if [ -n "$$ACKNOWLEDGE_LOSS" ]; then \
		go run ./cmd/restore -backup "$$BACKUP" -acknowledge-loss; \
	else \
		go run ./cmd/restore -backup "$$BACKUP"; \
	fi

backup-local: ## Create a validated, age-encrypted daily backup unit — DATA_DIR/BACKUP_DIR/AGE_RECIPIENT/APP_ENV via env; run while the app is stopped
	@go run ./cmd/backup

backup-status: ## Report the newest published backup folder and its age
	@go run ./cmd/backup -status

backup-docker: ## Run the compose backup service against the sick-fansubs stack (the same command the VPS cron uses)
	@docker compose --profile backup run --rm backup

down: ## Back up the sick-fansubs stack, then docker compose down (the VPS runs deploy/down.sh; a raw 'docker compose down' does NOT back up)
	@docker compose stop app
	@if ! docker compose --profile backup run --rm backup; then \
		echo "backup failed — leaving the stack up"; \
		docker compose up -d || true; \
		exit 1; \
	fi
	@docker compose down

rebuild-search-local: ## Rebuild the FTS5 search index from the content tables — also the post-restore/backfill step
	@go run ./cmd/rebuildsearch

reset-password-local: ## Reset a user's password + force a change on next sign-in (break-glass) — USERNAME=<username>
	@test -n "$$USERNAME" || (echo "USERNAME is required (the account to reset)"; exit 1)
	@go run ./cmd/resetpassword -username "$$USERNAME"

sweep-media-local: ## Remove orphaned media files not referenced by any content row (24h grace window)
	@go run ./cmd/sweepmedia

import-data-local: ## Import legacy blog data locally (migration-pilot source) — IMPORT_FILE=<path to JSON array or JSON-lines>
	@test -n "$$IMPORT_FILE" || (echo "IMPORT_FILE is required (legacy export file)"; exit 1)
	@go run ./cmd/importdata

import-projects-local: ## Import legacy project data locally — IMPORT_FILE=<path to JSON array or JSON-lines>
	@test -n "$$IMPORT_FILE" || (echo "IMPORT_FILE is required (legacy export file)"; exit 1)
	@go run ./cmd/importdata -kind projects

import-users-local: ## Import legacy user data locally (credential migration — run BEFORE content imports) — IMPORT_FILE=<path to JSON array or JSON-lines>
	@test -n "$$IMPORT_FILE" || (echo "IMPORT_FILE is required (legacy export file)"; exit 1)
	@go run ./cmd/importdata -kind users

import-favorites-local: ## Import legacy favorites locally (users export — run AFTER users + content imports) — IMPORT_FILE=<path to JSON array or JSON-lines>
	@test -n "$$IMPORT_FILE" || (echo "IMPORT_FILE is required (legacy users export file)"; exit 1)
	@go run ./cmd/importdata -kind favorites

import-media-local: ## Import legacy blog + project thumbnails locally — MEDIA_SOURCE_DIR=<dir with downloaded legacy objects> (default ./media-source)
	@MEDIA_SOURCE_DIR="$${MEDIA_SOURCE_DIR:-./media-source}" go run ./cmd/importmedia

import-avatars-local: ## Import legacy user avatars locally — IMPORT_FILE=<users export>, MEDIA_SOURCE_DIR (default ./media-source)
	@test -n "$$IMPORT_FILE" || (echo "IMPORT_FILE is required (legacy users export file)"; exit 1)
	@MEDIA_SOURCE_DIR="$${MEDIA_SOURCE_DIR:-./media-source}" go run ./cmd/importavatars

fetch-media-local: ## Download legacy media from a legacy export into MEDIA_SOURCE_DIR (default ./media-source) — IMPORT_FILE=<path>, KIND=blog-posts|projects|users (default blog-posts)
	@test -n "$$IMPORT_FILE" || (echo "IMPORT_FILE is required (legacy export file)"; exit 1)
	@MEDIA_SOURCE_DIR="$${MEDIA_SOURCE_DIR:-./media-source}" go run ./cmd/fetchmedia -kind $${KIND:-blog-posts}

dev-api: ## Start Go API locally (port 8080)
	@go run ./cmd/api/...

dev-web: ## Start Lit dev server locally (proxies /api/v1 to Go)
	@cd web && bun run dev

dev-watch: ## Start Go with auto-reload (requires 'air')
	@if command -v air > /dev/null; then \
		air -c .air.toml; \
	else \
		printf "Go 'air' is not installed. Install it? [Y/n] "; \
		read choice; \
		if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
			go install github.com/air-verse/air@latest; \
			air -c .air.toml; \
		else \
			echo "Exiting."; \
			exit 1; \
		fi; \
	fi

watch: dev-watch ## Alias for dev-watch

# ── VPS flight images (manual alpha-method deploy; the version-tag pipeline is the normal path) ──

docker-build-prod: ## Build the production image for a VPS flight — APP_VERSION=<bare semver, NO "v" prefix>
	@test -n "$$APP_VERSION" || (echo "APP_VERSION is required (bare semver, e.g. 2.0.0-alpha.2)"; exit 1)
	@DOCKER_BUILDKIT=1 docker build --target prod --build-arg APP_VERSION=$$APP_VERSION -t sick-fansubs:$$APP_VERSION .

docker-build-migrate: ## Build the migration tool image for a VPS flight
	@DOCKER_BUILDKIT=1 docker build -f Dockerfile.migrate -t sick-fansubs-migrate:latest .

docker-build-rebuildsearch: ## Build the search-index rebuild image for a VPS flight
	@docker build -f Dockerfile.rebuildsearch -t sick-fansubs-rebuildsearch:latest .

docker-build-importdata: ## Build the legacy-import image for a VPS flight (one-off docker run, mirrors Dockerfile.migrate)
	@docker build -f Dockerfile.importdata -t sick-fansubs-importdata:latest .

docker-build-importmedia: ## Build the legacy media-import image for a VPS flight (one-off docker run, mirrors Dockerfile.importdata)
	@docker build -f Dockerfile.importmedia -t sick-fansubs-importmedia:latest .

docker-build-importavatars: ## Build the legacy avatar-import image for a VPS flight (one-off docker run, mirrors Dockerfile.importmedia)
	@docker build -f Dockerfile.importavatars -t sick-fansubs-importavatars:latest .

docker-build-backup: ## Build the backup-command image (the compose backup service)
	@DOCKER_BUILDKIT=1 docker build -f Dockerfile.backup -t sick-fansubs-backup:latest .

docker-build-restore: ## Build the restore-command image (one-off docker run, docs/patterns/go/sqlite.md)
	@DOCKER_BUILDKIT=1 docker build -f Dockerfile.restore -t sick-fansubs-restore:latest .

# ── Go: Build ────────────────────────────────────────────────

go-build: ## Build Go binary (pure Go, no CGO) — run web-build first
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o main ./cmd/api/...

go-build-dev: ## Build Go binary with race detector for local testing
	@CGO_ENABLED=1 go build -race -o main ./cmd/api/...

build: go-build ## Alias for go-build

build-all: web-build go-build ## Build frontend + Go binary (production artifact)

# ── Go: Test ─────────────────────────────────────────────────

go-test: ## Run Go unit tests
	@go test $(GO_TEST_PKGS) -count=1

go-test-verbose: ## Run Go unit tests with verbose output
	@go test $(GO_TEST_PKGS) -count=1 -v

go-test-all: ## Run ALL Go tests (the race-detector subset included)
	@go test ./... -count=1 -v

go-test-race: ## Run Go unit tests with race detector
	@go test $(GO_TEST_PKGS) -count=1 -race

go-test-coverage: ## Run Go unit tests with coverage report
	@go test $(GO_TEST_PKGS) -count=1 -coverprofile=coverage.out
	@go tool cover -func=coverage.out
	@echo ""
	@echo "HTML report: go tool cover -html=coverage.out"

test: go-test ## Alias for go-test

# ── Go: Lint & Format ───────────────────────────────────────

go-vet: ## Run go vet (static analysis)
	@go vet $(GO_TEST_PKGS)

go-fix: ## Apply Go modernizations (go fix) — run before every commit (root AGENTS.md rule 13)
	@go fix $(GO_TEST_PKGS)

go-fmt: ## Format Go source files
	@go fmt $(GO_TEST_PKGS)

go-fmt-check: ## Check if Go files are formatted (for CI — exits non-zero on diff)
	@test -z "$$(gofmt -l cmd internal)" || (echo "The following files need formatting:"; gofmt -l cmd internal; exit 1)

go-lint: go-vet go-fmt-check ## Run all Go lint/format checks

deps-report: ## Report dependency drift (Go modules + web packages) — report only
	@go list -m -u all
	@cd web && bun outdated || true

# ── Frontend: Build ──────────────────────────────────────────

web-install: ## Install frontend dependencies
	@cd web && bun install

web-build: ## Build Lit production bundle to web/dist/
	@cd web && bun run build
	@mkdir -p cmd/api/web/dist
	# Prune the staging dir first: `vite` does not empty an outDir outside its
	# root, so stale bundles from an earlier build survive here and reach local
	# builds' embedded assets. The tracked .gitkeep stays.
	@find cmd/api/web/dist -mindepth 1 -maxdepth 1 ! -name .gitkeep -exec rm -rf {} +
	@cp -r web/dist/* cmd/api/web/dist/
	@echo "Frontend built and staged for Go embed at cmd/api/web/dist/"

web-typecheck: ## TypeScript type checking (no emit)
	@cd web && bunx tsc --noEmit

web-test: ## Run frontend unit tests (bun test)
	@cd web && bun test

# ── Frontend: Lint & Format ──────────────────────────────────
#
# Frontend formatting is handled by the IDE —
# there is deliberately no Prettier target here.

web-lint: web-typecheck ## Run all frontend lint checks (typecheck; formatting is IDE-handled)

# ── Docs ─────────────────────────────────────────────────────
#
# docs-lint enforces the documentation structure (docs/patterns/docs-conventions.md):
# every code scope has an AGENTS.md, every pattern/ops doc is linked from the
# index, relative links resolve, and no living file cites a retired decision
# record (checksummed migrations are the one recorded exemption).

docs-lint: ## Check the documentation structure and the decision-citation check
	@go run ./cmd/docslint

# ── Workflows ────────────────────────────────────────────────
#
# workflow-lint runs actionlint over .github/workflows. GitHub silently drops
# a workflow file its validator rejects — no sidebar entry, no runs — so an
# invalid file must fail the gates instead. Shellcheck is pinned off: CI
# images ship it and dev machines may not, and the gate must agree everywhere.

workflow-lint: ## Check the GitHub Actions workflows (globs, expressions, schema)
	@go tool actionlint -shellcheck=

# ── Combined Targets ─────────────────────────────────────────

fmt: go-fmt ## Format all code (Go only; frontend formatting is IDE-handled)

lint: go-lint web-lint ## Run all lint checks (Go + frontend)

check: go-vet go-fmt-check docs-lint workflow-lint web-typecheck web-test go-test ## Full CI check (lint + test)

check-all: go-vet go-fmt-check docs-lint workflow-lint web-typecheck web-test go-test-all ## Full CI check including integration tests

# ── Deploy ────────────────────────────────────────────────────────
#
# Deploy runs through GitHub Actions: .github/workflows/deploy.yml builds and
# pushes the images on a version tag, then SSHes to the VPS and runs
# deploy/deploy.sh. There is deliberately no local deploy target; the local
# Docker targets above are the offline fallbacks.

# ── Quality (legacy aliases, kept for compatibility) ─────────

itest: ## Run database tests
	@go test ./internal/database -v

clean: ## Remove build artifacts
	@rm -f main
	@rm -f sick-fansubs.db
	@rm -f coverage.out

# ── Phony Targets ────────────────────────────────────────────

.PHONY: help \
        dev-local dev-api dev-web dev-watch watch \
        migrate-local \
        restore-local \
	        backup-local backup-status backup-docker down \
	        docker-build-prod docker-build-migrate docker-build-rebuildsearch docker-build-importdata docker-build-importmedia docker-build-importavatars docker-build-backup docker-build-restore \
	        rebuild-search-local sweep-media-local reset-password-local \
	        import-data-local import-projects-local import-users-local import-favorites-local import-media-local import-avatars-local fetch-media-local \
        go-build go-build-dev build \
        go-test go-test-verbose go-test-all go-test-race go-test-coverage test \
        go-vet go-fmt go-fmt-check go-lint go-fix \
        deps-report \
	        web-install web-build web-typecheck web-test web-lint \
	        docs-lint workflow-lint \
        fmt lint check check-all \
        itest clean
