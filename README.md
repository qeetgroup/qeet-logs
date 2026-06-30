# Qeet Logs

> Structured, privacy-first log management built for the multi-tenant era.

**Qeet Logs** is a log management platform for teams that take privacy seriously: multi-tenant and
**identity-aware** by design, with PII masked *at the gate* (before storage), true hard-delete
retention, and native auth-event signals from [Qeet ID](https://id.qeet.in). Part of the
[Qeet Group](../) product suite.

This repository is **pre-release**, building the PRD's Phase 1 (MVP). See
[`../qeet-files/qeet-logs/`](../qeet-files/qeet-logs/) for the full PRD (v2.0) and TAD (v1.0).

## Why

1. **Retention is unaffordable at volume** — columnar storage on ClickHouse targets ~$0.05–0.15/GB
   retained vs. incumbents' $0.10–0.30+.
2. **PII becomes a compliance landmine** — a synchronous PII gate masks/hashes/drops sensitive data
   *before* it is ever stored.
3. **Identity events are never first-class** — auth events from Qeet ID are ingested as typed,
   queryable signals, not buried in free text.

## Stack

| Layer | Technology |
|---|---|
| Ingest (gateway + writer) | **Rust** (tokio, axum, ClickHouse native) |
| Query / API / alerting | **Go 1.25** + chi v5 |
| Log storage | **ClickHouse 25+** (columnar, per-tenant TTL) |
| Metadata | **PostgreSQL 17** (RLS multi-tenancy) |
| Ingestion bus | **NATS JetStream** |
| Cache / live-tail / rate limits | **Redis 7** |
| Console | **Next.js 16 + React 19 + @qeetrix/ui** |
| Auth | **Qeet ID** OIDC + scoped API keys |

## Quick start

```bash
cp .env.example .env
make infra-up      # ClickHouse, Postgres, NATS, Redis, MinIO
make migrate-up    # Postgres metadata schema
make dev           # query API on http://localhost:8100

curl localhost:8100/healthz   # liveness
curl localhost:8100/readyz    # readiness (checks all backing services)
```

See [CLAUDE.md](CLAUDE.md) for the full command list, architecture, and conventions.

## Status

Phase 1 (MVP) is being built in milestones M0–M9: scaffold → ClickHouse storage → Rust ingest →
LogQL++ query engine → live tail → multi-tenancy/RBAC → alerting → console → Go SDK → deploy.
