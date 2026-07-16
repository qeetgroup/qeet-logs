# Qeet Logs — Grafana dashboards

Importable Grafana dashboards for the Qeet Logs **query API**, driven by the Prometheus self-metrics
the API exposes on `:8100/metrics`.

| File | Dashboard | Covers |
|---|---|---|
| [`dashboards/qeet-logs-query.json`](dashboards/qeet-logs-query.json) | **Qeet Logs — Query API (RED)** | Rate, Errors, Duration (p50/p95/p99), in-flight concurrency, top-routes table, and Go/process runtime health |

The RED panels encode the SLO targets from [`docs/slo-sli.md`](../../docs/slo-sli.md) as thresholds:
**p95 < 2 s**, **p99 < 8 s**, **availability ≥ 99.9 %** (5xx error ratio < 0.1 %).

---

## Metrics these dashboards read

The query API (`platform/observability/metrics.go`) registers RED-style metrics on the default
Prometheus registry and serves them at `/metrics`:

| Metric | Type | Labels |
|---|---|---|
| `qeet_logs_http_requests_total` | counter | `method`, `route`, `status` |
| `qeet_logs_http_request_duration_seconds` | histogram (`_bucket`/`_count`/`_sum`) | `method`, `route` |
| `qeet_logs_http_requests_in_flight` | gauge | — |

Plus the standard Go runtime + process collectors (`go_*`, `process_*`) that ride on the default
registry.

> The `route` label is always the **matched chi route pattern** (e.g. `/v1/query`, `/api/v1/query`),
> never the raw URL — so high-cardinality path segments (ids, tenant UUIDs) never blow up the series
> count. Unmatched requests collapse to `route="unmatched"`.

---

## Which Prometheus to point at

Point the dashboard's datasource at whatever Prometheus is **scraping the query API's
`/metrics`**. Set that scrape up with [`prometheus-scrape.example.yml`](prometheus-scrape.example.yml):

- **Kubernetes (Helm chart):** the query Deployment pod template already carries the scrape
  annotations (`prometheus.io/scrape: "true"`, `prometheus.io/port: "8100"`,
  `prometheus.io/path: /metrics`). A Prometheus configured with the pod-annotation
  `kubernetes_sd_configs` relabeling (or a Prometheus Operator `PodMonitor`) will discover the pods
  automatically.
- **Docker / bare-metal:** scrape `<query-host>:8100/metrics` directly (see the static-config block
  in the example).

---

## Import

### Grafana UI

1. **Dashboards → New → Import**.
2. Upload `dashboards/qeet-logs-query.json` (or paste its contents).
3. When prompted for **`DS_PROMETHEUS`**, pick the Prometheus data source that scrapes the query API.
4. Import. Use the **Route** variable at the top to filter to a specific route
   (`/v1/query`, `/api/v1/query`, …) or leave it on **All**.

The dashboard declares its datasource via the `${DS_PROMETHEUS}` input, so it stays portable across
environments — nothing is hard-wired to a datasource UID.

### API / provisioning

```bash
# One-off import via the HTTP API (wrap in {"dashboard": ..., "overwrite": true}):
jq '{dashboard: ., overwrite: true, inputs: [
      {name:"DS_PROMETHEUS", type:"datasource", pluginId:"prometheus", value:"<your-ds-uid>"}]}' \
  dashboards/qeet-logs-query.json \
| curl -sS -X POST "$GRAFANA_URL/api/dashboards/import" \
    -H "Authorization: Bearer $GRAFANA_TOKEN" \
    -H 'Content-Type: application/json' -d @-
```

For **file-based provisioning** (`provisioning/dashboards/*.yaml` → a `providers:` folder), drop the
JSON into the provider's `path` and set a `DS_PROMETHEUS` default via the provider's
`foldersFromFilesStructure`/datasource variable, or template the datasource UID at deploy time.

---

## Notes

- All rate/latency panels use Grafana's `$__rate_interval` macro so they behave sensibly at any zoom.
- Latency percentiles are computed with `histogram_quantile()` over the `_bucket` series grouped by
  `le` — do not average pre-computed quantiles across pods.
- The **Top routes** table joins request rate, 5xx ratio, and p95 per route via a `merge`
  transformation; sort/filter columns live in the panel UI.
