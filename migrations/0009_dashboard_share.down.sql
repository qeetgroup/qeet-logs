DROP INDEX IF EXISTS idx_dashboards_share_token;
ALTER TABLE dashboards DROP COLUMN IF EXISTS share_token;
