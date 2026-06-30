-- Typed identity/auth-event stream (TAD §6.2, PRD Module 09).
--
-- Schema is created now so the rest of the platform can target it; it is
-- POPULATED in Phase 2, when qeet-logs subscribes to the Qeet ID auth-event
-- stream (NATS qeet.auth.events), enriches, and stores typed events here.
--
-- Same engine/partition/TTL story as logs: MergeTree locally, Replicated in prod.
CREATE TABLE IF NOT EXISTS qeet_logs.auth_events
(
    id               String,
    timestamp        DateTime64(9, 'UTC'),
    received_at      DateTime64(9, 'UTC') DEFAULT now64(9, 'UTC'),
    tenant_id        String,

    -- auth.* (TAD): the typed identity signal.
    event_type       LowCardinality(String),                    -- login_success|login_failed|token_issued|mfa_*|passkey_*|...
    auth_method      LowCardinality(String) DEFAULT '',
    user_id          String DEFAULT '',
    session_id       String DEFAULT '',
    error_code       LowCardinality(String) DEFAULT '',
    attempt_number   UInt32 DEFAULT 0,
    mfa_required     UInt8 DEFAULT 0,
    mfa_passed       UInt8 DEFAULT 0,
    ip_country       LowCardinality(String) DEFAULT '',
    ip_asn           String DEFAULT '',
    device_new       UInt8 DEFAULT 0,
    risk_score       Float32 DEFAULT 0,

    user_linkage_key String DEFAULT '',
    _retention_days  UInt16 DEFAULT 90,

    INDEX idx_user_id user_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY (tenant_id, toYYYYMM(timestamp))
ORDER BY (tenant_id, event_type, timestamp)
TTL toDateTime(timestamp) + toIntervalDay(_retention_days)
SETTINGS index_granularity = 8192;
