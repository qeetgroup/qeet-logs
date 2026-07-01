MODULE     := github.com/qeetgroup/qeet-logs
GO         := go
GOFLAGS    ?=
BUILD_DIR  := bin

# DB / migrate
DB_URL     ?= postgres://qeet-logs:qeet-logs@localhost:5434/qeet-logs?sslmode=disable
MIGRATIONS := migrations
CH_DIR     := clickhouse/migrations

# Build info
GIT_SHA    := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X main.version=$(GIT_SHA) -X main.buildTime=$(BUILD_TIME)

.PHONY: help install build dev dev-ingest dev-console dev-alerter test test-integration \
        lint fmt vet infra-up infra-down db-reset db-psql \
        migrate-up migrate-down migrate-version ch-migrate seed clean kill

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

install: ## Install Go deps + console JS deps
	$(GO) mod tidy
	@if [ -d apps/console ]; then cd apps/console && npm install; fi

# ── Build ────────────────────────────────────────────────────────────────────

build: ## Build all Go binaries to bin/
	mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/ ./cmd/...

# ── Dev ──────────────────────────────────────────────────────────────────────

dev: ## Run the query API (cmd/query) on :8100
	DATABASE_URL="$(DB_URL)" $(GO) run ./cmd/query/

dev-ingest: ## Run the Rust ingest gateway (requires Rust toolchain)
	cd ingest && cargo run --bin qeet-logs-gateway

dev-alerter: ## Run the alerter engine (cmd/alerter)
	DATABASE_URL="$(DB_URL)" $(GO) run ./cmd/alerter/

dev-console: ## Start the TanStack Start console on :3020
	cd apps/console && npm run dev

# ── Test ─────────────────────────────────────────────────────────────────────

test: vet ## Run all Go unit tests
	$(GO) test -race -count=1 ./...

test-integration: ## Run integration tests (requires running infra)
	$(GO) test -race -count=1 -tags integration ./...

# ── Quality ──────────────────────────────────────────────────────────────────

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format Go code
	$(GO) fmt ./...

# ── Infra ────────────────────────────────────────────────────────────────────

infra-up: ## Start local infra (ClickHouse, Postgres, NATS, Redis, MinIO)
	docker compose up -d

infra-down: ## Stop local infra
	docker compose down

db-reset: ## Drop + recreate the metadata DB, then migrate
	docker compose exec -T postgres psql -U qeet-logs -d postgres -c "DROP DATABASE IF EXISTS \"qeet-logs\";"
	docker compose exec -T postgres psql -U qeet-logs -d postgres -c "CREATE DATABASE \"qeet-logs\";"
	$(MAKE) migrate-up

db-psql: ## Open a psql shell on the metadata DB
	docker compose exec postgres psql -U qeet-logs -d qeet-logs

# ── Migrations ───────────────────────────────────────────────────────────────

migrate-up: ## Apply all pending Postgres migrations
	$(GO) run ./cmd/migrate/ -url "$(DB_URL)" -dir "$(MIGRATIONS)" up

migrate-down: ## Roll back last Postgres migration (n=N to roll back N)
	$(GO) run ./cmd/migrate/ -url "$(DB_URL)" -dir "$(MIGRATIONS)" down $(or $(n),1)

migrate-version: ## Print current Postgres migration version
	$(GO) run ./cmd/migrate/ -url "$(DB_URL)" -dir "$(MIGRATIONS)" version

ch-migrate: ## Apply ClickHouse DDL migrations (clickhouse/migrations/*.sql)
	@for f in $(CH_DIR)/*.sql; do \
		[ -e "$$f" ] || continue; \
		echo "applying $$f"; \
		docker compose exec -T clickhouse clickhouse-client --multiquery < "$$f"; \
	done

# ── Seed / Utility ───────────────────────────────────────────────────────────

seed: ## Seed demo tenant + API key + sample logs
	DATABASE_URL="$(DB_URL)" $(GO) run ./cmd/seed/

kill: ## Kill any process listening on :8100
	-lsof -ti:8100 | xargs kill -9 2>/dev/null || true

clean: ## Remove build artefacts
	rm -rf $(BUILD_DIR)
