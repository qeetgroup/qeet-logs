-- M9: Dead-letter queue for failed ingest events.
-- The Rust writer populates this table when ClickHouse insert fails after
-- max retries. The replay API (POST /v1/admin/dlq/{id}/replay) re-publishes
-- the raw payload back to NATS for another write attempt.
CREATE TABLE dlq_events (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL,
    payload     JSONB       NOT NULL,
    error_msg   TEXT        NOT NULL,
    attempt     INT         NOT NULL DEFAULT 1,
    status      TEXT        NOT NULL DEFAULT 'pending',  -- pending | replayed | dropped
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    replayed_at TIMESTAMPTZ
);
CREATE INDEX idx_dlq_tenant_status ON dlq_events (tenant_id, status, created_at DESC);
