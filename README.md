# Qeet Logs

> Structured, privacy-first, identity-aware log management built for the multi-tenant era.

**Qeet Logs** is an observability platform (logs + metrics + traces) for teams that take privacy
seriously: multi-tenant and **identity-aware** by design, with PII masked *at the gate* (before
storage), true hard-delete retention, and native auth-event signals from
[Qeet ID](https://id.qeet.in). Part of the [Qeet Group](../) product suite.

Phase 1 (the logs + metrics + traces MVP) is **feature-complete** and Phase 2 (RCA intelligence,
business-context correlation, incident/war-room, cost/compliance surfaces) is **substantially built**.
See the reconciliation registers linked under [Status](#status) and the ground-truth
[AS-BUILT-NOTES](../qeet-files/qeet-logs/research/AS-BUILT-NOTES.md).

## Why

1. **Retention is unaffordable at volume** — columnar storage on ClickHouse targets ~$0.05–0.15/GB
   retained vs. incumbents' $0.10–0.30+, with a hot→cold S3/MinIO tier for aged partitions.
2. **PII becomes a compliance landmine** — a synchronous PII gate masks/hashes/drops sensitive data
   *before* it is ever stored. PII never reaches ClickHouse.
3. **Identity events are never first-class** — auth events from Qeet ID are ingested as typed,
   queryable signals, not buried in free text.

## Architecture

Polyglot by design — each layer uses the best-fit stack:

| Layer | Technology | Where |
|---|---|---|
| Ingest — gateway + writer (hot path) | **Rust** (tokio, axum, prost/OTLP, ClickHouse HTTP) | `ingest/` |
| Query / API + alerting + lifecycle | **Go 1.25** + chi v5 | `cmd/`, `domains/`, `platform/` |
| Log / metric / trace storage | **ClickHouse 25+** (columnar, per-tenant TTL, hot→cold S3 tier) | `clickhouse/migrations/` |
| Metadata (tenants, keys, rules, incidents…) | **PostgreSQL 17** + Row-Level Security | `migrations/` |
| Ingestion bus | **NATS JetStream** (`qeet-logs.{tenant}.{logs\|metrics\|traces}`) | `platform/messaging/` |
| Cache / live-tail fan-out / rate limits | **Redis 7** | `platform/cache/` |
| Cold tier / archive object store | **S3 / MinIO** | `clickhouse/config/storage.xml` |
| Console | **TanStack Start + React 19 + @qeetrix/ui** | `qeet-consoles/qeet-logs-console` (own repo) |
| Auth | **Qeet ID** OIDC (relying party) + scoped `X-Qeet-Api-Key` | `platform/api/middleware/` |

Process topology:

```
                 SDKs / OTel collector / Prometheus remote_write
                                   │
                  ┌────────────────▼─────────────────┐
   ingest/gateway │ Rust — HTTP/OTLP receiver         │  :8101  (+ :4318 OTLP)
                  │ PII gate → transform/remap → NATS │
                  └────────────────┬─────────────────┘
                                   │ NATS JetStream (qeet-logs.>)
                  ┌────────────────▼─────────────────┐
   ingest/writer  │ Rust — signal-aware batch insert  │
                  │ → ClickHouse + Redis tail fan-out │
                  └────────────────┬─────────────────┘
                                   ▼
   ClickHouse (logs/metrics/traces/auth_events/change_events) ── hot→cold ──▶ S3/MinIO
                                   ▲
   cmd/query   Go — REST query (LogQL++), live-tail WS, PromQL + Loki surfaces, admin  :8100
   cmd/alerter Go — threshold/absence/anomaly rules, correlation, incidents, delivery
   cmd/lifecycle Go — per-tenant hot→cold partition mover (ClickHouse tiering)
   cmd/mcp     Go — stdio MCP server (query/incidents/rca/topology/deploy tools)
                                   ▲
   Postgres (RLS: tenants, api_keys, alert_rules, incidents, dashboards, …)
```

Bounded contexts live under `domains/` (query engine, alerting, anomaly, rca, deploy, topology,
timeline, retention/lifecycle, buscontext, postmortem, warroom, chatops, notify, aigateway,
forecast, billing, changesource, webhook, grafana, routingsim, ttfiq). Shared infrastructure with no
business logic lives under `platform/`.

## Quick start

Requires Docker (infra), Go 1.25, and — for the ingest hot path — a Rust toolchain (`rustup`) and
[bun](https://bun.sh) for the console.

```bash
cp .env.example .env
make infra-up      # ClickHouse, Postgres 17, NATS JetStream, Redis 7, MinIO
make migrate-up    # Postgres metadata schema (golang-migrate, 21 migrations)
make ch-migrate    # ClickHouse DDL (logs/metrics/traces/auth_events/change_events, 9 migrations)
make dev           # query API on http://localhost:8100

curl localhost:8100/healthz   # liveness
curl localhost:8100/readyz    # readiness (checks Postgres + Redis + ClickHouse + NATS)
```

Other long-running processes:

```bash
make dev-ingest    # Rust ingest gateway on :8101 (+ :4318 OTLP) — needs rustup
make dev-alerter   # alert/incident engine (cmd/alerter)
# console: now its own repo → qeet-consoles/qeet-logs-console (bun run dev on :3020)
# cmd/lifecycle (cold-tier mover) and cmd/mcp (MCP server) are run directly via `go run ./cmd/...`
```

> **Note:** the `make seed` target references `cmd/seed`, which is **not yet present** in the tree.
> Ingest sample data via the SDKs, the `ql` CLI (`cmd/ql`), or a `POST /v1/ingest` against the
> gateway instead. See [Status](#status).

See [CLAUDE.md](CLAUDE.md) for the full command list, architecture, and conventions.

## API surface

Two hosts: the **ingest gateway** (`ingest.logs.qeet.in` / `:8101`) and the **query API**
(`api.logs.qeet.in` / `:8100`). Full OpenAPI 3.1 contracts (split by bounded context) live in
[`api/openapi/`](api/openapi/README.md); a Postman collection is in `api/postman/`.

### Ingest gateway (Rust, `:8101` + `:4318` OTLP) — no Qeet API key, `X-Qeet-Api-Key` resolves tenant

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/ingest` | Single log event (flat `message`) |
| POST | `/v1/ingest/batch` | NDJSON batch |
| POST | `/v1/ingest/gelf` | GELF |
| POST | `/v1/ingest/syslog` | RFC 5424 syslog |
| POST | `/v1/logs` | OTLP/HTTP logs (JSON + protobuf) |
| POST | `/v1/metrics` | OTLP/HTTP metrics |
| POST | `/v1/traces` | OTLP/HTTP traces |
| POST | `/api/v1/write` | Prometheus `remote_write` |
| GET | `/healthz`, `/readyz` | Probes |

### Query API — `/v1` (auth: `X-Qeet-Api-Key` → tenant + scopes)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/query` | LogQL++ query (logs/metrics/traces/auth_events/change_events) |
| GET | `/v1/query/tail` | Live tail (**WebSocket**) |
| GET | `/v1/auth-events` | Typed Qeet ID auth-event signals |
| POST/GET | `/v1/changes` | Change-event ingestion + listing (Module 15.1) |
| POST | `/v1/changes/{provider}` | GitHub/GitLab/LaunchDarkly webhook connectors |
| GET | `/v1/topology` | Service dependency graph + blast radius |
| GET | `/v1/timeline` | Unified cross-signal investigation timeline |
| GET | `/v1/incidents` | Correlated incidents + low-severity feed |
| GET | `/v1/rca` | RCA structural retrieval (deploy + dependency candidates) |
| GET | `/v1/deploy/culprits` | Ranked deploy-culprit scoring + rollback suggestion |
| GET | `/v1/business-context`, `/v1/incidents/{id}/context` | Exposure tags |
| GET | `/v1/export` | Programmatic export |
| POST | `/v1/alerts/simulate` | Alert-routing simulation |
| GET | `/v1/analytics/ttfiq` | Time-to-first-insight-query metric |
| GET | `/v1/forecast` | Statistical capacity/exhaustion forecast |
| POST/GET | `/v1/query/copilot`, `/v1/query/copilot/conversations…` | Governed LLM copilot (single-shot + multi-turn) |
| GET | `/v1/overlays` | Correlation-aware panel overlays |
| POST | `/v1/query/nl` | NL → LogQL++ translation |

### Prometheus-compatible — `/api/v1` (point a Grafana Prometheus data source here)

`GET|POST /api/v1/query` · `GET|POST /api/v1/query_range`

### Grafana Loki-compatible — `/loki/api/v1` (point a Grafana Loki data source here)

`/query_range` · `/labels` · `/label/{name}/values` · `/series` · `/status/buildinfo`

### ChatOps — `/v1/chatops/slack/*` (Slack-signed, **not** API-key auth)

`/install` · `/callback` (OAuth v2) · `/commands` (signed slash-commands) · `/interactivity`.
Returns `501` until the Slack app secrets are configured.

### Admin — `/v1/admin` (auth: API key with `logs:admin` **or** a Qeet ID Bearer JWT)

API keys · alert rules · retention (+ cost/what-if) · AI features opt-in · notify-locale · plan +
billing preview · RCA feedback · outbound webhooks · incident feedback + war-room
(declare/session/entries/roles/handoff) · business-context CRUD · postmortems + commitments +
CERT-In export · transform programs · audit log · dashboards (+ share) · saved searches · DLQ
(list/replay/drop) · quota usage · DPDP/GDPR erasure.

### Public (no auth)

`GET /healthz` · `GET /readyz` · `GET /version` · `GET /shared/dashboards/{token}` (seat-free
shared-dashboard read).

## RBAC scopes

`logs:{ingest,read,query,export,admin,platform}`. `logs:platform` = cross-tenant (Qeet operators
only), absent by default from every tenant key. The query layer **always** injects the `tenant_id`
predicate from the resolved identity — never from user input (TAD §7.2). Postgres domain tables
enforce Row-Level Security via `current_setting('app.tenant_id')`.

## Ports (local dev)

| Port | Service |
|---|---|
| 8100 | query API (`cmd/query`) |
| 8101 (+ 4318 OTLP) | ingest gateway (Rust) |
| 3020 | TanStack Start console |
| 8123 / 9100 | ClickHouse HTTP / native |
| 5434 | PostgreSQL 17 |
| 4223 / 8223 | NATS client / monitor |
| 6380 | Redis 7 |
| 9002 / 9003 | MinIO S3 API / console (cold tier) |

## SDKs

Client SDKs live in the workspace-level [`qeet-sdks/`](../qeet-sdks/) repos, one per language — never
inside this repo:

- `qeet-sdks/qeet-logs-go` — buffered ingester, env auto-enrichment, panic auto-capture
- `qeet-sdks/qeet-logs-node` — same surface (ESM, built-in `fetch`)
- `qeet-sdks/qeet-logs-react` — read-only query/tail hooks

> The in-repo `sdk/{go,node,python,java}` tree is a **legacy duplicate**; the canonical SDKs are the
> `qeet-sdks/` repos. The `cmd/ql` CLI (`ql query|send|tail|incidents|topology|rca`) is the
> zero-dependency terminal/CI client.

## Status

- **Phase 1 (MVP)** — logs + metrics + traces platform, cross-signal investigation,
  correlation-first alerting, topology, multi-language SDKs. **Complete** (G1–G18). See
  [PHASE1-GAP-REGISTER.md](PHASE1-GAP-REGISTER.md).
- **Phase 2** — RCA intelligence, business-context correlation, incident/war-room collaboration,
  cost/compliance surfaces, Grafana/Loki + PromQL surfaces, MCP server, cold-tier lifecycle,
  governed LLM copilot, two-way ChatOps. **Substantially built**; genuinely-gated remainders
  (trained ONNX/L2R models, Qeet Pay charging, Teams inbound) are tracked in
  [PHASE2-GAP-REGISTER.md](PHASE2-GAP-REGISTER.md).
- **Phase 3** (self-hosted distribution, deeper ML tiers) — out of scope.

Full PRD (v2.0) + TAD (v1.0) and the single-source-of-truth as-built reconciliation live in
[`../qeet-files/qeet-logs/`](../qeet-files/qeet-logs/).

## Operations

Runbooks and product-readiness docs live under [`deploy/runbooks/`](deploy/runbooks/) and
[`docs/`](docs/):

- Runbooks: [incident-response](deploy/runbooks/incident-response.md) ·
  [on-call](deploy/runbooks/on-call.md) · [backup-restore](deploy/runbooks/backup-restore.md) ·
  [scaling](deploy/runbooks/scaling.md) · [migration-rollback](deploy/runbooks/migration-rollback.md) ·
  [disaster-recovery](deploy/runbooks/disaster-recovery.md)
- Docs: [SLO/SLI](docs/slo-sli.md) · [deployment-guide](docs/deployment-guide.md) ·
  [observability](docs/observability.md) · [data-privacy](docs/data-privacy.md)
- [SECURITY.md](SECURITY.md) · [deploy/SOC2-CONTROLS.md](deploy/SOC2-CONTROLS.md) ·
  Helm chart in [`deploy/helm/qeet-logs/`](deploy/helm/qeet-logs/)
