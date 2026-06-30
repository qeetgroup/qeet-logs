-- Scoped API keys (PRD Module 13.2). A tenant may hold several keys, each
-- granting a subset of logs:* scopes (ingest/read/query/export/admin/platform).
-- The raw key is shown once; only its SHA-256 hash is stored.
CREATE TABLE api_keys (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    key_hash     TEXT        NOT NULL UNIQUE,        -- SHA-256 of the raw key
    key_prefix   TEXT        NOT NULL,               -- first chars, shown in UI
    scopes       TEXT[]      NOT NULL DEFAULT '{}',  -- e.g. {logs:ingest,logs:read}
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash);
CREATE INDEX idx_api_keys_tenant   ON api_keys (tenant_id);
