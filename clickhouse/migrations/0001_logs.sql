-- qeet-logs primary log store (TAD §6.1/§6.2).
--
-- Engine: MergeTree for local/single-node dev. PRODUCTION swaps to
-- ReplicatedMergeTree (needs ClickHouse Keeper) — the columns, partitioning,
-- ordering, indexes, and TTL below are identical; only the ENGINE line changes.
--
-- Idempotency: the M2 writer sets ClickHouse insert-deduplication so retried
-- batches (same ULID set) are not double-inserted.
--
-- These statements are idempotent (IF NOT EXISTS) and applied via `make ch-migrate`.
CREATE DATABASE IF NOT EXISTS qeet_logs;

CREATE TABLE IF NOT EXISTS qeet_logs.logs
(
    id               String,                                    -- ULID, generated at ingest
    timestamp        DateTime64(9, 'UTC'),                      -- event time (OTel nanosecond fidelity)
    received_at      DateTime64(9, 'UTC') DEFAULT now64(9, 'UTC'),
    tenant_id        String,                                    -- required; enforced at the gate
    service          LowCardinality(String),                    -- required
    environment      LowCardinality(String) DEFAULT '',
    level            LowCardinality(String) DEFAULT 'info',     -- trace|debug|info|warn|error|fatal
    message          String,
    body             String DEFAULT '{}',                       -- arbitrary-depth JSON (PII masked); queried via JSONExtract
    resource         String DEFAULT '{}',                       -- OTel resource attributes (JSON)
    trace_id         String DEFAULT '',                         -- OTel W3C
    span_id          String DEFAULT '',
    user_linkage_key String DEFAULT '',                         -- pseudonymous GDPR-erasure reference
    pii_detected     Array(LowCardinality(String)) DEFAULT [],  -- fields where PII was masked
    ingested_by      LowCardinality(String) DEFAULT 'http',     -- ingest path (http|otlp|syslog|...)
    _retention_days  UInt16 DEFAULT 7,                          -- per-record retention, set from tenant config

    -- Full-text SEARCH acceleration: token bloom filter over the message.
    INDEX idx_message  message  TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1,
    -- "View logs for this trace" lookups.
    INDEX idx_trace_id trace_id TYPE bloom_filter(0.01)      GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY (tenant_id, toYYYYMM(timestamp))
ORDER BY (tenant_id, service, timestamp)
-- True hard-delete at the per-tenant retention boundary (Module 3.3): no shadow copies.
TTL toDateTime(timestamp) + toIntervalDay(_retention_days)
SETTINGS index_granularity = 8192;
