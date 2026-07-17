-- Outbound webhook subscriptions (PRD Module 30.4, Phase 2). A tenant registers
-- endpoint URLs subscribed to event types (e.g. incident.opened, incident.resolved,
-- alert.fired); the dispatcher POSTs a JSON payload signed with an HMAC-SHA256
-- secret (header X-Qeet-Signature: sha256=<hex>). Mirrors the incidents table's
-- explicit-tenant-filter convention (queries scope by tenant_id; no RLS policy).
CREATE TABLE webhook_endpoints (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    url         TEXT        NOT NULL,
    secret      TEXT        NOT NULL DEFAULT '',      -- HMAC signing secret (empty = unsigned)
    events      TEXT[]      NOT NULL DEFAULT '{}',    -- subscribed event types; empty = all events
    active      BOOLEAN     NOT NULL DEFAULT true,
    description TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_webhook_endpoints_tenant ON webhook_endpoints (tenant_id) WHERE active;
