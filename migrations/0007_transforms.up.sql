-- Per-tenant in-flight remap program (PRD Module 04.2). The Rust gateway loads
-- the enabled program for a tenant and applies it to each event before storage.
-- Versioned so a change is an atomic swap the gateway picks up on its next
-- auth-cache refresh (Module 04 edge case: config changes roll back atomically).
CREATE TABLE transforms (
    tenant_id  UUID        PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    program    TEXT        NOT NULL DEFAULT '',
    version    INT         NOT NULL DEFAULT 1,
    enabled    BOOLEAN     NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
