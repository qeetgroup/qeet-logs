-- qeet-logs metrics store (TAD §6.1/§6.2, PRD Module 02/05).
--
-- One row per OTLP data point. Scalar points (gauge / sum) carry `value`;
-- histogram / exponential-histogram points carry the count/sum/bucket columns.
-- Everything lands in the SAME columnar substrate as logs and traces so a
-- metric breach is a row a query can join against a trace/log by service and
-- time (PRD Module 02.3 "Unified Metrics + Logs + Traces Storage").
--
-- Engine: MergeTree for local/single-node dev. PRODUCTION swaps to
-- ReplicatedMergeTree (needs ClickHouse Keeper) — columns, partitioning,
-- ordering, indexes, and TTL below are identical; only the ENGINE line changes.
--
-- Idempotency: non_replicated_deduplication_window makes redelivered batches
-- (same insert token) idempotent on the local engine; ReplicatedMergeTree has
-- block deduplication on by default in production.
--
-- Idempotent (IF NOT EXISTS); applied via `make ch-migrate`.
CREATE TABLE IF NOT EXISTS qeet_logs.metrics
(
    id               String,                                    -- ULID, generated at ingest
    timestamp        DateTime64(9, 'UTC'),                      -- data-point time (OTel nanosecond fidelity)
    start_timestamp  DateTime64(9, 'UTC') DEFAULT toDateTime64(0, 9, 'UTC'), -- cumulative window start (sums)
    received_at      DateTime64(9, 'UTC') DEFAULT now64(9, 'UTC'),
    tenant_id        String,                                    -- required; enforced at the gate
    service          LowCardinality(String),                    -- required (resource service.name)
    environment      LowCardinality(String) DEFAULT '',

    metric_name      LowCardinality(String),                    -- e.g. http.server.duration
    metric_type      LowCardinality(String) DEFAULT 'gauge',    -- gauge|sum|histogram|exp_histogram|summary
    unit             LowCardinality(String) DEFAULT '',
    description      String DEFAULT '',
    temporality      LowCardinality(String) DEFAULT '',         -- delta|cumulative (sum/histogram)
    is_monotonic     UInt8 DEFAULT 0,                           -- monotonic sum == counter

    -- Scalar (gauge / sum) value.
    value            Float64 DEFAULT 0,

    -- Data-point attributes (labels). Map keeps high-cardinality labels queryable
    -- without a mapping-explosion failure mode (PRD Module 02.3 / 05.2).
    attributes       Map(LowCardinality(String), String),
    resource         String DEFAULT '{}',                       -- OTel resource attributes (JSON)

    -- Histogram / exponential-histogram data-point columns (empty for scalars).
    count            UInt64 DEFAULT 0,
    sum              Float64 DEFAULT 0,
    min              Float64 DEFAULT 0,
    max              Float64 DEFAULT 0,
    bucket_counts    Array(UInt64)  DEFAULT [],                 -- explicit-bucket histogram
    explicit_bounds  Array(Float64) DEFAULT [],
    scale            Int32  DEFAULT 0,                          -- exponential histogram
    zero_count       UInt64 DEFAULT 0,
    positive_offset  Int32  DEFAULT 0,
    positive_buckets Array(UInt64) DEFAULT [],
    negative_offset  Int32  DEFAULT 0,
    negative_buckets Array(UInt64) DEFAULT [],

    _retention_days  UInt16 DEFAULT 30,                         -- per-record retention, set from tenant config

    -- "Show me this metric across services" and label lookups stay index-assisted.
    INDEX idx_metric_name metric_name TYPE set(0)             GRANULARITY 1,
    INDEX idx_attr_keys   mapKeys(attributes) TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY (tenant_id, toYYYYMM(timestamp))
ORDER BY (tenant_id, metric_name, service, timestamp)
-- True hard-delete at the per-tenant retention boundary: no shadow copies.
TTL toDateTime(timestamp) + toIntervalDay(_retention_days)
SETTINGS index_granularity = 8192, non_replicated_deduplication_window = 1000;
