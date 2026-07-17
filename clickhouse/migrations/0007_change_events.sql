-- Change-event stream (PRD Module 15.1 Phase-1 slice + Gap 7/Gap 19): a simple,
-- standardized contract any CI/CD, feature-flag, or config tool can push
-- "something changed" events to (git SHA, PR, deploy ID, flag key, config diff).
-- Stored in the same columnar substrate so the Unified Investigation Timeline
-- (Module 09) and RCA structural retriever (Module 11) can query change proximity
-- alongside logs/metrics/traces. Idempotent (IF NOT EXISTS).
CREATE TABLE IF NOT EXISTS qeet_logs.change_events
(
    id              String,                                    -- ULID
    timestamp       DateTime64(9, 'UTC'),                      -- when the change happened
    received_at     DateTime64(9, 'UTC') DEFAULT now64(9, 'UTC'),
    tenant_id       String,
    service         LowCardinality(String) DEFAULT '',
    environment     LowCardinality(String) DEFAULT '',
    kind            LowCardinality(String) DEFAULT 'deploy',   -- deploy|flag|config|rollback
    title           String DEFAULT '',
    git_sha         String DEFAULT '',
    deploy_id       String DEFAULT '',
    pr_number       String DEFAULT '',
    flag_key        String DEFAULT '',
    config_diff     String DEFAULT '',
    author          LowCardinality(String) DEFAULT '',
    metadata        String DEFAULT '{}',                       -- arbitrary JSON
    _retention_days UInt16 DEFAULT 90,

    INDEX idx_git_sha git_sha TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY (tenant_id, toYYYYMM(timestamp))
ORDER BY (tenant_id, service, timestamp)
TTL toDateTime(timestamp) + toIntervalDay(_retention_days)
SETTINGS index_granularity = 8192;
