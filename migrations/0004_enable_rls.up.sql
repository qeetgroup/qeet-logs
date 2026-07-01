-- M5: Enable row-level security on all domain tables so that the non-owner
-- application role (qeet_logs_app) can only see rows belonging to the
-- tenant whose UUID is set via: SET LOCAL app.tenant_id = '<uuid>';
-- Auth-root tables (tenants, api_keys) are intentionally excluded — they
-- are queried before the tenant context is known.
--
-- Role creation uses IF NOT EXISTS so re-running is idempotent. In
-- production the application connects as qeet_logs_app; in dev the
-- migration user (postgres/superuser) is the table owner and bypasses RLS
-- naturally — the policies only bind qeet_logs_app.

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'qeet_logs_app') THEN
        CREATE ROLE qeet_logs_app WITH LOGIN;
    END IF;
END $$;

GRANT CONNECT ON DATABASE postgres TO qeet_logs_app;
GRANT USAGE ON SCHEMA public TO qeet_logs_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO qeet_logs_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO qeet_logs_app;

-- Ensure future tables are also accessible (migration user grants these on
-- each migrate-up, so this is a belt-and-suspenders default).
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO qeet_logs_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO qeet_logs_app;

-- ── retention_config ─────────────────────────────────────────────────────────
ALTER TABLE retention_config ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON retention_config
    AS PERMISSIVE FOR ALL TO qeet_logs_app
    USING     (tenant_id::text = current_setting('app.tenant_id', TRUE))
    WITH CHECK (tenant_id::text = current_setting('app.tenant_id', TRUE));

-- ── alert_rules ───────────────────────────────────────────────────────────────
ALTER TABLE alert_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON alert_rules
    AS PERMISSIVE FOR ALL TO qeet_logs_app
    USING     (tenant_id::text = current_setting('app.tenant_id', TRUE))
    WITH CHECK (tenant_id::text = current_setting('app.tenant_id', TRUE));

-- ── saved_searches ────────────────────────────────────────────────────────────
ALTER TABLE saved_searches ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON saved_searches
    AS PERMISSIVE FOR ALL TO qeet_logs_app
    USING     (tenant_id::text = current_setting('app.tenant_id', TRUE))
    WITH CHECK (tenant_id::text = current_setting('app.tenant_id', TRUE));

-- ── dashboards ────────────────────────────────────────────────────────────────
ALTER TABLE dashboards ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dashboards
    AS PERMISSIVE FOR ALL TO qeet_logs_app
    USING     (tenant_id::text = current_setting('app.tenant_id', TRUE))
    WITH CHECK (tenant_id::text = current_setting('app.tenant_id', TRUE));

-- ── audit_log ─────────────────────────────────────────────────────────────────
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_log
    AS PERMISSIVE FOR ALL TO qeet_logs_app
    USING     (tenant_id::text = current_setting('app.tenant_id', TRUE))
    WITH CHECK (tenant_id::text = current_setting('app.tenant_id', TRUE));

-- ── erasure_requests ──────────────────────────────────────────────────────────
ALTER TABLE erasure_requests ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON erasure_requests
    AS PERMISSIVE FOR ALL TO qeet_logs_app
    USING     (tenant_id::text = current_setting('app.tenant_id', TRUE))
    WITH CHECK (tenant_id::text = current_setting('app.tenant_id', TRUE));
