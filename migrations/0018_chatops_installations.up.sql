-- Two-way ChatOps app installations (PRD Module 19.1 slash-commands + 19.3 OAuth
-- app install / P2-G7). The one-way outbound slice (domains/chatops incoming-
-- webhook formatters + handler/chatops.go) is already shipped; this table backs
-- the INBOUND direction: a tenant installs the Qeet Logs Slack/Teams app via
-- OAuth, and the resulting workspace→tenant binding + bot token land here so an
-- incoming slash-command (which carries a workspace team_id, NOT a Qeet API key)
-- can be resolved back to the owning tenant.
--
-- Mirrors the incidents/ai_features convention: queries scope by tenant_id
-- explicitly; NO RLS policy on this table. bot_token is a workspace credential —
-- in production it MUST be encrypted at rest (KMS envelope, same as api_keys);
-- stored here as TEXT for the dev substrate.
CREATE TABLE chatops_installations (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider             TEXT        NOT NULL CHECK (provider IN ('slack', 'teams')),
    team_id              TEXT        NOT NULL,              -- Slack workspace / Teams tenant id
    team_name            TEXT,                              -- human label for the workspace
    bot_token            TEXT,                              -- xoxb-… (encrypt at rest in prod)
    incoming_webhook_url TEXT,                              -- optional outbound channel from the same install
    installed_by         TEXT,                              -- Slack user id that authorised the install
    created_at           TIMESTAMPTZ DEFAULT now(),
    -- One installation per (tenant, provider, workspace); re-install updates it.
    UNIQUE (tenant_id, provider, team_id)
);

-- Inbound slash-commands look up the installation by (provider, team_id) to
-- resolve the tenant — index that path.
CREATE INDEX idx_chatops_installations_team ON chatops_installations (provider, team_id);
