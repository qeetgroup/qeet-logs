DROP POLICY IF EXISTS tenant_isolation ON erasure_requests;
ALTER TABLE erasure_requests DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON audit_log;
ALTER TABLE audit_log DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON dashboards;
ALTER TABLE dashboards DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON saved_searches;
ALTER TABLE saved_searches DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON alert_rules;
ALTER TABLE alert_rules DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON retention_config;
ALTER TABLE retention_config DISABLE ROW LEVEL SECURITY;

DROP ROLE IF EXISTS qeet_logs_app;
