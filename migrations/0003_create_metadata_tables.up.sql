-- M1 metadata tables. All carry tenant_id for isolation; RLS policies + a
-- non-owner app role are added in M5 (the query layer already scopes every
-- read by the JWT/API-key tenant, never user input).

-- Per-tenant retention window + PII masking actions (Modules 3.2, 10.2, 17.2).
-- Authoritative config consumed by the ingest PII gate and the writer's
-- _retention_days stamping; tenants.retention_days is the convenience default.
CREATE TABLE retention_config (
    tenant_id       UUID        PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    retention_days  INT         NOT NULL DEFAULT 7,
    masking_actions JSONB       NOT NULL DEFAULT '{}',  -- {"email":"mask","ip":"hash","card":"drop_field"}
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Threshold + absence alert rules (Module 07).
CREATE TABLE alert_rules (
    id             UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID             NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name           TEXT             NOT NULL,
    kind           TEXT             NOT NULL,                       -- 'threshold' | 'absence'
    service        TEXT,                                            -- optional scope
    condition      TEXT,                                            -- LogQL++ / condition expression
    threshold      DOUBLE PRECISION,                                -- threshold rules
    window_seconds INT              NOT NULL DEFAULT 300,
    channels       JSONB            NOT NULL DEFAULT '[]',          -- [{"type":"email","target":"..."},{"type":"webhook",...}]
    enabled        BOOLEAN          NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ      NOT NULL DEFAULT now()
);
CREATE INDEX idx_alert_rules_tenant ON alert_rules (tenant_id);

-- Saved searches, shareable via stable URLs (Module 16.1).
CREATE TABLE saved_searches (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    query_text TEXT        NOT NULL,
    created_by TEXT,                                  -- Qeet ID user id/email
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_saved_searches_tenant ON saved_searches (tenant_id);

-- Custom dashboards (Module 06.3).
CREATE TABLE dashboards (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    panels     JSONB       NOT NULL DEFAULT '[]',
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dashboards_tenant ON dashboards (tenant_id);

-- Append-only query audit log (Module 13.3). Tamper-evident hash chaining is a
-- GA hardening; M1 ships the append-only table.
CREATE TABLE audit_log (
    id           BIGSERIAL   PRIMARY KEY,
    tenant_id    UUID        NOT NULL,
    user_id      TEXT,
    action       TEXT        NOT NULL,                 -- 'query' | 'export' | 'tail' | ...
    query_text   TEXT,
    time_range   TEXT,
    result_count BIGINT,
    latency_ms   INT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_tenant_time ON audit_log (tenant_id, created_at);

-- GDPR Article 17 erasure requests (Module 11.1). Schema now; the erasure job
-- + signed receipts are Phase 2.
CREATE TABLE erasure_requests (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_linkage_key TEXT        NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'pending',  -- pending|completed|failed
    requested_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ,
    receipt          JSONB
);
CREATE INDEX idx_erasure_requests_tenant ON erasure_requests (tenant_id);
