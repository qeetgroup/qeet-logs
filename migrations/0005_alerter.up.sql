-- M6 alerter: per-rule firing state + richer audit_log columns.

-- alert_state persists which rules are currently firing so the alerter
-- survives restarts without re-firing on startup.
CREATE TABLE alert_state (
    rule_id     UUID        PRIMARY KEY REFERENCES alert_rules(id) ON DELETE CASCADE,
    tenant_id   UUID        NOT NULL,
    firing      BOOLEAN     NOT NULL DEFAULT false,
    fired_at    TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    last_eval   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_alert_state_tenant ON alert_state (tenant_id);

-- Extend audit_log with actor / resource context for the admin audit view.
-- Existing query-audit rows get NULL for the new columns (fine).
ALTER TABLE audit_log
    ADD COLUMN IF NOT EXISTS actor       TEXT,
    ADD COLUMN IF NOT EXISTS resource    TEXT,
    ADD COLUMN IF NOT EXISTS resource_id TEXT,
    ADD COLUMN IF NOT EXISTS status      TEXT     NOT NULL DEFAULT 'ok',
    ADD COLUMN IF NOT EXISTS ip          TEXT,
    ADD COLUMN IF NOT EXISTS user_agent  TEXT;
