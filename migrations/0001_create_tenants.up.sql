-- Tenants are the multi-tenancy root for qeet-logs metadata. Log records
-- themselves live in ClickHouse, partitioned by tenant_id; this table holds
-- the tenant registry, plan, and default retention window.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT        NOT NULL,
    slug           TEXT        NOT NULL UNIQUE,
    plan           TEXT        NOT NULL DEFAULT 'free',
    retention_days INT         NOT NULL DEFAULT 7,
    metadata       JSONB       NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
