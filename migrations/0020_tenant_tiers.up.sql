-- Per-tenant hot/warm storage-tier configuration (PRD Module 6 hot/warm/cold
-- tiering / P2-G2). Drives the ClickHouse cold-tier lifecycle (see
-- clickhouse/migrations/0009_cold_tier.sql + cmd/lifecycle): partitions newer
-- than hot_days stay on fast local disk; between hot_days and the tenant's
-- retention boundary they live on the cold S3 volume (MinIO); past retention
-- they are hard-deleted by the existing per-record TTL.
--
-- An absent row means "all-hot until retention" (hot_days = retention). Same
-- convention as retention/tenant_plans: explicit tenant_id filtering, NO RLS.
CREATE TABLE tenant_tiers (
    tenant_id  UUID        PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    hot_days   INT         NOT NULL DEFAULT 3  CHECK (hot_days >= 0),   -- days kept on fast local disk
    cold_days  INT         NOT NULL DEFAULT 30 CHECK (cold_days >= 0),  -- days kept on cold S3 before delete
    updated_at TIMESTAMPTZ DEFAULT now()
);
