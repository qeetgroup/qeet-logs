-- qeet-logs metric rollups (PRD Module 02.4 / Gap G2 tracked).
--
-- Pre-aggregated 5-minute and 1-hour rollup tables fed by Materialized Views.
-- The MVs fire on each batch INSERT to the raw `metrics` table and merge the
-- AggregatingMergeTree state in the background.
--
-- What is rolled up:
--   - Scalar (gauge/sum): avg/min/max/sum/count of `value`
--   - Histogram: sum of data-point count + sum columns (useful for rate()
--     calculations); per-bucket merging requires quantile state — Phase 2.
--
-- Retention: 90 days for 5m, 365 days for 1h (longer than raw 30-day default).
-- Rollup tables are not per-tenant-retention — they are infrastructure aggregates.
--
-- Idempotent (IF NOT EXISTS); applied via `make ch-migrate`.

-- ── 5-minute rollup ─────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS qeet_logs.metrics_5m
(
    tenant_id    String,
    service      LowCardinality(String),
    environment  LowCardinality(String),
    metric_name  LowCardinality(String),
    metric_type  LowCardinality(String),
    attributes   Map(LowCardinality(String), String),
    bucket       DateTime64(0, 'UTC'),

    value_avg    AggregateFunction(avg, Float64),
    value_min    AggregateFunction(min, Float64),
    value_max    AggregateFunction(max, Float64),
    value_sum    AggregateFunction(sum, Float64),
    sample_count AggregateFunction(count, UInt8),
    histo_count  AggregateFunction(sum, UInt64),
    histo_sum    AggregateFunction(sum, Float64)
)
ENGINE = AggregatingMergeTree
PARTITION BY (tenant_id, toYYYYMM(bucket))
ORDER BY (tenant_id, metric_name, service, environment, attributes, bucket)
TTL toDateTime(bucket) + toIntervalDay(90)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS qeet_logs.metrics_5m_mv
TO qeet_logs.metrics_5m
AS
SELECT
    tenant_id,
    service,
    environment,
    metric_name,
    metric_type,
    attributes,
    toStartOfInterval(timestamp, INTERVAL 5 MINUTE) AS bucket,
    avgState(value)   AS value_avg,
    minState(value)   AS value_min,
    maxState(value)   AS value_max,
    sumState(value)   AS value_sum,
    countState()      AS sample_count,
    sumState(count)   AS histo_count,
    sumState(sum)     AS histo_sum
FROM qeet_logs.metrics
GROUP BY tenant_id, service, environment, metric_name, metric_type, attributes, bucket;

-- ── 1-hour rollup ───────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS qeet_logs.metrics_1h
(
    tenant_id    String,
    service      LowCardinality(String),
    environment  LowCardinality(String),
    metric_name  LowCardinality(String),
    metric_type  LowCardinality(String),
    attributes   Map(LowCardinality(String), String),
    bucket       DateTime64(0, 'UTC'),

    value_avg    AggregateFunction(avg, Float64),
    value_min    AggregateFunction(min, Float64),
    value_max    AggregateFunction(max, Float64),
    value_sum    AggregateFunction(sum, Float64),
    sample_count AggregateFunction(count, UInt8),
    histo_count  AggregateFunction(sum, UInt64),
    histo_sum    AggregateFunction(sum, Float64)
)
ENGINE = AggregatingMergeTree
PARTITION BY (tenant_id, toYYYYMM(bucket))
ORDER BY (tenant_id, metric_name, service, environment, attributes, bucket)
TTL toDateTime(bucket) + toIntervalDay(365)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS qeet_logs.metrics_1h_mv
TO qeet_logs.metrics_1h
AS
SELECT
    tenant_id,
    service,
    environment,
    metric_name,
    metric_type,
    attributes,
    toStartOfHour(timestamp) AS bucket,
    avgState(value)   AS value_avg,
    minState(value)   AS value_min,
    maxState(value)   AS value_max,
    sumState(value)   AS value_sum,
    countState()      AS sample_count,
    sumState(count)   AS histo_count,
    sumState(sum)     AS histo_sum
FROM qeet_logs.metrics
GROUP BY tenant_id, service, environment, metric_name, metric_type, attributes, bucket;
