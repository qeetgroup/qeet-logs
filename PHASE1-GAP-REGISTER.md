# Qeet Logs — Phase 1 Gap Register

**Purpose:** Reconcile the *built* MVP (labelled "M0–M9 complete" in `CLAUDE.md`) against the **PRD's actual Phase 1** ([Product_Requirement_Document.md §10](../qeet-files/qeet-logs/Product_Requirement_Document.md), Phase 1 = Q4 2026–Q2 2027). The built MVP is a **logs-only** platform; PRD Phase 1 is a **logs + metrics + traces** observability platform with cross-signal investigation, correlation-first alerting, topology, and multi-language SDKs.

This register is the ordered worklist. We close gaps **one by one**, in dependency order. Each gap = a module reference, what Phase 1 requires, what exists today, what to build, and acceptance criteria.

**Legend:** ✅ done · ⚠️ partial · ❌ missing · effort S/M/L/XL

---

## What already exists (baseline — NOT gaps)

| Area | Status |
|---|---|
| ClickHouse `logs` + `auth_events` tables, tenant-partitioned, TTL, full-text bloom index | ✅ |
| Rust ingest gateway: `/v1/ingest`, `/v1/ingest/batch` (JSON), `/v1/logs` (OTLP/HTTP **JSON**) | ✅ logs only |
| PII gate (pre-storage regex redaction) — `ingest/core/pii.rs` | ✅ |
| Go query engine: LogQL++ lexer→parser→AST→ClickHouse compiler (logs/auth_events) | ✅ |
| Live tail (WS), saved searches, log explorer, dashboards CRUD | ✅ / ⚠️ |
| Alerter: **threshold + absence** rules | ✅ (no correlation/confidence) |
| Qeet Notify + webhook delivery — `domains/alerting/delivery.go` | ✅ |
| OIDC auth, RBAC scope guard, API-key→tenant, Postgres RLS | ✅ |
| Retention config (per-tenant TTL), DLQ + replay, quota usage API | ✅ |
| Go SDK, OpenAPI 3.1 spec, Helm chart (query/ingest/alerter), SOC2 control map | ✅ |
| Console (TanStack Start): sign-in, index, search, tail, alerts, dashboards, saved-searches, audit, api-keys, settings | ✅ 8 routes |

---

## The gaps (ordered, dependency-driven)

### Wave A — Signal foundation (unblocks all cross-signal work)

**G1 · Metrics & traces storage schema** — Modules 05.1 / 02.3 / 03.2 — ✅ **DONE** — **S**
- *Built:* `clickhouse/migrations/0004_metrics.sql` (gauge/sum/histogram/exp-histogram, high-cardinality labels as `Map(LowCardinality(String),String)`, tenant-led sort+partition key, `_retention_days` TTL, insert-dedup) and `0005_traces.sql` (**flat span rows**, `trace_id`/`span_id`/`duration_ns` bloom+minmax indexes for log↔span join and slow-span queries).
- *Verified:* `make ch-migrate` applies all 5 migrations; `metrics`+`traces` tables materialize; `tenant_id` confirmed leading partition/sort key; scalar + histogram metrics and flat spans round-trip; **log↔span cross-signal join on `trace_id`/`span_id` returns correlated rows** (Module 03.4 smoke test).
- *Deps:* none.

**G2 · Metrics ingestion** — Modules 02.1 (OTLP) / 02.2 (Prometheus `remote_write`) — ✅ **DONE** — **L**
- *Built:* `core/metric.rs` (`MetricRecord`/`MetricInput`/`build_metric`, non-finite→0 sanitisation); `gateway/otlp.rs::parse_metrics` (gauge/sum/histogram/exp-histogram/summary, lenient string-encoded int64, resource `service.name`/`deployment.environment`); `gateway/prom.rs` (Snappy-block + inline-`prost` `WriteRequest` decode, `__name__`/`job`/`service`/`env` label mapping); routes `/v1/metrics` + `/api/v1/write`; writer is now **signal-aware** — routes `qeet-logs.{tenant}.{logs|metrics|traces}` by subject to the right table, per-table dedup token, ack-all-or-retry. NATS stream widened to `qeet-logs.>`.
- *Verified:* `cargo build` + `cargo clippy` clean; `cargo test` green incl. the remote_write encode→snappy→decode round-trip. **End-to-end:** posted an OTLP/HTTP JSON payload (gauge + monotonic sum + histogram) through gateway→NATS→writer→ClickHouse; all 3 rows landed with correct typed columns (histogram `count/bucket_counts/explicit_bounds`, `is_monotonic=1`, gauge value), `attributes` Map + `service`/`environment` populated. Prometheus path covered by the round-trip unit test + shared `ingest_metric_inputs` handler (proven e2e via OTLP).
- *~~Outstanding Phase-1 P1 sub-items:~~* **Metric rollups + cardinality cap** ✅ **DONE** — `clickhouse/migrations/0008_metric_rollups.sql`: `metrics_5m` and `metrics_1h` AggregatingMergeTree tables (TTL 90d/365d) with Materialized Views that aggregate avg/min/max/sum/count of `value` and histogram count+sum on each INSERT. Per-tenant label-cardinality cap: `METRIC_MAX_LABEL_CARDINALITY` env (default 50 000); `checkCardinality()` runs `uniqExact(attributes)` before PromQL instant/range query execution and returns HTTP 400 with guidance if exceeded.
- *Deps:* G1.

**G3 · Traces ingestion + trace↔log correlation** — Modules 03.1 / 03.2 / 03.4 (P0) / 03.3 sampling (P1) — ✅ **DONE** — **L**
- *Built:* `core/span.rs` (`SpanRecord`/`SpanInput`/`build_span`, flat-span model, missing-parent preserved not dropped); `gateway/otlp.rs::parse_traces` (OTLP/HTTP JSON spans, `SpanKind`/`Status.code` int→string, `duration_ns` = end−start); route `/v1/traces` + `publish_span` → `qeet-logs.{tenant}.traces` (writer already routes it, G2). Tail-sampling `keep_span` — error + slow-outlier always kept; others kept iff a deterministic `sha256(trace_id)` bucket falls under `TRACE_SAMPLE_RATE` (consistent per trace; default 1.0 = lossless), `TRACE_SLOW_MS` slow threshold.
- *Verified:* build + clippy clean. **End-to-end:** pushed a log (`/v1/ingest`) and a 2-span trace (`/v1/traces`) sharing `trace_id` through the real pipeline → spans landed with correct `service`/`kind`/`status_code`/`parent_span_id`/`duration_ns`; the **log↔span join on `trace_id`+`span_id` returns the correlated error row**. Sampling proven: at `TRACE_SAMPLE_RATE=0` a normal span was `sampled_out` while an **error** span was kept and stored.
- *Deps:* G1.

### Wave B — Query the new signals

**G4 · Query engine for metrics & traces (+ PromQL surface)** — Modules 07.2 / 05.5 / 02.2 — ✅ **DONE** — **M**
- *Built:* extended the LogQL++ compiler (`domains/query/compile.go`) to `metrics`/`traces` — column whitelists, `attr.<label>` (metrics Map access / traces JSON), `resource.<k>` JSON extract, per-table default projections, `SEARCH` guarded to logs. New `domains/query/promql.go` — a PromQL-compatible surface (selectors, `=`/`!=`/`=~`/`!~` matchers, `sum/avg/min/max/count` with `by (...)`, `rate(...[range])`) compiling to a **two-level** ClickHouse query (per-series instant resolution → aggregation) so instant semantics match Prometheus. New handlers `PromInstantQuery`/`PromRangeQuery` at `/api/v1/query` + `/api/v1/query_range` (Grafana-compatible), tenant always injected.
- *Verified:* `go build`/`go vet`/`go test ./...` green incl. new `g4_test.go` (compiler + PromQL compile + forced-tenant guard). **End-to-end** against the live query server: LogQL++ `avg(value)` and `attr.route` filters over `metrics` returned correct rows; PromQL instant `sum by (service)` returned the **latest-per-series** value (220, not the naive 480 sum — bug caught and fixed during verification); range query returned a proper `matrix` with per-bucket points.
- *Deps:* G1–G3.

### Wave C — Ingestion completeness

**G5 · Ingest enrichment + legacy formats + OTLP protobuf** — Modules 01.2 / 04.3 — ✅ **DONE** (protobuf tracked) — **M**
- *Built:* enrichment — migration `0006_enrichment.sql` adds `git_sha`/`deploy_id`/`pr_number`/`k8s_namespace`/`k8s_pod`/`k8s_node` to `logs` + `traces`; `IngestInput`/`LogRecord`/`SpanInput`/`SpanRecord` carry them; `build_record` harvests them (explicit field → else common alt keys out of `extra`) and stamps the `resource` JSON; OTLP parsers fill them from resource attrs (`k8s.*`, `vcs.repository.ref.revision`, `deployment.id`). Legacy formats — new `gateway/legacy.rs`: `/v1/ingest/gelf` (GELF, syslog-level → canonical level, `_field` → attribute) and `/v1/ingest/syslog` (RFC 5424 parser with structured-data stripping + raw-line fallback); JSON-lines already served by `/v1/ingest/batch`. Query compiler gained the 6 enrichment columns for logs + traces.
- *Verified:* cargo build/clippy/test green (incl. GELF + syslog + fallback unit tests); go build/test green. **End-to-end:** OTLP log with resource attrs → `git_sha=abc123`, `deploy_id=deploy-42`, `k8s_pod=checkout-7f9`, `resource` JSON populated; GELF → `web-1`/warn; syslog → `authd`/fatal, message cleaned.
- *~~Outstanding (tracked, P0):~~* **OTLP/protobuf** ✅ **DONE** — `ingest/gateway/src/otlp_proto.rs` — inline prost structs (zero build-script/vendored-proto overhead) for `ExportLogsServiceRequest`, `ExportMetricsServiceRequest`, `ExportTraceServiceRequest`. All five metric data types (Gauge/Sum/Histogram/ExpHistogram/Summary) decoded. `ingest_otlp`/`ingest_metrics`/`ingest_traces` handlers branch on `Content-Type: application/x-protobuf`. `cargo build --release` clean.
- *Deps:* G1–G3.

**G6 · VRL-inspired transform/remap engine** — Modules 04.2 (P0) / 04.4 routing (P1) — ✅ **DONE** (advanced tracked) — **XL**
- *Built:* `core/remap.rs` — a bounded VRL-inspired language (`set`/`del`/`redact`/`rename` + `to_string`/`to_int`/`to_float`/`upcase`/`downcase`/`concat`/`coalesce`, dotted paths, string/number/bool literals, `#` comments) with a hard 200-statement cap and **fail-open** apply (a bad statement is skipped + recorded, never drops the event). Per-tenant programs in Postgres (`0007_transforms.up.sql`, versioned) loaded+compiled in the gateway auth path (invalid program ignored, fail-open); applied in-flight to the raw JSON event before storage on `/v1/ingest` + `/v1/ingest/batch`. Admin CRUD `GET`/`PUT /v1/admin/transform` (`handler/transforms.go`) bumps the version for atomic pickup.
- *Verified:* cargo build/clippy/test green incl. `remap` unit tests (set/del/redact/rename, nested paths, functions, fail-open, comments); go build green. **End-to-end:** set a program via admin API → gateway applied it: `environment` set, `git_sha` set (→enrichment column), `level` downcased, and `password` **redacted before ClickHouse**.
- *~~Outstanding (tracked):~~* **Conditionals + routing** ✅ **DONE** — `remap.rs` extended with `Stmt::If(Cond, Box<Stmt>)` and `Stmt::Route(String)`. `Cond` supports `==`, `!=`, `<`, `>`, `exists`, `absent`. `ApplyReport.route: Option<String>` carries the routing hint to the caller without polluting the event. `route` validates against the 5 known tables (logs/metrics/traces/auth_events/change_events). 8 new unit tests (16 total, all green). Wall-clock budget: unchanged — still bounded by no-loops design + 200-statement cap.
- *Deps:* G5.

**G7 · OTel Collector distribution + K8s auto-discovery** — Modules 04.1 (P0) / 01.3 (P0) / 01.5 (P1) — ✅ **DONE** — **M**
- *Built:* `deploy/otel-collector/builder-config.yaml` (ocb manifest — curated tested subset: otlp+filelog receivers, memory_limiter/k8sattributes/resourcedetection/batch processors, otlphttp+debug exporters, healthcheck) + `config.yaml` (pod-log auto-discovery via filelog `/var/log/pods/*/*/*.log`, k8s metadata enrichment → OTLP/HTTP to gateway with `X-Qeet-Api-Key`). Helm DaemonSet + ConfigMap + ClusterRole/RBAC + ServiceAccount under `deploy/helm/qeet-logs/templates/*-collector.yaml` (gated `collector.enabled`, default off), values block, and `deploy/otel-collector/README.md` framing it as the power-user escape hatch (SDKs remain the default).
- *Verified:* standalone collector `config.yaml` + `builder-config.yaml` parse; the Helm ConfigMap's embedded collector config parses. *Caveat:* `helm lint`/cluster apply not run (Helm CLI + K8s unavailable in this env) — templates mirror the chart's existing working conventions (fullname/labels/selectorLabels/image-tag).
- *Deps:* G5, existing Helm.

### Wave D — Investigation surfaces

**G8 · Deploy / change-event ingestion (basic)** — Module 15.1 (Phase-1 slice) — ✅ **DONE** — **S**
- *Built:* ClickHouse `change_events` table (`0007_change_events.sql`, tenant-partitioned, TTL, git_sha bloom index); `handler/changes.go` — `POST /v1/changes` (deploy/flag/config/rollback contract: git_sha/deploy_id/pr_number/flag_key/config_diff/author/metadata, `logs:ingest`) and `GET /v1/changes?service=` (`logs:read`); table added to the LogQL++ compiler so it's queryable uniformly (feeds G10 timeline + G13 RCA).
- *Verified:* go build/test green; migration applied. **End-to-end:** posted a deploy event → returned id; `GET /v1/changes?service=payments` returned it with all fields; `SELECT ... FROM change_events` via LogQL++ returned it too.
- *Deps:* G1.

**G9 · Service dependency & topology graph** — Modules 10.3 baseline (P0) / 10.1 multi-signal (P1) / 10.2 blast-radius (P1) — ✅ **DONE** — **L**
- *Built:* `domains/topology/topology.go` — edges from cross-service parent↔child span self-join (calls/errors/p95), nodes from traces + **log-based service inference** (multi-signal, degrades gracefully), coverage flag (`full`/`traces-only`/`logs-only`), transitive `FocusBlastRadius`. `GET /v1/topology?since=&service=` handler. Console: `_app/topology.tsx` (services + dependencies tables, click-to-focus blast radius) + nav item.
- *Verified:* go build green. **End-to-end:** seeded a gateway→payments→db trace chain + a log-only `cron-worker` → full graph returned correct nodes/edges (p95, error counts), `cron-worker` flagged `logs-only` (not omitted), and **blast radius for `db` = [gateway, payments]** (transitive upstream). *Console route added but not runtime-verified* (Node 24/pnpm toolchain not exercised; imports mirror existing working routes).
- *Deps:* G3, G5.

**G10 · Unified Investigation Timeline** — Modules 09.1 cross-signal (P0) / 09.2 deploy overlay (P1) — ✅ **DONE** (09.3/09.4 tracked) — **L**
- *Built:* `domains/timeline/timeline.go` — merges logs + spans + deploys into one time-sorted feed over the shared store; `?trace_id=T` gives the full cross-signal story of one request; window mode (`?service=&since=`) shows warn+ logs, error spans, and deploy overlays, with an `include_info` escape. `GET /v1/timeline` handler. Console `_app/timeline.tsx` (vertical event feed, trace focus, error highlighting) + nav.
- *Verified:* go build green. **End-to-end:** trace-scoped view returned the 3-event story (gateway span → payments error span → correlated error log, chronological); service-window view overlaid the deploy and correctly excluded the info-level noise log.
- *Outstanding (tracked):* failure-domain-isolated investigation plane (09.3 — a deployment/topology property, doc note) and incident-scoped sessions (09.4, P1 — needs a sessions store, pairs with Module 18 in Phase 2).
- *Deps:* G4, G8.

### Wave E — Detection & intelligence

**G11 · Alert correlation + confidence scoring** — Modules 13.1 (P0) / 13.2 (P0) / 13.4 adaptive baselines (P1) — ✅ **DONE** — **L**
- *Built:* `alerting/confidence.go` (calibrated [0,1] score: absence fixed, threshold via baseline z-score or cold-start excess; severity buckets); `alerting/baseline.go` (rolling per-window mean/std from ClickHouse, nil→static fallback); `alerting/incident.go` (fingerprint = tenant|service, `incidents` upsert with dedup via partial-unique open index, deploy-proximity from `change_events`, page-once gate at `ALERT_PAGE_MIN_CONFIDENCE` default 0.6). Engine now: adaptive-OR-static firing, correlate every firing cycle, page only above the gate, resolve closes the incident. Migration `0008_incidents`. `GET /v1/incidents` exposes the feed. **Also fixed a pre-existing bug**: `Evaluate` never fired because ClickHouse `count()` (UInt64) arrives as a JSON string — now parsed.
- *Verified:* go build + `go test ./domains/alerting` green (confidence/severity/fingerprint units). **End-to-end:** ran the alerter over seeded data — `payments` strong breach → critical, **paged**, deploy `deploy-99` attached; `billing` marginal → low, **low-severity feed, not paged**; both deduped into single incidents.
- *~~Outstanding (tracked):~~* **topology-proximity correlation** ✅ **DONE** — `topology.Graph.Neighbors()` added (1-hop caller+callee set). `Engine.neighborIncidentFP()` queries ClickHouse topology, then Postgres for any open incident matching a neighbor's fingerprint. In `correlate()`, a proximate neighbor incident is reused instead of opening a parallel one — cascading failures (A→B degradation) collapse into one incident. `go test ./domains/alerting/` green. Continuous calibration 13.3 = Phase 2.
- *Deps:* G4, G8, G9.

**G12 · Baseline anomaly scoring (Tier 1), non-predictive** — Module 14 Phase-1 slice — ✅ **DONE** — **M**
- *Built:* `domains/anomaly/anomaly.go` — `Score` (z-score with a Poisson √mean variance floor so spikes out of flat baselines are caught; drops score 0) + `Sweep` (per-(tenant,service) error-rate vs prior-window baseline, withholds services with <4 windows). Wired into the alerter cycle (`sweepAnomalies`) feeding outliers into the **same correlation path** as rules via a synthetic `KindAnomaly` detector (score = confidence).
- *Verified:* `go test ./domains/anomaly` green (incl. flat-baseline spike + below-mean cases). **End-to-end:** seeded a flat error baseline + a 200-error spike for `search` → alerter raised a **critical anomaly-driven incident** (`correlated_rules: ["anomaly:search"]`, paged) with no hand-configured rule. *"Tier 1" = the statistical floor; heavier ONNX model tiers are Phase 2.*
- *Deps:* G4, G11.

**G13 · RCA structural retrieval layer** — Module 11.1 (P0, Phase 1) — ✅ **DONE** — **M**
- *Built:* `domains/rca/rca.go` — retrieve-then-rank retriever (Meta-style): deploy-proximity candidates from `change_events` (recency-decayed score) + dependency-proximity candidates from `topology.Derive` (downstream callees with errors, scored by error rate), each with inspectable `evidence` (git_sha/deploy_id/ts or calls/errors/p95). `GET /v1/rca?service=|incident_id=&since=`. No generative step (11.2 learned ranker = Phase 2).
- *Verified:* go build green. **End-to-end:** seeded a `payments` deploy + a `payments→db` chain where `db` errors → RCA ranked `db` dependency (0.9, 2/2 calls) and the `v3.1` deploy (0.89, `sha777`) with full evidence.
- *Deps:* G8, G9, G10, G12.

### Wave F — Dashboards, DX, compliance

**G14 · Dashboards: panel data + correlation overlays + sharing (backend)** — Modules 22.1 (P0) / 22.2 (P1) / 22.3 (P1) — ✅ **DONE (backend)** — **M**
- *Built:* panel *data* is served by the existing `/v1/query` (LogQL++) + `/api/v1/query` (PromQL) — panels store their query and render client-side. NEW: `GET /v1/overlays` (`handler/overlays.go`) returns deploy/change markers (ClickHouse) + incident windows (Postgres) for correlation-aware panels (22.2); shareable dashboards via `share_token` (migration `0009`), `POST /v1/admin/dashboards/{id}/share` (stable token) + **public** `GET /shared/dashboards/{token}` (seat-free, unauthenticated read of name+panels — 22.3).
- *Verified:* go build green; migration applied. **End-to-end:** overlays returned a deploy marker + incident windows; created a dashboard → minted a share token → read it publicly with **no API key** → token stable on repeat.
- *Scope note:* per "backend only", the `@qeetrix/ui` drag-and-drop panel builder / chart rendering (22.1 UI) is **not** built — backend data + overlays + sharing are.
- *Deps:* G4.

**G15 · Multi-language zero-config SDKs (Node, Python, Java)** — Modules 01.1 (P0) / 29.1 (P0) — ✅ **DONE** — **L**
- *Built:* `sdk/python/` (stdlib-only `Client`, `log()`/`ingest()`/`ingest_batch()`, best-effort send with retry + `raise_on_error`), `sdk/node/` (ESM `QeetLogs` using built-in `fetch`, same surface), `sdk/java/` (single-class `QeetLogs` on `java.net.http`, hand-rolled JSON, zero deps). All target the **real** gateway contract (`/v1/ingest` single, `/v1/ingest/batch` NDJSON, `message` field) — fixing the shape the Go SDK got wrong. Best-effort/never-throw by default (logging can't break the host).
- *Verified:* **End-to-end against the live gateway** — Python (1 + batch 2), Node (1 + batch 1), Java (1, compiled with `javac` + run) all landed in ClickHouse with correct service/level/message.
- *Outstanding (tracked):* auto-capture of unhandled exceptions/HTTP/k8s metadata + async background buffering (current SDKs are explicit-call + synchronous best-effort); the existing Go SDK's `/v1/ingest` body shape (`{"records":...}`/`body`) should be reconciled to the real contract.
- *Deps:* G5.

**G16 · `qeet logs` CLI** — Module 29.2 (P1) — ✅ — **M**
- *Build:* `cmd/ql/main.go` — subcommands: `query`, `send`, `tail`, `incidents`, `topology`, `rca`. Zero deps (stdlib only). Env: `QEET_LOGS_API_KEY`, `QEET_LOGS_URL`, `QEET_LOGS_INGEST_URL`.
- *Accept:* `go build ./cmd/ql/` succeeds; help + error paths verified.
- *Deps:* stable query/API surface.

**G17 · DPDP data lifecycle & erasure API** — Modules 06.5 (P0) / 27 §7.1 — ✅ — **S**
- *Build:* `platform/api/handler/erasure.go` — `POST /v1/admin/erasure` + `GET /v1/admin/erasure`. Accepts `user_linkage_key` and/or `time_from`/`time_to`. Inserts `erasure_requests` row (pending), fires `ALTER TABLE logs/metrics/traces DELETE WHERE …` ClickHouse mutations (async), updates row to completed with JSON receipt. `GET` lists prior requests for the tenant.
- *Accept:* `go build ./cmd/query/` succeeds; handler registered under `logs:admin` scope.
- *Deps:* G1.

**G18 · NL-to-query API (backend)** — Modules 07.3 (P1) / 07.4 (P1) — ✅ — **M**
- *Build:* `platform/api/handler/nl_query.go` — `POST /v1/query/nl`. Calls claude-sonnet-5 via Anthropic Messages API with schema context. Returns `{"loqlpp":"SELECT …","explanation":"…"}` — inspectable and editable. Returns 501 if `ANTHROPIC_API_KEY` is unset. Console filter-builder UI deferred (backend-only per user instruction).
- *Accept:* `go build ./cmd/query/` succeeds; handler registered on authenticated tenant router.
- *Deps:* G4.

---

## Explicitly deferred (Phase 2/3 — NOT in this register)

Custom/business metrics API (2.5), pipeline dry-run (4.5), cost-transparent retention (6.4), TTFIQ metric (7.5), export/programmatic (8.5), business-event overlay (9.5), deploy-churn freshness (10.4) / network-flow inference (10.5), **RCA ranking model (11.2)**, AI Copilot (12), continuous calibration (13.3), **predictive observability (14.1–14.4)**, ranked culprit scoring (15.2), **Business Context Layer (16)**, Incident War Room (18), ChatOps (19), Postmortem graph (20), Closed-loop remediation (21), **auto-generated dashboards (23)**, Grafana-compatible data source (22.4), MCP server (29.3 Phase 2), IDE/CI surfacing (29.4).

---

## Build order summary

```
A: G1 → (G2, G3)          signal foundation
B: G4                     query metrics/traces
C: G5 → G6, G7            ingestion completeness
D: G8 → G9 → G10          investigation surfaces
E: G11 → G12 → G13        detection & RCA retrieval
F: G14, G15, G16, G17, G18 (largely parallel)
```

**Recommended first: G1** (additive ClickHouse migrations — zero risk to existing code, unblocks the most downstream work).

---

## Post-Phase-1 completions

**Integration test suite — Phase-1 coverage** ✅ — Two new `//go:build integration` files in `platform/clickhouse/`:
- `signals_integration_test.go` — 5 tests: metrics insert+query+rollup table existence, traces insert+cross-signal log↔span join, change_events insert+filter, cardinality `uniqExact` guard, cross-signal timeline row presence.
- `query_integration_test.go` — 3 tests: LogQL++ end-to-end (compile→CH execute, logs+metrics+aggregation), PromQL instant+range end-to-end, tenant isolation (row from tenant A never visible to tenant B query).
- All tests skip gracefully when `make infra-up` is not running (`c.Ping` check). Run with `make test-integration`.

**OpenAPI spec — Phase-1 parity** ✅ — `api/openapi/openapi.yaml` bumped to v2.0. Added:
- **New schemas**: `IngestRecord` (flat `message` field — fixes old `{"records":[...]}` contract), `MetricIngestRecord`, `SpanIngestRecord`, `IngestResponse`, `ChangeEvent`, `TopologyNode/Edge/Graph`, `TimelineEvent`, `Incident`, `RCACandidate/Result`, `OverlayMarker`, `ErasureRequest`, `NLQueryRequest/Response`, `TransformProgram`, `SharedDashboard`.
- **New paths** (20+): `/v1/ingest/batch` (NDJSON), `/v1/ingest/gelf`, `/v1/ingest/syslog`, `/v1/logs`, `/v1/metrics`, `/api/v1/write`, `/v1/traces`, `/api/v1/query`, `/api/v1/query_range`, `/v1/query/nl`, `/v1/changes`, `/v1/topology`, `/v1/timeline`, `/v1/incidents`, `/v1/rca`, `/v1/overlays`, `/shared/dashboards/{token}`, `/v1/admin/transform`, `/v1/admin/erasure`, `/v1/admin/dashboards/{id}/share`.
- **New tags**: Ingest — Logs/Metrics/Traces, PromQL, Changes, Topology, Timeline, Incidents, RCA, Dashboards, Admin — Transform, Admin — Erasure.
- **Ingest server** split: ingest-gateway endpoints (`ingest.logs.qeet.in` / `:8101`) documented separately from query API (`api.logs.qeet.in` / `:8100`).
