# Runbook — Scaling

How each Qeet Logs tier scales, and what to do under load. Chart defaults are in
[`../helm/qeet-logs/values.yaml`](../helm/qeet-logs/values.yaml).

## Scaling model at a glance

| Tier | Scale type | Default | HPA | Notes |
|---|---|---|---|---|
| Query API (`cmd/query`) | Horizontal, stateless | 2 replicas | ✅ 2→10 @ 70% CPU | Add replicas freely |
| Ingest gateway (Rust) | Horizontal, stateless | 3 replicas | ✅ 3→20 @ 60% CPU | The write front door — scale first under write load |
| Ingest writer (Rust) | Horizontal, competing NATS consumers | (per deployment) | manual | More writers = more parallel ClickHouse batch inserts |
| Alerter (`cmd/alerter`) | **Singleton** | 1 replica | ❌ never | Scaling > 1 double-pages and races incident upserts |
| Lifecycle mover (`cmd/lifecycle`) | Singleton / scheduled | 1 | ❌ | Best-effort partition mover |
| ClickHouse | Vertical + shard/replica | operator | ❌ | Stateful — plan capacity, do not autoscale |
| PostgreSQL | Vertical + read replicas | operator | ❌ | Stateful — metadata, modest volume |
| NATS / Redis | Cluster (replicas) | operator | ❌ | Sized for throughput, not autoscaled per-request |

## Query layer (read path)

Stateless behind a Service; HPA scales on CPU (`query.autoscaling`, 2→10 @ 70%).

- **Latency breach, ClickHouse healthy** → raise `query.autoscaling.minReplicas` for a higher floor,
  or `maxReplicas` if pinned. Each replica is cheap (128Mi/100m requested).
- **Latency breach, ClickHouse hot** → the bottleneck is ClickHouse, not the query tier; adding
  query replicas will not help and may worsen it. Address ClickHouse (below) and check per-tenant
  cardinality (`METRIC_MAX_LABEL_CARDINALITY`) and query shape.
- Live-tail load lands on **Redis** (fan-out), not ClickHouse — scale Redis if tail fan-out
  saturates.

## Ingest layer (write path)

Two independently-scaled Rust processes:

- **Gateway** (`ingest.autoscaling`, 3→20 @ 60% CPU) — HTTP/OTLP receiver + PII gate + transform. CPU
  is dominated by PII regex scanning and remap. Scale out on 5xx rate or CPU. It is fully stateless.
- **Writer** — competing consumers on the NATS `qeet-logs.>` stream. Add writer replicas to increase
  parallel ClickHouse batch-insert throughput when the DLQ or NATS backlog grows while ClickHouse is
  healthy. Inserts are idempotent (ULID dedup), so extra consumers are safe.

Back-pressure order under a write flood: gateway scales → NATS buffers (durable) → writer scales →
ClickHouse absorbs. If ClickHouse is the wall, the DLQ absorbs failed batches for later replay
rather than dropping data.

## ClickHouse (storage) — the real capacity planning

Vertical first (CPU/RAM/disk), then horizontal (shards + `ReplicatedMergeTree` replicas via
ClickHouse Keeper). Levers:

- **Tiering** — hot data on fast local disk; `cmd/lifecycle` proactively `MOVE`s each tenant's aged
  partitions to the S3/MinIO **cold** volume based on `tenant_tiers.hot_days` (Postgres migration
  `0020`), refining the global 3-day table-level move TTL. This keeps the hot working set small and
  query-fast without buying more hot disk. Requires the `hot_cold` storage policy
  (`clickhouse/config/storage.xml`) and reachable S3/MinIO.
- **Retention** — per-tenant `_retention_days` hard-deletes at the boundary (no shadow copies),
  bounding total volume. Tune via `PUT /v1/admin/retention`; preview cost with
  `GET /v1/admin/retention/cost`.
- **Rollups** — `metrics_5m` / `metrics_1h` AggregatingMergeTree materialized views
  (CH migration `0008`) shrink long-range metric queries; prefer them for wide time windows.
- **Cardinality cap** — `METRIC_MAX_LABEL_CARDINALITY` (default 50 000) protects a node from a
  single tenant's runaway label cardinality; over-limit PromQL queries return HTTP 400 with
  guidance.

## PostgreSQL

Metadata volume is modest. Scale vertically; add read replicas if admin/console read load grows.
RLS adds negligible overhead. Connection pooling is via `pgxpool` in-process.

## When to scale what — quick decision guide

| Symptom | First lever |
|---|---|
| Gateway 5xx / high CPU | `ingest.autoscaling.maxReplicas` ↑ |
| NATS backlog / DLQ climbing, ClickHouse healthy | more **writer** replicas |
| Query p95 breach, ClickHouse healthy | `query` replicas ↑ |
| Query p95 breach, ClickHouse hot | ClickHouse (vertical / shards / rollups / cardinality) |
| Hot disk filling | tune `tenant_tiers.hot_days` / retention; confirm `cmd/lifecycle` is moving partitions |
| Tail lag | Redis capacity |
| Alert lag/storm | tune the alerter (gate, rules) — **do not** add replicas |
