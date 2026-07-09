-- qeet-logs trace/span store (TAD §6.1/§6.2, PRD Module 03/05).
--
-- FLAT span rows, not a nested trace tree — modelled on Slack's SpanEvent
-- schema, which "explicitly rejected nested Zipkin/Jaeger-style trace trees"
-- (PRD Module 03.2). Each span is one row in the SAME columnar substrate as
-- logs and metrics, sharing `trace_id` as the join key so a span links to its
-- log lines and metric breaches without a separate tracing product
-- (PRD Module 03.4 "Trace-to-Log/Metric Correlation").
--
-- Engine: MergeTree for local/single-node dev. PRODUCTION swaps to
-- ReplicatedMergeTree (needs ClickHouse Keeper) — columns, partitioning,
-- ordering, indexes, and TTL below are identical; only the ENGINE line changes.
--
-- Idempotent (IF NOT EXISTS); applied via `make ch-migrate`.
CREATE TABLE IF NOT EXISTS qeet_logs.traces
(
    id               String,                                    -- ULID, generated at ingest (row id)
    timestamp        DateTime64(9, 'UTC'),                      -- span start; partition/TTL/order key (mirrors logs)
    received_at      DateTime64(9, 'UTC') DEFAULT now64(9, 'UTC'),
    tenant_id        String,                                    -- required; enforced at the gate
    service          LowCardinality(String),                    -- required (resource service.name)
    environment      LowCardinality(String) DEFAULT '',

    trace_id         String,                                    -- OTel W3C; join key to logs/metrics
    span_id          String,
    parent_span_id   String DEFAULT '',
    name             LowCardinality(String),                    -- span/operation name
    kind             LowCardinality(String) DEFAULT 'internal', -- internal|server|client|producer|consumer

    start_time       DateTime64(9, 'UTC'),
    end_time         DateTime64(9, 'UTC') DEFAULT toDateTime64(0, 9, 'UTC'),
    duration_ns      UInt64 DEFAULT 0,                          -- end - start, precomputed for slow-span queries

    status_code      LowCardinality(String) DEFAULT 'unset',    -- unset|ok|error
    status_message   String DEFAULT '',

    attributes       String DEFAULT '{}',                       -- span attributes (JSON); queried via JSONExtract
    resource         String DEFAULT '{}',                       -- OTel resource attributes (JSON)
    scope_name       LowCardinality(String) DEFAULT '',
    scope_version    LowCardinality(String) DEFAULT '',
    trace_state      String DEFAULT '',

    _retention_days  UInt16 DEFAULT 30,                         -- per-record retention, set from tenant config

    -- "All spans in this trace" + the log<->span correlation join.
    INDEX idx_trace_id  trace_id  TYPE bloom_filter(0.01) GRANULARITY 1,
    -- parent/child span resolution.
    INDEX idx_span_id   span_id   TYPE bloom_filter(0.01) GRANULARITY 1,
    -- slow-outlier trace queries (feeds tail-based sampling + topology).
    INDEX idx_duration  duration_ns TYPE minmax          GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY (tenant_id, toYYYYMM(timestamp))
ORDER BY (tenant_id, service, timestamp)
-- True hard-delete at the per-tenant retention boundary: no shadow copies.
TTL toDateTime(timestamp) + toIntervalDay(_retention_days)
SETTINGS index_granularity = 8192, non_replicated_deduplication_window = 1000;
