-- Per-tenant billing plan + quota allowances (PRD Module 33.4 / P2-G17). Drives
-- usage-vs-plan overage COMPUTATION and invoice PREVIEW only; actual invoicing /
-- charging is delegated to Qeet Pay (Module 33.5) and is NOT implemented here.
-- One row per tenant; an absent row is treated as the 'free' plan with zero
-- allowances. Explicit tenant_id filtering (no RLS on this admin-only table).
CREATE TABLE tenant_plans (
    tenant_id                  UUID          PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    plan                       TEXT          NOT NULL DEFAULT 'free' CHECK (plan IN ('free','pro','enterprise')),
    included_events            BIGINT        NOT NULL DEFAULT 0,       -- events included per calendar month
    included_gb                NUMERIC(18,3) NOT NULL DEFAULT 0,       -- stored GB included per calendar month
    overage_per_million_events NUMERIC(12,4) NOT NULL DEFAULT 0,       -- USD per 1e6 events over the allowance
    overage_per_gb             NUMERIC(12,4) NOT NULL DEFAULT 0,       -- USD per GB over the allowance
    updated_at                 TIMESTAMPTZ   DEFAULT now()
);
