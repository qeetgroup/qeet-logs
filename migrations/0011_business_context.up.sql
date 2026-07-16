-- Business Context Correlation Layer (PRD Module 16, Phase 2 gap P2-G4). Maps a
-- tenant's services to the customers / plan-tiers / recurring revenue / SLA they
-- carry, so an incident on a service can be tagged with its BUSINESS impact:
-- affected customers + plan tiers, a qualified revenue-at-risk range, and the
-- strictest SLA target on the line. The exposure estimate itself is derived
-- (domains/buscontext, pure Go) — this table is just the mapping substrate.
-- Read-only CRM/billing connectors (Stripe/CSV, Module 16 I/O) can later populate
-- it; for now it is admin-managed CRUD. Mirrors the incidents table's
-- explicit-tenant-filter convention (queries scope by tenant_id; NO RLS policy).
CREATE TABLE business_context (
    id              UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    service         TEXT           NOT NULL,                 -- correlates to incidents.service
    customer        TEXT,                                    -- affected customer / account
    plan_tier       TEXT,                                    -- free | pro | enterprise
    monthly_revenue NUMERIC(18,2)  DEFAULT 0,                -- recurring monthly revenue on this line
    sla_target      NUMERIC(6,3),                            -- promised availability %, e.g. 99.900
    owner           TEXT,                                    -- accountable team / owner
    notes           TEXT,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now()
);

-- Exposure lookups fan out from (tenant, service) — the join key with incidents.
CREATE INDEX idx_business_context_tenant_service ON business_context (tenant_id, service);
