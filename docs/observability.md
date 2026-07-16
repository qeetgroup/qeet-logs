# Observability

How to observe Qeet Logs itself — the meta-monitoring story. (Yes, it should eventually ingest its
own telemetry; see the dogfooding note at the end.)

## Current state — read this first

Qeet Logs services emit **structured JSON logs** (zerolog, `platform/observability/logger.go`) in
production and human-readable console output in `development`. Logs carry request IDs
(`chi/middleware.RequestID`).

> **A Prometheus `/metrics` scrape endpoint is NOT yet exposed by the Go services**, and the query
> API does **not** yet export its own OpenTelemetry traces. The metrics endpoint is being added. Do
> not point a Prometheus scrape at `:8100/metrics` expecting app metrics today. The `/api/v1/query`
> and `/loki/api/v1/*` surfaces are **inbound query APIs** for *tenant* data (Grafana data sources),
> not self-metrics.

Until the metrics endpoint lands, monitor via the proxies below.

## What to watch now (via proxies)

### Health & readiness (both hosts)
```bash
curl -fsS https://api.logs.qeet.in/healthz      # liveness (process up)
curl -fsS https://api.logs.qeet.in/readyz       # readiness: Postgres + Redis + ClickHouse + NATS
curl -fsS https://api.logs.qeet.in/version      # build SHA / version
curl -fsS https://ingest.logs.qeet.in/readyz    # gateway deps
```
Wire `/readyz` into the K8s readiness probe (the chart does) so a pod with a degraded dependency is
pulled from rotation rather than serving errors. Alert on `/readyz` flapping.

### Golden signals (from logs + backing-store metrics)
| Signal | Where to get it today |
|---|---|
| Ingest 2xx/5xx | Gateway access logs / ingress metrics |
| Query latency | Query API request logs (duration) / ingress metrics |
| Tail lag | Synthetic probe: ingest → observe in `/v1/query/tail` |
| DLQ depth | `GET /v1/admin/dlq` (climbing = writer/ClickHouse unhealthy) |
| Alerter liveness | `cmd/alerter` cycle logs (~60 s cadence) |

### Backing-store telemetry (scrape these — they *do* export metrics)
- **ClickHouse** — `system.metrics`, `system.events`, `system.mutations`, `system.merges`,
  `system.parts`; the ClickHouse Prometheus endpoint. Watch mutation/merge backlog (erasure and
  DROP-column operations show here), disk usage per volume (hot vs `s3_cold`), and query duration.
- **PostgreSQL** — connections, replication lag, slow queries (`pg_stat_statements`).
- **NATS JetStream** — `:8222/varz`, stream `qeet-logs.>` depth, consumer lag, redelivery counts.
- **Redis** — memory, evictions, pub/sub channel counts (`tail.{tenant}.{service}`).

## Ingestion of external telemetry (the collector)

Qeet Logs ships an **OTel Collector distribution** for *ingesting your fleet's* telemetry
(`deploy/otel-collector/`, Helm DaemonSet, off by default). This is inbound data-plane, distinct
from self-observability. It tails pod logs, enriches with k8s metadata, and forwards
logs/metrics/traces over OTLP to the gateway. SDKs remain the default; the collector is the
power-user escape hatch.

## Tracing knobs (data-plane, not self-tracing)

The ingest path supports **tail sampling** for *incoming* traces: `TRACE_SAMPLE_RATE` (default 1.0 =
lossless; deterministic per-`trace_id` bucketing so a trace is kept or dropped consistently) and
`TRACE_SLOW_MS` (slow-span always-keep threshold). Error spans are always kept. This governs which
customer spans are stored, not the qeet-logs services' own spans.

## What to alert on

| Condition | Severity | Action |
|---|---|---|
| `/readyz` red on either host | SEV-1/2 | [incident-response.md](../deploy/runbooks/incident-response.md) |
| Ingest 5xx rate elevated | SEV-2 | Scale gateway; check NATS |
| DLQ depth climbing unbounded | SEV-2 | Check writer + ClickHouse |
| ClickHouse mutation/merge backlog growing | SEV-2/3 | Throttle erasures; add ClickHouse capacity |
| Alerter no successful cycle > 5 min | SEV-2 | Restart the singleton |
| Hot-disk usage near threshold | SEV-3 | Tune retention / `hot_days`; confirm `cmd/lifecycle` |

## Roadmap (honest)

- **In-process Prometheus `/metrics`** on query + gateway + alerter (RED/USE metrics) — in progress.
- **Self-tracing** — the services do not yet export their own OTLP traces; W3C tracecontext
  propagation on the query path is a follow-up.
- **Dogfooding** — the end state is Qeet Logs ingesting its own JSON logs, metrics, and traces as a
  tenant, so meta-monitoring uses the product's own query/alert/incident surfaces. Not wired yet.
