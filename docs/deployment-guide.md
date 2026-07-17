# Deployment Guide

How to deploy Qeet Logs to Kubernetes with the bundled Helm chart, plus the local-dev path. Chart
lives in [`../deploy/helm/qeet-logs/`](../deploy/helm/qeet-logs/).

## Topology

Qeet Logs deploys as several independent workloads:

| Workload | Chart section | Scale | Notes |
|---|---|---|---|
| Query API (`cmd/query`) | `query` | HPA 2→10 | Stateless REST/WS/admin, `:8100` |
| Ingest gateway (Rust) | `ingest` | HPA 3→20 | `:8101` + `:4318` OTLP |
| Alerter (`cmd/alerter`) | `alerter` | **1 (singleton)** | Never scale > 1 |
| OTel collector (DaemonSet) | `collector` | per-node | **Disabled by default** (`collector.enabled: false`) |
| ClickHouse / Postgres / NATS / Redis | — | operator-managed | **Not** in this chart — bring your own / cluster operators |

The chart ships Deployments, Services, HPAs (`hpa.yaml`), an Ingress, RBAC + ConfigMap for the
collector, and `_helpers.tpl`. The **stateful backing stores are out of scope** for this chart —
provision ClickHouse (with Keeper for `ReplicatedMergeTree`), PostgreSQL 17, NATS JetStream, Redis 7,
and S3/MinIO separately, then point the app at them via the secret.

## Prerequisites

- Kubernetes with an ingress controller (chart default `ingress.className: nginx`) and
  [cert-manager](https://cert-manager.io/) (`cluster-issuer: letsencrypt-prod`) for TLS.
- Container images published to `ghcr.io/qeetgroup/qeet-logs-{query,alerter,ingest,collector}`
  (see `global.imageRegistry`; tag defaults to `.Chart.AppVersion`).
- The backing stores reachable from the cluster.

## Secrets

The chart references an **externally-created** Kubernetes Secret (`secrets.existingSecret`, default
`qeet-logs-secrets`) — never put real values in `values.yaml`. Expected keys:

```
DATABASE_URL          postgres://…            (Postgres 17, RLS app role)
REDIS_URL             redis://…
NATS_URL              nats://…
CLICKHOUSE_URL        http(s)://…:8123
CLICKHOUSE_DATABASE   qeet_logs
CLICKHOUSE_USER
CLICKHOUSE_PASSWORD
QEET_ID_ISSUER        https://api.id.qeet.in  (OIDC relying-party)
NOTIFY_URL            (optional — Qeet Notify for alert delivery)
NOTIFY_KEY            (optional)
INGEST_API_KEY        (only if collector.enabled)
```

Create it, e.g.:

```bash
kubectl create secret generic qeet-logs-secrets \
  --from-literal=DATABASE_URL="postgres://…" \
  --from-literal=CLICKHOUSE_URL="http://clickhouse:8123" \
  --from-literal=CLICKHOUSE_DATABASE="qeet_logs" \
  --from-literal=QEET_ID_ISSUER="https://api.id.qeet.in" \
  # …remaining keys…
```

Environment variables map 1:1 to `platform/config` (`HTTP_PORT`, `ENV`, `DATABASE_URL`,
`CLICKHOUSE_*`, `NATS_URL`, `REDIS_URL`, `QEET_ID_ISSUER`, `COOKIE_DOMAIN`, `S3_*`,
`QEET_NOTIFY_*`, and tuning knobs `ALERT_PAGE_MIN_CONFIDENCE`, `METRIC_MAX_LABEL_CARDINALITY`,
`TRACE_SAMPLE_RATE`, `TRACE_SLOW_MS`). See [`../.env.example`](../.env.example) for the full list.

## Install

```bash
# 1. Provision + migrate the stores BEFORE the app comes up.
#    Postgres metadata:
make migrate-up   DB_URL="$PROD_DATABASE_URL"
#    ClickHouse DDL (idempotent):
make ch-migrate   # or apply clickhouse/migrations/*.sql against the prod cluster in order

# 2. Install the chart.
helm upgrade --install qeet-logs ./deploy/helm/qeet-logs \
  --namespace qeet-logs --create-namespace \
  --set global.imageRegistry=ghcr.io/qeetgroup \
  -f my-prod-values.yaml

# 3. Verify.
kubectl -n qeet-logs rollout status deploy/qeet-logs-query
curl -fsS https://api.logs.qeet.in/readyz
curl -fsS https://ingest.logs.qeet.in/readyz
```

Run migrations as a pre-install/pre-upgrade step (Helm hook or CI job) so schema always leads the
app — see [../deploy/runbooks/migration-rollback.md](../deploy/runbooks/migration-rollback.md) for
the ordering rules.

## TLS & ingress

The shipped, source-of-truth path is the **Helm Ingress** (`ingress.yaml`) — nginx +
cert-manager/Let's Encrypt — terminating TLS for two hosts:

- `api.logs.qeet.in` → the query API (`:8100`)
- `ingest.logs.qeet.in` → the ingest gateway (`:8101`)

with a `qeet-logs-tls` certificate covering both. Cookies are scoped to `.logs.qeet.in` (never the
parent `.qeet.in` zone). Internal cluster traffic encryption (mTLS via Cilium/Istio) is the
operator's responsibility per SOC 2 CC6.7.

> **Caddy note:** the workspace `CLAUDE.md` references a `deploy/Caddyfile` for edge TLS, but **no
> Caddyfile is vendored in this repo** (drift). The ingress above is the actual shipped TLS path. If
> you prefer Caddy as an edge reverse proxy, front the two Services with a Caddy site block that
> reverse-proxies `api.logs.qeet.in` → query `:8100` and `ingest.logs.qeet.in` → gateway `:8101`,
> with `tls` auto-HTTPS — but treat that as a bring-your-own overlay, not part of the chart.

## Cold-tier storage

To enable hot→cold tiering, mount `clickhouse/config/storage.xml` (the `s3_cold` MinIO/S3 disk +
`hot_cold` policy) into ClickHouse at `/etc/clickhouse-server/config.d/`, apply CH migration
`0009_cold_tier.sql`, and run `cmd/lifecycle`. Without the storage policy configured, the tiering
migration cannot attach. See [../deploy/runbooks/scaling.md](../deploy/runbooks/scaling.md).

## OTel collector (optional)

The DaemonSet is the power-user / fleet ingestion path and is **off by default** — the zero-config
SDKs pointing at the gateway are the default onboarding path. Enable with `collector.enabled=true`
(requires `INGEST_API_KEY` in the secret); it tails every pod log per node and forwards
logs/metrics/traces over OTLP to the gateway. See `deploy/otel-collector/README.md`.

## Local development

```bash
cp .env.example .env
make infra-up      # docker compose: ClickHouse, Postgres, NATS, Redis, MinIO
make migrate-up && make ch-migrate
make dev           # query API :8100
make dev-ingest    # gateway :8101 (needs rustup)
make dev-console   # console :3020 (bun)
```

Local infra ports are offset from qeet-id and qeet-notify so all three stacks run side by side (see
[`../docker-compose.yml`](../docker-compose.yml)).
