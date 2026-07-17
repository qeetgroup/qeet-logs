# Runbook — On-Call

The Qeet Logs on-call operator keeps the ingest and query paths healthy and responds to pages. Pair
this with [incident-response.md](incident-response.md).

## What you own

| Component | Deployment | Notes |
|---|---|---|
| Query API (`cmd/query`) | Deployment, HPA 2→10 | Stateless; safe to restart/scale freely |
| Ingest gateway (Rust) | Deployment, HPA 3→20 | Stateless; the write front door |
| Ingest writer (Rust) | Deployment | Drains NATS → ClickHouse; idempotent inserts |
| Alerter (`cmd/alerter`) | Deployment, **replicaCount 1** | **Singleton** — never scale > 1 |
| Lifecycle mover (`cmd/lifecycle`) | Deployment/CronJob | Cold-tier partition mover; best-effort |
| ClickHouse | StatefulSet (operator-managed) | Log/metric/trace store; **stateful** |
| PostgreSQL 17 | StatefulSet (operator-managed) | Metadata + RLS; **stateful** |
| NATS JetStream | StatefulSet | Ingestion bus; persists in-flight messages |
| Redis 7 | StatefulSet/Deployment | Live-tail fan-out, rate limits; cache-like |

## Start-of-shift checklist

1. **Probes green** for both hosts:
   ```bash
   curl -fsS https://api.logs.qeet.in/readyz
   curl -fsS https://ingest.logs.qeet.in/readyz
   curl -fsS https://api.logs.qeet.in/version   # confirm the deployed build
   ```
2. **No unactioned SEV-1/2** from the previous shift; read the handoff notes.
3. **DLQ depth** stable, not climbing — `GET /v1/admin/dlq` (a growing DLQ means the writer or
   ClickHouse is unhealthy).
4. **Alerter alive** — one replica, recent successful evaluation cycle in its logs (polls every
   ~60 s).
5. **Backups fresh** — last Postgres + ClickHouse backup within RPO (see
   [backup-restore.md](backup-restore.md)).
6. **Capacity headroom** — HPA not pinned at `maxReplicas`; ClickHouse disk below the cold-tier /
   retention thresholds.

## Golden signals to watch

Until the in-process Prometheus `/metrics` endpoint ships (see
[../../docs/observability.md](../../docs/observability.md)), watch these proxies:

- **Ingest availability** — gateway 2xx rate / 5xx rate; NATS publish success.
- **Query latency** — p95 of `/v1/query` and `/api/v1/query`. Target in
  [../../docs/slo-sli.md](../../docs/slo-sli.md).
- **Tail lag** — time from ingest to appearance in `/v1/query/tail`.
- **Alert delivery** — alerter cycle success; Qeet Notify / webhook delivery errors.
- **DLQ depth** and **ClickHouse mutation/merge backlog** (`system.mutations`, `system.merges`).

## Known gotchas

- **The alerter is a singleton.** Two concurrent alerters double-page and race incident upserts.
  Keep `alerter.replicaCount: 1`.
- **Erasure mutations are heavy.** A `POST /v1/admin/erasure` fires async ClickHouse
  `ALTER … DELETE` mutations; several at once can saturate a node. Watch `system.mutations`.
- **PromQL cardinality cap** returns HTTP 400 (not 5xx) when a tenant exceeds
  `METRIC_MAX_LABEL_CARDINALITY` — that is by design, not an outage.
- **Slack ChatOps returns 501** until the Slack app secrets are set — expected, not a bug.
- **`make seed` / `cmd/seed`** is not present in the tree; do not rely on it for prod smoke tests.
  Use the SDKs or `cmd/ql`.
- **ClickHouse dev engine is `MergeTree`; prod is `ReplicatedMergeTree`** (needs ClickHouse Keeper).
  Only the `ENGINE` line differs — see `clickhouse/migrations/0001_logs.sql`.

## Escalation

1. On-call engineer (you) — triage + mitigate.
2. Qeet Logs service owner — subsystem expertise (query engine, Rust ingest, ClickHouse).
3. Platform/infra on-call — ClickHouse/Postgres/NATS cluster, K8s, storage.
4. Qeet Group leadership — for SEV-1, cross-tenant exposure, or CERT-In-reportable events.

## Handoff

Record open incidents, mitigations in flight, any temporary config (e.g. raised
`ALERT_PAGE_MIN_CONFIDENCE`, bumped replica floors), and pending DLQ replays. Use the war-room
session entries (`POST /v1/admin/sessions/{id}/entries`) as the durable record.
