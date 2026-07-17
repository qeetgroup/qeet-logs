-- Shareable dashboards (PRD Module 22.3): a stable share token lets a dashboard
-- be viewed/embedded without a per-viewer seat (Sumo Logic's praised pattern).
ALTER TABLE dashboards
    ADD COLUMN IF NOT EXISTS share_token TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_dashboards_share_token
    ON dashboards (share_token) WHERE share_token IS NOT NULL;
