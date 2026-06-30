# CLAUDE.md

`qeet-logs` is the **Qeet Logs** platform — privacy-first, multi-tenant, identity-aware log management (PRD Phase 1 / MVP in progress).

PRD: [../qeet-files/qeet-logs/Product_Requirement_Document.md](../qeet-files/qeet-logs/Product_Requirement_Document.md) (v2.0)
TAD: [../qeet-files/qeet-logs/Technical_Architecture_Document.md](../qeet-files/qeet-logs/Technical_Architecture_Document.md) (v1.0)

## Quick commands

```bash
nvm use            # Node 24 (.nvmrc) — for apps/console
cp .env.example .env

make infra-up      # ClickHouse, Postgres, NATS, Redis, MinIO via Docker
make migrate-up    # Apply Postgres metadata migrations
make ch-migrate    # Apply ClickHouse DDL (clickhouse/migrations/*.sql) — M1+

make dev           # Run query API (cmd/query) on :8100
make dev-ingest    # Run Rust ingest gateway (needs Rust toolchain) — M2
make dev-console   # Next.js console on :3020 — M7

make build         # Build all Go binaries to bin/
make test          # Go unit tests (go vet + go test -race)
make test-integration  # Integration tests (needs running infra)
make lint fmt      # golangci-lint / go fmt
```

> **Rust toolchain required for the ingest service (M2+).** Not yet installed on this machine —
> install via `rustup` before `make dev-ingest`.

## Architecture (TAD)

Polyglot: **Rust** for the hot ingest path, **Go** for query/API/alerting, **ClickHouse** for log
storage, **Postgres** for metadata, **NATS JetStream** as the ingestion bus, **Redis** for live-tail
fan-out + rate limits, **Next.js + @qeetrix/ui** console.

```
cmd/
  query/      → qeet-logs query API (:8100) — REST query, live-tail WS, admin (M3+)
  alerter/    → threshold + absence rule engine (M6)
  migrate/    → golang-migrate runner (Postgres)
  seed/       → demo tenant + API key + sample logs (M0/M1)

ingest/                   Rust Cargo workspace (M2)
  gateway/                HTTP/OTLP receiver → PII gate → NATS
  writer/                 NATS → ClickHouse batch insert + Redis tail fan-out
  core/                   shared LogRecord, PII detectors, ULID, normalisation

domains/                  Go business logic (bounded contexts) — added per milestone
  query/                  LogQL++ lexer/parser/AST → ClickHouse SQL compiler (M3)
  alerting/               rule engine + delivery (M6)
  tenancy/  retention/    tenant resolution / per-tenant TTL (M1/M5)

platform/                 Shared infrastructure (no business logic)
  api/handler/            chi route handlers (health, query, admin, apikeys)
  api/middleware/         apikey→tenant, OIDC, rate-limit, scope guard (M2/M5)
  clickhouse/             ClickHouse HTTP client (Ping/Exec; query path M3)
  database/               pgxpool + LookupAPIKey + WithTenant RLS helper
  cache/ messaging/ config/ observability/

migrations/               Postgres golang-migrate pairs (NNNN_*.up/down.sql, immutable)
clickhouse/migrations/    ClickHouse DDL (logs table, TTL, auth_events) — M1
apps/console/             Next.js 16 + @qeetrix/ui (:3020) — M7
sdk/go/                   Public Go SDK — M8
api/openapi/              OpenAPI 3.1 spec — M9
deploy/                   docker-compose + Caddyfile (+ Helm) — M9
```

## Key conventions

- **Tenant isolation**: `tenant_id` is resolved from `X-Qeet-Api-Key` → SHA-256 → `api_keys` lookup
  (or from the Qeet ID JWT for console). The query layer **always** injects the `tenant_id` predicate
  from identity — **never from user input** (TAD §7.2). Postgres domain tables use RLS via
  `current_setting('app.tenant_id')` (set by `database.WithTenant`).
- **RBAC scopes**: `logs:{ingest,read,query,export,admin,platform}`. `logs:platform` = cross-tenant
  (QEET operators only).
- **NATS subjects**: `qeet-logs.{tenant_id}.logs` (ingestion). Redis live-tail channel:
  `tail.{tenant_id}.{service}`.
- **PII gate is synchronous, pre-storage** — PII never reaches ClickHouse. Phase-0 = regex detectors;
  ML detection is Phase 2.
- **Migrations**: sequential integers, immutable once applied. `make migrate-up` (Postgres) and
  `make ch-migrate` (ClickHouse) after pull.
- **Domain**: `logs.qeet.in` (app), `api.logs.qeet.in` (query), `ingest.logs.qeet.in` (ingest).
  Cookies scoped to `.logs.qeet.in` — never the parent `.qeet.in` zone.

## Infrastructure ports (local dev)

| Service | Port |
|---|---|
| query API (cmd/query) | 8100 |
| ingest gateway (Rust) | 8101 + 4318 (OTLP) |
| Next.js console | 3020 |
| ClickHouse | 8123 (HTTP) / 9100 (native) |
| PostgreSQL 17 | 5434 |
| NATS | 4223 / 8223 (monitor) |
| Redis 7 | 6380 |
| MinIO (cold tier, Phase 2) | 9002 / 9003 |

## Build status / roadmap

Building the PRD's **Phase 1 (MVP)** in milestones M0–M9 (see `~/.claude/plans/` plan). M0 = repo
scaffold + local infra + health/readiness. Phase 2 (PII ML, Qeet ID auth-event stream, GDPR erasure)
and Phase 3 (anomaly detection, NLQ, self-hosted) are explicitly out of scope for now.
